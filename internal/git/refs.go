package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/tool"
)

// Branches lists local branches under a slash-terminated prefix, by
// exact ref-namespace match — for-each-ref patterns are path-wise, so
// "dockhand/" cannot match "dockhand-hidden".
func (r *Repo) Branches(ctx context.Context, prefix string) ([]string, error) {
	out, err := r.git(ctx, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+prefix)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// Materialize writes one path's tree at a revision into dest on the
// filesystem, via git archive — the object database is the source, so
// the working tree's state is irrelevant. dest must exist. The archive
// passes through memory rather than a pipe: a portdir is a Portfile
// and its files/, and two one-shot commands are simpler than a
// hand-built pipeline that must reap both halves on every failure.
func (r *Repo) Materialize(ctx context.Context, rev, path, dest string) error {
	bin, err := r.tools.Find(tool.Git)
	if err != nil {
		return err
	}
	tarBin, err := r.tools.Find(tool.Tar)
	if err != nil {
		return err
	}
	archive, _, err := tool.Output(ctx, bin, tool.Opts{
		Args: []string{"-C", r.Root, "archive", rev, "--", path},
		Env:  append(scrubbedEnv(), "GIT_PAGER=cat"),
	})
	if err != nil {
		return fmt.Errorf("git archive %s -- %s: %s", rev, path, stderrOf(err))
	}
	if _, _, err := tool.Output(ctx, tarBin, tool.Opts{Args: []string{"-x", "-C", dest}, Stdin: bytes.NewReader(archive)}); err != nil {
		return fmt.Errorf("extracting archive of %s: %s", path, stderrOf(err))
	}
	return nil
}

// stderrOf is what a failed command wrote to stderr, and nothing else:
// Materialize's messages carry the tool's own words, or end bare when
// it said nothing, as they always have.
func stderrOf(err error) string {
	var f *tool.Failure
	if errors.As(err, &f) {
		return f.Stderr
	}
	return ""
}

// RevList returns up to n commit shas reachable from rev, newest
// first — the tip is element zero, so an element's index is its
// distance behind the tip.
func (r *Repo) RevList(ctx context.Context, rev string, n int) ([]string, error) {
	out, err := r.git(ctx, "rev-list", fmt.Sprintf("-%d", n), rev)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// MergeBase returns the merge base of two revisions.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	return r.git(ctx, "merge-base", a, b)
}

// IsAncestor reports whether a is an ancestor of b.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) bool {
	_, _, err := execGit(ctx, r.tools, r.Root, nil, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// FormerTips is the set of shas a branch's reflog records it pointing
// at — the amended-away history ancestry cannot see. Best-effort: a
// branch with no reflog reports nothing, never an error, because the
// reflog is corroborating evidence, not the record.
func (r *Repo) FormerTips(ctx context.Context, branch string) map[string]bool {
	out, err := r.git(ctx, "reflog", "show", "--format=%H", branch)
	if err != nil {
		return nil
	}
	tips := map[string]bool{}
	for line := range strings.Lines(out) {
		if sha := strings.TrimSpace(line); sha != "" {
			tips[sha] = true
		}
	}
	return tips
}

// DiffNames lists the paths that differ between two revisions.
func (r *Repo) DiffNames(ctx context.Context, a, b string) ([]string, error) {
	out, err := r.git(ctx, "diff-tree", "-r", "--name-only", a, b)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// Subject is one commit's subject line.
func (r *Repo) Subject(ctx context.Context, sha string) (string, error) {
	return r.git(ctx, "log", "-1", "--format=%s", sha)
}

// CommittedAt is when a commit was made, in UTC.
//
// The committer date and not the author's: what a record wants to know
// is how old the tree a change was built against actually is, and a
// rebased or cherry-picked commit carries an author date from before
// the tree it now sits on.
//
// Strict RFC 3339 (%cI) rather than a unix stamp, because the parse is
// then the format's own and a value git could not produce cannot be
// silently read as the epoch.
func (r *Repo) CommittedAt(ctx context.Context, rev string) (time.Time, error) {
	out, err := r.git(ctx, "log", "-1", "--format=%cI", rev)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if err != nil {
		return time.Time{}, fmt.Errorf("git: reading the commit date of %s: %w", Abbrev(rev), err)
	}
	return t.UTC(), nil
}

// OwnCommits lists the commits reachable from rev but not from base —
// a branch's own work, the commits whose notes die with it.
func (r *Repo) OwnCommits(ctx context.Context, rev, base string) ([]string, error) {
	out, err := r.git(ctx, "rev-list", rev, "--not", base)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// DeleteBranch removes a local branch regardless of merge state —
// deliberately the one porcelain call here: branch -D owns the
// configuration-section and reflog cleanup that a raw update-ref -d
// would leave behind.
func (r *Repo) DeleteBranch(ctx context.Context, name string) error {
	_, err := r.git(ctx, "branch", "-D", name)
	return err
}
