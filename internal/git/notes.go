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
// NotesRefs lists the notes namespaces under a slash-terminated prefix,
// by their short name — "dockhand/verify" for refs/notes/dockhand/verify.
//
// It exists because a namespace outlives the build that named it. A
// purge that removed only the ref THIS build writes left every note an
// older spelling had made, under a name nothing in the tree mentions any
// more — measured: fifteen records under refs/notes/dockhand/outcome
// survived a purge that reported removing everything.
func (r *Repo) NotesRefs(ctx context.Context, prefix string) ([]string, error) {
	out, err := r.git(ctx, "for-each-ref", "--format=%(refname)", "refs/notes/"+prefix)
	if err != nil {
		return nil, err
	}
	var refs []string
	for line := range strings.Lines(out) {
		if name := strings.TrimPrefix(strings.TrimSpace(line), "refs/notes/"); name != "" {
			refs = append(refs, name)
		}
	}
	return refs, nil
}

// DropNotesRef removes a whole notes namespace.
//
// A NOTES REF IS NOT AN OWNED REF and this is not a second ref-mover:
// R23 reserves refs/dockhand/state, the pins and the dockhand branches
// for the store's own batch, and refs/notes/ is none of them — this
// package already writes and removes individual notes there. Deleting
// the namespace is the same authority applied to the whole rather than
// to one commit's entry, and it is what a purge of an ABANDONED spelling
// needs: there is no list of commits to walk when nothing in the tree
// knows what wrote them.
func (r *Repo) DropNotesRef(ctx context.Context, ref string) error {
	_, err := r.git(ctx, "update-ref", "-d", "refs/notes/"+ref)
	return err
}

func (r *Repo) NoteRemove(ctx context.Context, ref, sha string) error {
	_, err := r.git(ctx, "notes", "--ref="+ref, "remove", "--ignore-missing", sha)
	return err
}
