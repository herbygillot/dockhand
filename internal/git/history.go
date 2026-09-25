package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// ResetIndex makes the index match HEAD and leaves the working files as
// they are: what follows moving a checked-out branch to a commit whose tree
// the working files already hold.
func (r *Repository) ResetIndex(ctx context.Context) error {
	_, err := r.output(ctx, "reset", "--quiet", "--mixed")
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
