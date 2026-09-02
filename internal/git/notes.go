package git

import (
	"context"
	"errors"
	"fmt"
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
	out, code, err := execGit(ctx, r.tools, r.Root, nil, "notes", "--ref="+ref, "show", sha)
	if err != nil {
		if code == 1 || code == exitFatal {
			return nil, fmt.Errorf("%w: %s", ErrNoNote, sha)
		}
		return nil, err
	}
	return out, nil
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

// NoteRemove deletes a commit's note under the ref; a commit with no
// note is fine — removal is idempotent.
func (r *Repo) NoteRemove(ctx context.Context, ref, sha string) error {
	_, err := r.git(ctx, "notes", "--ref="+ref, "remove", "--ignore-missing", sha)
	return err
}
