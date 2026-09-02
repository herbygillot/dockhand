// Package git drives the git CLI against one repository, plumbing
// commands only. The same principle that has dockhand drive MacPorts
// rather than reimplement it applies to git verbatim: a Go git library
// is a reimplementation, and it diverges from real git exactly where
// this design lives — linked worktrees, the sparse index, notes — in
// the user's own repository, the riskiest place to find out. git is a
// Required-tier tool with a doctor-enforced version floor, so the CLI
// is present by contract; state is read by asking git through its
// machine-readable plumbing, never by parsing .git.
//
// Every command runs with a scrubbed environment and classifies
// failures by exit code, never by message text. If per-call latency
// ever measures at fleet scale, the escalation is one long-lived
// `git cat-file --batch` session — the eval pattern — not a library.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
)

// ErrNotARepo reports a directory that no git repository contains.
var ErrNotARepo = errors.New("git: not inside a git repository")

// ErrBranchExists reports a mint refused because its branch already
// exists: there is a change in flight under that name.
var ErrBranchExists = errors.New("git: branch already exists")

// Repo is one repository, addressed by its top-level working directory.
type Repo struct {
	Root string
	// tools is the run's finder, set by Open and carried so every
	// command the repository runs resolves git the way doctor did.
	tools *tool.Finder
}

// exitFatal is git's exit code for fatal errors — for rev-parse
// --show-toplevel, "not in a repository" (a bare repository, equally
// unusable here, exits the same way).
const exitFatal = 128

// Open finds the repository containing dir, resolving git through the
// run's finder.
func Open(ctx context.Context, tools *tool.Finder, dir string) (*Repo, error) {
	out, code, err := execGit(ctx, tools, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		if code == exitFatal {
			return nil, fmt.Errorf("%w: %s", ErrNotARepo, dir)
		}
		return nil, err
	}
	return &Repo{Root: strings.TrimRight(string(out), "\n"), tools: tools}, nil
}

// git runs one git command in the repository and returns its trimmed
// stdout.
func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	out, _, err := execGit(ctx, r.tools, r.Root, nil, args...)
	return strings.TrimRight(string(out), "\n"), err
}

// scrubbedEnv is the environment git runs with. Inherited GIT_*
// redirection variables could point a command at some other
// repository's objects or index — a leak from whatever invoked
// dockhand — so they are dropped, and LC_ALL=C keeps diagnostic text
// stable. The user's own config survives on purpose: their authorship,
// remotes, and pager are the point. GIT_PAGER stays untouched here —
// execGit adds its own cat so internal plumbing never pages, while
// Pager can still ask what the user configured.
func scrubbedEnv() []string {
	// Alongside the redirection family: GIT_CONFIG_COUNT heads the
	// `git -c` injection vars (dropping the count makes git ignore the
	// KEY/VALUE pairs), GIT_DIFF_OPTS can silently strip a patch's
	// context lines (-u0), and GIT_EXTERNAL_DIFF is redirection-shaped
	// even though nothing here passes --ext-diff. GIT_CONFIG_GLOBAL
	// and _SYSTEM survive: users export those deliberately to relocate
	// their real config, which is authorship and pager — the point.
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_NAMESPACE": true, "GIT_OBJECT_DIRECTORY": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_COMMON_DIR": true,
		"GIT_CEILING_DIRECTORIES": true, "GIT_CONFIG_PARAMETERS": true,
		"GIT_CONFIG_COUNT": true, "GIT_DIFF_OPTS": true,
		"GIT_EXTERNAL_DIFF": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			env = append(env, kv)
		}
	}
	return append(env, "LC_ALL=C")
}

// Pager resolves the pager git itself would use for a diff. Real git
// consults pager.diff first — false means never page a diff, a string
// names the diff-specific pager, true falls through — and only then
// the general chain: $GIT_PAGER, core.pager, $PAGER, less, which
// `git var GIT_PAGER` runs (correctly even from behind a pipe).
func (r *Repo) Pager(ctx context.Context) string {
	if v, err := r.git(ctx, "config", "--get", "pager.diff"); err == nil {
		switch strings.ToLower(v) {
		case "false", "no", "off", "0":
			return "cat"
		case "true", "yes", "on", "1", "":
			// Page, with the general chain's pager.
		default:
			return v
		}
	}
	bin, err := r.tools.Find(tool.Git)
	if err != nil {
		return "cat"
	}
	out, _, err := tool.Output(ctx, bin, tool.Opts{Args: []string{"-C", r.Root, "var", "GIT_PAGER"}, Env: scrubbedEnv()})
	pager := strings.TrimSpace(string(out))
	if err != nil || pager == "" {
		return "cat"
	}
	return pager
}

// RunPager feeds content through a pager command the way git does: the
// value is a shell command, and LESS=FRX / LV=-c are defaulted exactly
// as git defaults them, so a bare less quits on a single screenful. A
// pager the user closes early is a shown diff, not an error.
func RunPager(ctx context.Context, pager string, content []byte, out, errOut io.Writer) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", pager)
	env := os.Environ()
	if os.Getenv("LESS") == "" {
		env = append(env, "LESS=FRX")
	}
	if os.Getenv("LV") == "" {
		env = append(env, "LV=-c")
	}
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(content)
	cmd.Stdout, cmd.Stderr = out, errOut
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return nil
	}
	return err
}

// execGit runs one git command with the scrubbed environment,
// surfacing the exit code for the callers that classify by it. A
// failure reads "git <subcommand>: <stderr>", with the exec error in
// place of a stderr git left empty; a finder miss passes through as
// the finder worded it.
func execGit(ctx context.Context, tools *tool.Finder, dir string, stdin []byte, args ...string) ([]byte, int, error) {
	bin, err := tools.Find(tool.Git)
	if err != nil {
		return nil, 0, err
	}
	// A nil []byte must stay a nil io.Reader: a typed nil reader would
	// be read from.
	var in io.Reader
	if stdin != nil {
		in = bytes.NewReader(stdin)
	}
	out, code, err := tool.Output(ctx, bin, tool.Opts{
		Args:  append([]string{"-C", dir}, args...),
		Env:   append(scrubbedEnv(), "GIT_PAGER=cat"),
		Stdin: in,
	})
	if err != nil {
		return nil, code, fmt.Errorf("git %s: %s", args[0], err) //nolint:errorlint // not wrapped: the exec error beneath carries the child's exit status, which ExitCode would take for a band
	}
	return out, 0, nil
}

// RevParse resolves a revision to its object name.
func (r *Repo) RevParse(ctx context.Context, rev string) (string, error) {
	return r.git(ctx, "rev-parse", "--verify", "--quiet", rev)
}

// HasBranch reports whether a local branch exists, by exact name.
func (r *Repo) HasBranch(ctx context.Context, name string) bool {
	_, err := r.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// PrimaryBranch names the repository's primary branch at its local
// position — the base new work forks from (D21). The remote's own
// declaration wins when a local branch tracks it; the conventional
// names are tried next; the current branch is the honest fallback.
// Never fetches: the local position is the answer, staleness included.
func (r *Repo) PrimaryBranch(ctx context.Context) (string, error) {
	if head, err := r.git(ctx, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		name := strings.TrimPrefix(head, "refs/remotes/origin/")
		if r.HasBranch(ctx, name) {
			return name, nil
		}
	}
	for _, name := range []string{"main", "master"} {
		if r.HasBranch(ctx, name) {
			return name, nil
		}
	}
	name, err := r.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git: no primary branch: origin/HEAD unset, no main or master, HEAD detached")
	}
	return name, nil
}

// BlobAt returns one file's bytes as a revision's tree records them.
// path is slash-separated, relative to the repository root.
func (r *Repo) BlobAt(ctx context.Context, rev, path string) ([]byte, error) {
	out, _, err := execGit(ctx, r.tools, r.Root, nil, "cat-file", "blob", rev+":"+path)
	if err != nil {
		return nil, fmt.Errorf("%s:%s: %w", rev, path, err)
	}
	return out, nil
}

// RelPath renders an absolute path relative to the repository root,
// slash-separated, refusing paths that escape it. Both sides are
// compared by their real locations: a checkout reached through a
// symlink (~/Source/ports linking the real clone) hands us symlinked
// paths, while git names its top level by where it really is, and the
// mismatch must not read as "outside the repository".
func (r *Repo) RelPath(path string) (string, error) {
	root := r.Root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("git: %s is outside the repository at %s", path, r.Root)
	}
	return filepath.ToSlash(rel), nil
}

// MintRequest describes one branch to mint: a single-file change on a
// base revision, with its commit message.
type MintRequest struct {
	Branch  string // local branch name to create; must not exist
	Base    string // revision the commit's parent resolves from
	Path    string // slash-separated repo-relative path of the file
	Content []byte // the file's new bytes
	Message string // the commit message, project format
}

// Mint creates a branch carrying one commit, entirely in the object
// database: hash the blob, graft it into the base tree, commit, then
// create the ref refusing to move an existing one. No worktree, no
// index, no checkout — the caller's HEAD and working tree are never
// touched (D21).
func (r *Repo) Mint(ctx context.Context, req MintRequest) (string, error) {
	if r.HasBranch(ctx, req.Branch) {
		return "", fmt.Errorf("%w: %s", ErrBranchExists, req.Branch)
	}
	baseCommit, err := r.RevParse(ctx, req.Base+"^{commit}")
	if err != nil {
		return "", err
	}
	tree, err := r.GraftTree(ctx, baseCommit, req.Path, req.Content)
	if err != nil {
		return "", err
	}
	commit, err := r.gitStdin(ctx, []byte(req.Message), "commit-tree", tree, "-p", baseCommit, "-F", "-")
	if err != nil {
		return "", err
	}
	// The empty old-value makes creation atomic: the ref must not
	// exist, so two concurrent mints cannot silently trade the name.
	if _, err := r.git(ctx, "update-ref", "refs/heads/"+req.Branch, commit, ""); err != nil {
		return "", err
	}
	return commit, nil
}

// GraftTree writes a tree equal to base's but with the file at path
// holding content: the object-database half of a mint, shared with
// diffing — "diff the trees" and "commit the tree" are the same
// construction with different last steps. The objects written are
// ordinary gc fodder until something references them. hash-object
// without --path bypasses gitattributes clean filters, so the blob is
// content's exact bytes — the assumption is that no filter governs
// Portfiles, which holds in macports-ports.
func (r *Repo) GraftTree(ctx context.Context, base, path string, content []byte) (string, error) {
	baseTree, err := r.RevParse(ctx, base+"^{tree}")
	if err != nil {
		return "", err
	}
	blob, err := r.gitStdin(ctx, content, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return r.graft(ctx, baseTree, strings.Split(path, "/"), blob)
}

// DiffTrees renders the patch between two tree-ishes. diff-tree is
// plumbing: output ignores the user's diff.* config, carries the
// standard a/ and b/ path prefixes, and never colors — so what it
// prints is what `git apply` accepts.
func (r *Repo) DiffTrees(ctx context.Context, a, b string) ([]byte, error) {
	out, _, err := execGit(ctx, r.tools, r.Root, nil, "diff-tree", "-p", a, b)
	return out, err
}

// gitStdin runs one git command with the given stdin.
func (r *Repo) gitStdin(ctx context.Context, stdin []byte, args ...string) (string, error) {
	out, _, err := execGit(ctx, r.tools, r.Root, stdin, args...)
	return strings.TrimRight(string(out), "\n"), err
}

// graft returns a tree equal to tree but with the blob at the given
// path. Every level along the path must already exist: dockhand
// replaces files, it does not invent directories, and a missing level
// means the path does not name what the caller thought. Entries keep
// their positions, so the rebuilt trees stay in git's canonical order.
func (r *Repo) graft(ctx context.Context, tree string, parts []string, blob string) (string, error) {
	listing, err := r.git(ctx, "ls-tree", "-z", tree)
	if err != nil {
		return "", err
	}
	entries := strings.Split(strings.TrimRight(listing, "\x00"), "\x00")
	found := false
	for i, e := range entries {
		meta, name, ok := strings.Cut(e, "\t")
		if !ok || name != parts[0] {
			continue
		}
		fields := strings.Fields(meta) // mode, type, sha
		if len(parts) == 1 {
			if fields[1] != "blob" {
				return "", fmt.Errorf("git: %s is a %s, not a file", name, fields[1])
			}
			entries[i] = fields[0] + " blob " + blob + "\t" + name
		} else {
			if fields[1] != "tree" {
				return "", fmt.Errorf("git: %s is a %s, not a directory", name, fields[1])
			}
			sub, err := r.graft(ctx, fields[2], parts[1:], blob)
			if err != nil {
				return "", err
			}
			entries[i] = fields[0] + " tree " + sub + "\t" + name
		}
		found = true
		break
	}
	if !found {
		return "", fmt.Errorf("git: no entry %q in tree %s", parts[0], tree)
	}
	return r.gitStdin(ctx, []byte(strings.Join(entries, "\x00")+"\x00"), "mktree", "-z")
}
