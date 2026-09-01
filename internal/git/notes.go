package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// VerifyNotesRef is the notes namespace holding verification records,
// keyed by commit sha. Notes are local by decision (D21): a note
// answers "ready to promote?", a question only this machine asks, and
// dockhand never pushes the ref.
const VerifyNotesRef = "dockhand/verify"

// ErrNoNote reports a commit with no note under the ref.
var ErrNoNote = errors.New("git: no note for this commit")

// NoteWrite records content as the note on a commit, replacing any
// previous note there: the note is the commit's current verification
// record, and its history lives in the notes ref's own commits.
func (r *Repo) NoteWrite(ctx context.Context, ref, sha string, content []byte) error {
	_, err := r.gitStdin(ctx, content, "notes", "--ref="+ref, "add", "-f", "-F", "-", sha)
	return err
}

// NoteRead returns a commit's note under the ref, ErrNoNote when there
// is none.
func (r *Repo) NoteRead(ctx context.Context, ref, sha string) ([]byte, error) {
	out, code, err := execGit(ctx, r.Root, nil, "notes", "--ref="+ref, "show", sha)
	if err != nil {
		if code == 1 || code == exitFatal {
			return nil, fmt.Errorf("%w: %s", ErrNoNote, sha)
		}
		return nil, err
	}
	return out, nil
}

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
// the working tree's state is irrelevant. dest must exist.
func (r *Repo) Materialize(ctx context.Context, rev, path, dest string) error {
	archive := exec.CommandContext(ctx, "git", "-C", r.Root, "archive", rev, "--", path)
	archive.Env = append(scrubbedEnv(), "GIT_PAGER=cat")
	untar := exec.CommandContext(ctx, "tar", "-x", "-C", dest)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	var aerr, terr strings.Builder
	archive.Stderr, untar.Stderr = &aerr, &terr
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		_ = untar.Wait()
		return fmt.Errorf("git archive %s -- %s: %s", rev, path, strings.TrimSpace(aerr.String()))
	}
	if err := untar.Wait(); err != nil {
		return fmt.Errorf("extracting archive of %s: %s", path, strings.TrimSpace(terr.String()))
	}
	return nil
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

// NotesList returns the shas of every commit annotated under the ref.
func (r *Repo) NotesList(ctx context.Context, ref string) ([]string, error) {
	out, err := r.git(ctx, "notes", "--ref="+ref, "list")
	if err != nil || out == "" {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(out, "\n") {
		if _, sha, ok := strings.Cut(line, " "); ok {
			shas = append(shas, sha)
		}
	}
	return shas, nil
}

// MergeBase returns the merge base of two revisions.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	return r.git(ctx, "merge-base", a, b)
}

// IsAncestor reports whether a is an ancestor of b.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) bool {
	_, _, err := execGit(ctx, r.Root, nil, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// DiffNames lists the paths that differ between two revisions.
func (r *Repo) DiffNames(ctx context.Context, a, b string) ([]string, error) {
	out, err := r.git(ctx, "diff-tree", "-r", "--name-only", a, b)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// Remotes returns each remote's fetch URL.
func (r *Repo) Remotes(ctx context.Context) (map[string]string, error) {
	out, err := r.git(ctx, "remote", "-v")
	if err != nil {
		return nil, err
	}
	remotes := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == "(fetch)" {
			remotes[fields[0]] = fields[1]
		}
	}
	return remotes, nil
}

// Push pushes a branch to a remote under its own name, recording the
// upstream tracking configuration — the push config a later PR lookup
// derives the head ref from (D21), and what makes a bare `git push`
// work for the human who takes the branch over.
func (r *Repo) Push(ctx context.Context, remote, branch string) error {
	_, err := r.git(ctx, "push", "-u", remote, branch)
	return err
}

// PushDelete removes a branch from a remote — the undo of Push, for
// cleanup that follows a merged PR. Deleting a ref that is already
// gone is an error from git, and callers who want idempotence should
// treat it as advisory.
func (r *Repo) PushDelete(ctx context.Context, remote, branch string) error {
	_, err := r.git(ctx, "push", remote, "--delete", branch)
	return err
}

// TrackedRemote names the remote a branch tracks, "" when none.
func (r *Repo) TrackedRemote(ctx context.Context, branch string) string {
	out, _ := r.git(ctx, "config", "--get", "branch."+branch+".remote")
	return out
}

// NoteRemove deletes a commit's note under the ref; a commit with no
// note is fine — removal is idempotent.
func (r *Repo) NoteRemove(ctx context.Context, ref, sha string) error {
	_, err := r.git(ctx, "notes", "--ref="+ref, "remove", "--ignore-missing", sha)
	return err
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
