package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/scratch"
)

// HistoryCommit is one commit of a branch, as tidy and submit read it.
type HistoryCommit struct {
	ID      string
	Parents []string
	Tree    string
	Message string
	Author  Signature
	// Paths are the files it changes against its first parent.
	Paths []string
}

// Merge reports whether the commit has more than one parent.
func (c HistoryCommit) Merge() bool { return len(c.Parents) > 1 }

// Subject is the message's first line.
func (c HistoryCommit) Subject() string {
	subject, _, _ := strings.Cut(c.Message, "\n")
	return strings.TrimSpace(subject)
}

// History lists the commits head has and base does not, oldest first.
func (r *Repository) History(ctx context.Context, base, head string) ([]HistoryCommit, error) {
	if !ValidObjectID(base) || !ValidObjectID(head) {
		return nil, fmt.Errorf("git: literal commit objects are required")
	}
	out, err := r.output(ctx, "rev-list", "--reverse", "--topo-order", "--parents", base+".."+head, "--")
	if err != nil {
		return nil, err
	}
	var commits []HistoryCommit
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		commit := HistoryCommit{ID: fields[0], Parents: fields[1:]}
		info, err := r.output(ctx, "show", "-s", "--format=%T%x00%an%x00%ae%x00%at%x00%B", commit.ID, "--")
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(string(info), "\x00", 5)
		if len(parts) != 5 {
			return nil, fmt.Errorf("git: unreadable commit %s", commit.ID)
		}
		seconds, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git: commit %s has an unreadable author date", commit.ID)
		}
		commit.Tree, commit.Message = parts[0], strings.TrimRight(parts[4], "\n")+"\n"
		commit.Author = Signature{Name: parts[1], Email: parts[2], When: time.Unix(seconds, 0).UTC()}
		parent := base
		if len(commit.Parents) > 0 {
			parent = commit.Parents[0]
		}
		if commit.Paths, err = r.ChangedPaths(ctx, parent, commit.ID); err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

// ComposeTree is onto's tree with each of paths as it is in from: its
// entry there, whatever its mode, or removed when from lacks it. Nothing
// but objects is written; a private index does the work.
func (r *Repository) ComposeTree(ctx context.Context, onto, from string, paths []string) (string, error) {
	if !ValidObjectID(onto) || !ValidObjectID(from) {
		return "", fmt.Errorf("git: literal trees are required")
	}
	directory, err := scratch.Dir("compose-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(directory, "index")}
	if _, err := r.run(ctx, nil, env, "read-tree", onto); err != nil {
		return "", err
	}
	var input strings.Builder
	for _, name := range paths {
		if !snapshotPath(name) {
			return "", fmt.Errorf("git: invalid path %q", name)
		}
		out, err := r.output(ctx, "ls-tree", "-z", from, "--", name)
		if err != nil {
			return "", err
		}
		entry := strings.TrimSuffix(string(out), "\x00")
		if entry == "" {
			// A mode of 0 removes the path from the index.
			fmt.Fprintf(&input, "0 %s\t%s\x00", strings.Repeat("0", len(from)), name)
			continue
		}
		meta, entryPath, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || entryPath != name || len(fields) != 3 || fields[1] == "tree" {
			return "", fmt.Errorf("git: %s is not a file in %s", name, from)
		}
		fmt.Fprintf(&input, "%s %s\t%s\x00", fields[0], fields[2], name)
	}
	if _, err := r.run(ctx, []byte(input.String()), env, "update-index", "-z", "--index-info"); err != nil {
		return "", err
	}
	out, err := r.run(ctx, nil, env, "write-tree")
	return objectResult(out, err)
}

// SetIndex makes the index hold a tree and leaves the working files as
// they are: what restores an index recorded earlier. It rewrites only the
// entries that differ, so a sparse checkout's entries outside its paths
// keep the skip-worktree bits a wholesale read-tree would drop, which
// would make every port outside them read as deleted.
func (r *Repository) SetIndex(ctx context.Context, tree string) error {
	if !ValidObjectID(tree) {
		return fmt.Errorf("git: a literal tree is required")
	}
	current, err := r.IndexTree(ctx)
	if err != nil {
		return err
	}
	out, err := r.output(ctx, "diff-tree", "-r", "-z", "--no-renames", current, tree)
	if err != nil {
		return err
	}
	// Each change is ":<old mode> <new mode> <old id> <new id> <status>",
	// then its path, NUL-terminated.
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var info bytes.Buffer
	for i := 0; i+1 < len(fields); i += 2 {
		change := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(change) != 5 {
			return fmt.Errorf("git: unreadable change %q", fields[i])
		}
		mode, object := change[1], change[3]
		if strings.HasPrefix(change[4], "D") {
			mode, object = "0", strings.Repeat("0", len(object))
		}
		fmt.Fprintf(&info, "%s %s\t%s\x00", mode, object, fields[i+1])
	}
	if info.Len() == 0 {
		return nil
	}
	_, err = r.run(ctx, info.Bytes(), nil, "update-index", "-z", "--index-info")
	return err
}

// ResetIndex makes the index match HEAD and leaves the working files as
// they are: what follows moving a checked-out branch to a commit whose tree
// the working files already hold.
func (r *Repository) ResetIndex(ctx context.Context) error {
	_, err := r.output(ctx, "reset", "--quiet", "--mixed")
	return err
}

// MoveCheckout moves the branch this checkout has out, from commit from to
// commit to, with its index and working files, as git reset --keep does:
// the files that differ between the two are rewritten, within the
// checkout's sparse paths, and a local change to one of them, or an
// untracked file in the way, stops it with nothing changed. It is what
// undoes a rebase, whose files are the rebased commit's.
func (r *Repository) MoveCheckout(ctx context.Context, from, to string) error {
	if !ValidObjectID(from) || !ValidObjectID(to) {
		return fmt.Errorf("git: literal commits are required")
	}
	branch, head, err := r.checkoutHead(ctx)
	if err != nil {
		return err
	}
	if branch == "" {
		return fmt.Errorf("git: no branch is checked out")
	}
	if head != from {
		return fmt.Errorf("git: the checkout is at %s, not %s", head, from)
	}
	_, err = r.output(ctx, "reset", "--quiet", "--keep", to)
	return err
}

// FileBlobs maps each of paths that tree holds to its object ID; a path
// the tree lacks is absent from the map.
func (r *Repository) FileBlobs(ctx context.Context, tree string, paths []string) (map[string]string, error) {
	blobs := map[string]string{}
	if len(paths) == 0 {
		return blobs, nil
	}
	if !ValidObjectID(tree) {
		return nil, fmt.Errorf("git: a literal tree is required")
	}
	out, err := r.output(ctx, append([]string{"ls-tree", "-z", "--full-tree", tree, "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	for entry := range strings.SplitSeq(string(out), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if ok && len(fields) == 3 && fields[1] == "blob" {
			blobs[name] = fields[2]
		}
	}
	return blobs, nil
}

// Bundle writes a Git bundle at path holding commit under ref, less what
// the excluded commits already hold: a receiver needs those first.
func (r *Repository) Bundle(ctx context.Context, path, ref, commit string, exclude ...string) error {
	if !ValidRefName(ref) || !strings.HasPrefix(ref, "refs/") || !ValidObjectID(commit) {
		return fmt.Errorf("git: invalid bundle ref %q or commit %q", ref, commit)
	}
	current, err := r.ReadRef(ctx, ref)
	if err != nil {
		return err
	}
	if err := r.UpdateRefs(ctx, []RefChange{{Name: ref, Expected: current, Desired: RefValue{Exists: true, Object: commit}}}); err != nil {
		return err
	}
	args := []string{"bundle", "create", "--quiet", path, ref}
	for _, commit := range exclude {
		if !ValidObjectID(commit) {
			return fmt.Errorf("git: invalid excluded commit %q", commit)
		}
		args = append(args, "^"+commit)
	}
	_, err = r.output(ctx, args...)
	return err
}

// ErrRebaseConflict reports a rebase that stopped on conflicts; the
// checkout was put back as it was.
var ErrRebaseConflict = errors.New("git: the rebase stopped on conflicts")

// Replay replays the commits head has above upstream onto onto, as git
// rebase does, without touching a checkout or a ref, and returns the
// replayed head. Each commit's change is merged onto the replayed commit
// before it (git merge-tree, with the commit's parent as the merge base),
// and committed with its own author, date, and message, and the committer
// given. A commit whose change onto already has is dropped, as git rebase
// drops it; one that was empty to begin with is kept. A conflict is
// ErrRebaseConflict, naming the files, with nothing written that anything
// refers to. A merge commit is refused. It needs Git 2.40 or newer.
func (r *Repository) Replay(ctx context.Context, onto, upstream, head string, committer Signature) (string, error) {
	if !ValidObjectID(onto) || !ValidObjectID(upstream) || !ValidObjectID(head) {
		return "", fmt.Errorf("git: invalid replay of %q onto %q", head, onto)
	}
	history, err := r.History(ctx, upstream, head)
	if err != nil {
		return "", err
	}
	parent := onto
	for _, commit := range history {
		if commit.Merge() || len(commit.Parents) == 0 {
			return "", fmt.Errorf("git: %.12s is a merge commit, which dockhand doesn't replay; rebase it by hand with git rebase", commit.ID)
		}
		out, status, err := r.runStatus(ctx, "merge-tree", "--write-tree", "--name-only", "-z", "--no-messages", "--merge-base", commit.Parents[0], parent, commit.ID)
		if err != nil {
			if status == 129 {
				return "", fmt.Errorf("git: replaying commits needs Git %s or newer: %w", MinimumVersion, err)
			}
			return "", err
		}
		fields := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
		tree := fields[0]
		if status == 1 {
			var paths []string
			for _, path := range fields[1:] {
				if path != "" && !slices.Contains(paths, path) {
					paths = append(paths, path)
				}
			}
			return "", fmt.Errorf("%w in %s", ErrRebaseConflict, strings.Join(paths, ", "))
		}
		if !ValidObjectID(tree) {
			return "", fmt.Errorf("git: merge-tree returned %q", tree)
		}
		trees, err := r.CommitTrees(ctx, []string{parent, commit.Parents[0]})
		if err != nil {
			return "", err
		}
		// A change onto already has leaves nothing to commit; a commit
		// that changed nothing to begin with is kept as it was.
		if tree == trees[parent] && commit.Tree != trees[commit.Parents[0]] {
			continue
		}
		if parent, err = r.WriteCommit(ctx, Commit{Tree: tree, Parents: []string{parent}, Message: commit.Message, Author: commit.Author, Committer: committer}); err != nil {
			return "", err
		}
	}
	return parent, nil
}

// Rebase replays the commits branch has above upstream onto onto, in this
// checkout, which must have branch checked out. A rebase that stops on
// conflicts is aborted, leaving everything as it was, and the error names
// the conflicting files.
func (r *Repository) Rebase(ctx context.Context, onto, upstream, branch string) error {
	if !ValidObjectID(onto) || !ValidObjectID(upstream) || !ValidBranchName(branch) {
		return fmt.Errorf("git: invalid rebase of %q onto %q", branch, onto)
	}
	_, err := r.output(ctx, "rebase", "--quiet", "--no-autosquash", "--no-update-refs", "--onto", onto, upstream, branch)
	if err == nil {
		return nil
	}
	conflicts, conflictErr := r.Conflicts(context.WithoutCancel(ctx))
	_, abortErr := r.output(context.WithoutCancel(ctx), "rebase", "--abort")
	if conflictErr == nil && len(conflicts) > 0 {
		return errors.Join(fmt.Errorf("%w in %s", ErrRebaseConflict, strings.Join(conflicts, ", ")), abortErr)
	}
	return errors.Join(err, abortErr)
}
