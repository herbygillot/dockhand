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

// OutcomeNotesRef is the notes namespace holding the audit rows: what
// dockhand published, and what became of it. Keyed by the mint sha,
// append-only, and local by the same decision as the verification
// notes — dockhand never pushes either ref.
//
// It is a second namespace rather than more keys on the first because
// the two have opposite lifetimes. A verification record is working
// state, and discard removes it along with the branch it describes; an
// audit row is meant to outlive the branch, which is the entire reason
// for keeping one. A ref that discard does not touch is the only way to
// say that in storage rather than in a rule somebody has to remember.
//
// The lifetime holds against git's own collector: rows on a commit that
// is no longer reachable survive `git gc --prune=now`, because the notes
// ref itself keeps the annotated object alive. Only `git notes prune`
// removes them, and nothing here runs it — pruning the audit is the
// user's explicit act, never a side effect of housekeeping.
const OutcomeNotesRef = "dockhand/outcome"

// ErrNoNote reports a commit with no note under the ref.
var ErrNoNote = errors.New("git: no note for this commit")

// NoteWrite records content as the note on a commit, replacing any
// previous note there: the note is the commit's current verification
// record, and its history lives in the notes ref's own commits.
func (r *Repo) NoteWrite(ctx context.Context, ref, sha string, content []byte) error {
	_, err := r.gitStdin(ctx, content, "notes", "--ref="+ref, "add", "-f", "-F", "-", sha)
	return err
}

// NoteAppend adds content to a commit's note under the ref, creating
// the note when the commit has none. git separates what was already
// there from what arrives with a blank line, so the only note format
// that reads back exactly as it was written is one whose records are
// whole lines and whose reader steps over the blanks.
//
// It exists so that an append-only log can be append-only in the
// plumbing rather than by convention: this verb cannot overwrite, so no
// caller can lose an earlier row by forgetting to read one first.
func (r *Repo) NoteAppend(ctx context.Context, ref, sha string, content []byte) error {
	_, err := r.gitStdin(ctx, content, "notes", "--ref="+ref, "append", "-F", "-", sha)
	return err
}

// NoteRead returns a commit's note under the ref, ErrNoNote when there
// is none.
//
// Only exit 1 is absence. `git notes show` answers a commit nobody
// annotated with "no note found for object" and exit 1, whether the ref
// exists or not; exit 128 is git's fatal band — a ref another process
// holds locked, an object it cannot read, a resource it cannot get —
// and those must propagate. Reading a fatal as absence is not a
// cosmetic miscount: LoadOrStart begins a blank record on ErrNoNote and
// Update writes it back, so one transient git failure would erase the
// job, the claim and the released flag that govern whether a guest may
// be handed back.
func (r *Repo) NoteRead(ctx context.Context, ref, sha string) ([]byte, error) {
	out, code, err := execGit(ctx, r.tools, r.Root, nil, "notes", "--ref="+ref, "show", sha)
	if err != nil {
		if code == 1 {
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
