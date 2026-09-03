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
	"slices"
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
		return nil, code, fmt.Errorf("git %s: %s", args[0], err) //nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
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
//
// This is the read a cohort planner wants and the reason no separate
// Blob exists beside it: a member's Portfile as the branch tip has it,
// never as the worktree happens to hold it.
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

// ErrTipMoved reports an extend refused because the branch is no
// longer where the caller last saw it: something else has already
// added to it, and committing on the tip that was read would either
// discard that work or bury it.
var ErrTipMoved = errors.New("git: branch tip moved")

// File is one file's place in a commit: the bytes to record at a path,
// or that path's removal. Path is slash-separated and relative to the
// repository root, the way a tree names it.
type File struct {
	Path    string // slash-separated, repo-relative; no empty segments
	Content []byte // the file's new bytes; unread when Delete is set
	Delete  bool   // remove the path instead of writing to it
}

// Commit is one commit's worth of work: the files it moves, and the
// message it carries. Message reaches commit-tree verbatim — git
// appends nothing to it, so a trailing newline here is a trailing
// newline in the object, and two messages differing by one are two
// different commits.
type Commit struct {
	Files   []File
	Message string
}

// MintRequest describes one branch to mint: a chain of commits on a
// base revision. The chain is plural because one change can need more
// than one commit to state itself — a cohort moves several ports, and
// each port's move is its own commit with its own message — and the
// ordinary single-file change is the chain of one.
type MintRequest struct {
	Branch  string   // local branch name to create; must not exist
	Base    string   // revision the first commit's parent resolves from
	Commits []Commit // oldest first; the last one is where the branch lands
}

// Mint creates a branch carrying a chain of commits, entirely in the
// object database: hash the blobs, graft them into the parent's tree,
// commit, chain the next commit onto that one, then create the ref
// refusing to move an existing one. No worktree, no index, no
// checkout — the caller's HEAD and working tree are never touched
// (D21).
func (r *Repo) Mint(ctx context.Context, req MintRequest) (string, error) {
	if err := checkCommits(req.Branch, req.Commits); err != nil {
		return "", err
	}
	if r.HasBranch(ctx, req.Branch) {
		return "", fmt.Errorf("%w: %s", ErrBranchExists, req.Branch)
	}
	commit, err := r.RevParse(ctx, req.Base+"^{commit}")
	if err != nil {
		return "", err
	}
	for _, c := range req.Commits {
		if commit, err = r.commit(ctx, commit, c); err != nil {
			return "", err
		}
	}
	// The empty old-value makes creation atomic: the ref must not
	// exist, so two concurrent mints cannot silently trade the name.
	if _, err := r.git(ctx, "update-ref", "refs/heads/"+req.Branch, commit, ""); err != nil {
		return "", err
	}
	return commit, nil
}

// Extend adds one commit to a branch that already exists, refusing
// unless the branch is still at expectedTip. Two sessions extending
// one branch must not both win: the tip is read first so the refusal
// can say where the branch actually is, and update-ref is handed the
// old value so the swap itself is atomic — the window between the read
// and the write belongs to git, not to dockhand.
func (r *Repo) Extend(ctx context.Context, branch, expectedTip string, c Commit) (string, error) {
	if err := checkCommits(branch, []Commit{c}); err != nil {
		return "", err
	}
	ref := "refs/heads/" + branch
	tip, err := r.RevParse(ctx, ref)
	if err != nil {
		return "", err
	}
	if tip != expectedTip {
		return "", fmt.Errorf("%w: %s is at %s, not %s", ErrTipMoved, branch, Abbrev(tip), Abbrev(expectedTip))
	}
	commit, err := r.commit(ctx, tip, c)
	if err != nil {
		return "", err
	}
	if _, err := r.git(ctx, "update-ref", ref, commit, expectedTip); err != nil {
		// A lost lease and a broken repository leave update-ref with
		// the same exit code, and this package classifies by code
		// rather than by message text. So the branch is asked where it
		// is now: a tip that has moved says so itself, and anything
		// else is git's own failure, handed on as it came.
		if now, rerr := r.RevParse(ctx, ref); rerr == nil && now != expectedTip {
			return "", fmt.Errorf("%w: %s is at %s, not %s", ErrTipMoved, branch, Abbrev(now), Abbrev(expectedTip))
		}
		return "", err
	}
	return commit, nil
}

// commit writes one commit on parent: the files grafted into parent's
// tree, then commit-tree with the message as its bytes stand.
func (r *Repo) commit(ctx context.Context, parent string, c Commit) (string, error) {
	tree, err := r.GraftTree(ctx, parent, c.Files)
	if err != nil {
		return "", err
	}
	return r.gitStdin(ctx, []byte(c.Message), "commit-tree", tree, "-p", parent, "-F", "-")
}

// checkCommits refuses a chain that would record nothing. A branch is
// at least one commit, and a commit is at least one file: a commit with
// no files grafts the parent's tree unchanged, so commit-tree records
// that same tree again and the branch lands on an empty commit.
//
// Both refusals belong here because this package is what promises what
// a minted branch contains. Neither is reachable today — the engine
// refuses a no-op at verdict.NothingToMint, before a request is ever
// built — but Extend has no such caller in front of it at all, and a
// guard that stops one level short of the thing it defends is the
// shape a later plural walks into.
func checkCommits(branch string, commits []Commit) error {
	if len(commits) == 0 {
		return fmt.Errorf("git: %s: a branch is at least one commit", branch)
	}
	for _, c := range commits {
		if len(c.Files) == 0 {
			return fmt.Errorf("git: %s: a commit is at least one file", branch)
		}
	}
	return nil
}

// GraftTree writes a tree equal to base's but with every file applied:
// the object-database half of a mint, shared with diffing — "diff the
// trees" and "commit the tree" are the same construction with
// different last steps. The objects written are ordinary gc fodder
// until something references them. hash-object without --path bypasses
// gitattributes clean filters, so a blob is the content's exact bytes —
// the assumption is that no filter governs Portfiles, which holds in
// macports-ports.
//
// The textbook way to rewrite several paths at once is a temporary
// index — read-tree, update-index --index-info, write-tree — and that
// road is deliberately closed: scrubbedEnv drops GIT_INDEX_FILE and
// Mint promises no index. ls-tree and mktree are the whole toolkit, so
// the plural case is one walk of the tree rather than one walk per
// file.
func (r *Repo) GraftTree(ctx context.Context, base string, files []File) (string, error) {
	baseTree, err := r.RevParse(ctx, base+"^{tree}")
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return baseTree, nil
	}
	grafts := make([]graft, 0, len(files))
	for _, f := range files {
		// An empty segment — an empty path, a leading or doubled slash —
		// would look for an entry with no name, and on an insert would
		// write one.
		parts := strings.Split(f.Path, "/")
		if slices.Contains(parts, "") {
			return "", fmt.Errorf("git: %q does not name a path in a tree", f.Path)
		}
		grafts = append(grafts, graft{parts: parts, content: f.Content, remove: f.Delete})
	}
	// Sorted by segment, not by path: git's tree order is not the
	// string order — "a-b" sorts before "a/b" as text, which would
	// split the "a" group in two and read that directory twice.
	slices.SortFunc(grafts, func(a, b graft) int { return slices.Compare(a.parts, b.parts) })
	for i := 1; i < len(grafts); i++ {
		if slices.Equal(grafts[i-1].parts, grafts[i].parts) {
			return "", fmt.Errorf("git: %s named twice in one tree", strings.Join(grafts[i].parts, "/"))
		}
	}
	// The two refusals above are the ones a caller earns without reading
	// the tree — a path that does not name a path, and a path named
	// twice — and both come before a single blob is written. The walk's
	// own refusals cannot: a directory that is not there, or a name that
	// is a tree where a blob was expected, is discovered below with the
	// blobs already written, and they stay the gc fodder every unmade
	// tree here already leaves.
	for i := range grafts {
		if grafts[i].remove {
			continue
		}
		if grafts[i].blob, err = r.gitStdin(ctx, grafts[i].content, "hash-object", "-w", "--stdin"); err != nil {
			return "", err
		}
	}
	tree, _, err := r.graft(ctx, baseTree, grafts)
	return tree, err
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

// graft is one file's remaining journey down the tree: the path
// segments still to be descended, and what to do once they run out.
type graft struct {
	parts   []string
	content []byte // the file's bytes, until hash-object has taken them
	blob    string // the written blob; empty when remove is set
	remove  bool
}

// newFileMode is the mode a file git has not seen before is recorded
// at. A path already in the tree keeps the mode the tree gives it — an
// executable script survives a rewrite as one — so this is only ever
// the mode of an insert.
const newFileMode = "100644"

// graft returns a tree equal to tree but with every graft applied,
// alongside the number of entries the new tree holds so a caller can
// drop a directory whose last file has just left it.
//
// The grafts arrive sorted by segment, and that is what makes this one
// pass: everything sharing a first segment is contiguous, so each
// directory is read once with ls-tree and written once with mktree
// however many files fall under it.
//
// A file may be new; a directory may not. dockhand replaces files and
// adds them beside their siblings, but a missing directory means the
// path does not name what the caller thought, so the walk refuses
// rather than inventing one.
//
// Order is mktree's business, not the walk's: it normalizes entry
// order itself, so an insert can simply be appended. What the walk
// must not do is hand mktree the same name twice — mktree takes that
// silently and writes a tree fsck calls corrupt — which is why
// GraftTree refuses a repeated path before any of this runs.
func (r *Repo) graft(ctx context.Context, tree string, grafts []graft) (string, int, error) {
	listing, err := r.git(ctx, "ls-tree", "-z", tree)
	if err != nil {
		return "", 0, err
	}
	// An empty tree lists as zero bytes, and splitting those yields one
	// empty record rather than none.
	var entries []string
	if listing != "" {
		entries = strings.Split(strings.TrimRight(listing, "\x00"), "\x00")
	}
	at := make(map[string]int, len(entries))
	for i, e := range entries {
		if _, name, ok := strings.Cut(e, "\t"); ok {
			at[name] = i
		}
	}
	gone := make([]bool, len(entries))
	var added []string
	for i := 0; i < len(grafts); {
		name := grafts[i].parts[0]
		j := i + 1
		for j < len(grafts) && grafts[j].parts[0] == name {
			j++
		}
		group := grafts[i:j]
		i = j

		idx, present := at[name]
		var fields []string // mode, type, sha
		if present {
			meta, _, _ := strings.Cut(entries[idx], "\t")
			fields = strings.Fields(meta)
		}
		if len(group[0].parts) == 1 {
			// A path that is a file cannot also be a directory, and
			// sorting puts the shorter one first, so the group holding
			// it holds nothing else.
			if len(group) > 1 {
				return "", 0, fmt.Errorf("git: %s is named as both a file and a directory", name)
			}
			if !present {
				if group[0].remove {
					return "", 0, fmt.Errorf("git: no entry %q in tree %s", name, tree)
				}
				added = append(added, newFileMode+" blob "+group[0].blob+"\t"+name)
				continue
			}
			if fields[1] != "blob" {
				return "", 0, fmt.Errorf("git: %s is a %s, not a file", name, fields[1])
			}
			if group[0].remove {
				gone[idx] = true
				continue
			}
			entries[idx] = fields[0] + " blob " + group[0].blob + "\t" + name
			continue
		}
		if !present {
			return "", 0, fmt.Errorf("git: no entry %q in tree %s", name, tree)
		}
		if fields[1] != "tree" {
			return "", 0, fmt.Errorf("git: %s is a %s, not a directory", name, fields[1])
		}
		below := make([]graft, len(group))
		for k, g := range group {
			below[k] = graft{parts: g.parts[1:], blob: g.blob, remove: g.remove}
		}
		sub, held, err := r.graft(ctx, fields[2], below)
		if err != nil {
			return "", 0, err
		}
		// git records no empty directories: the delete that took the
		// last file out of one takes the directory with it.
		if held == 0 {
			gone[idx] = true
			continue
		}
		entries[idx] = fields[0] + " tree " + sub + "\t" + name
	}
	kept := make([]string, 0, len(entries)+len(added))
	for i, e := range entries {
		if !gone[i] {
			kept = append(kept, e)
		}
	}
	kept = append(kept, added...)
	// mktree reads its records from stdin, so a tree with nothing left
	// in it is an empty stdin — not the one empty record a join of
	// nothing would write.
	records := []byte{}
	if len(kept) > 0 {
		records = []byte(strings.Join(kept, "\x00") + "\x00")
	}
	sha, err := r.gitStdin(ctx, records, "mktree", "-z")
	if err != nil {
		return "", 0, err
	}
	return sha, len(kept), nil
}
