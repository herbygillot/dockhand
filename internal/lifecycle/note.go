package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Note is a commit's verification record, stored as its git note
// under refs/notes/dockhand/verify: sha-keyed, local to this machine,
// read back by status. Schema 2 holds one run per platform, keyed by
// the resolved release name — a commit's verdict is a set, so a
// platform-floor investigation lives in one note instead of
// overwriting itself. Each run's Job is the serializable value the
// process that collects need not have submitted; Handle names a kept
// environment, machine-local by nature.
type Note struct {
	Schema int            `json:"schema"`
	Sha    string         `json:"sha"`
	Tree   string         `json:"tree"` // content identity: a message-only amend moves Sha, not Tree
	Port   string         `json:"port"`
	Runs   map[string]Run `json:"runs"`
}

// Run is one platform's verification: running, passed, failed,
// unsupported (the port declines the platform), canceled, superseded,
// deferred (no slot when asked), or errored.
type Run struct {
	State  string     `json:"state"`
	Job    verify.Job `json:"job"`
	Handle string     `json:"handle,omitempty"`
	Detail string     `json:"detail,omitempty"`
	// Tested says the run included the port's test suite (`port test`)
	// after the install — promote's checklist vouches only for what a
	// note remembers.
	Tested bool `json:"tested,omitempty"`
	// Linted says the run led with `port lint` — every tart run does
	// now, but the note remembers rather than the code assuming, so
	// verdicts recorded before lint existed stay honest.
	Linted bool `json:"linted,omitempty"`
	// Lint is what lint actually said, read from the log as the run
	// settles: "clean", or "2 warnings". It exists because the PR body
	// vouches per checked box, and a checked lint box with no
	// corroborating evidence was the one dishonest claim in it —
	// field-caught on the first post-lint batch.
	Lint string `json:"lint,omitempty"`
}

const noteSchema = 2

// legacyNote is schema 1: one flat verdict. Notes are local and
// short-lived, but an in-flight branch should survive the upgrade.
type legacyNote struct {
	Schema   int        `json:"schema"`
	Sha      string     `json:"sha"`
	Tree     string     `json:"tree"`
	Port     string     `json:"port"`
	Platform string     `json:"platform"`
	State    string     `json:"state"`
	Job      verify.Job `json:"job"`
	Handle   string     `json:"handle"`
	Detail   string     `json:"detail"`
}

// platforms lists the note's run keys, sorted for stable rendering.
func (n Note) Platforms() []string {
	out := make([]string, 0, len(n.Runs))
	for k := range n.Runs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AnyState reports whether any run is in the given state.
func (n Note) AnyState(s string) bool {
	for _, r := range n.Runs {
		if r.State == s {
			return true
		}
	}
	return false
}

// promotable is the gate promote applies to a verdict set: at least
// one platform passed, and none failed. A port declining a platform
// (unsupported) does not block — that refusal is often the change
// working — but an unexplained failure does, because it is exactly the
// question review will ask.
func (n Note) promotable() bool {
	return n.AnyState("passed") && !n.AnyState("failed")
}

// WriteNote records the note on its commit, replacing what was there:
// the note is the commit's current record, and history lives in the
// notes ref itself.
func WriteNote(ctx context.Context, repo *git.Repo, n Note) error {
	n.Schema = noteSchema
	body, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return repo.NoteWrite(ctx, git.VerifyNotesRef, n.Sha, body)
}

// ReadNote returns a commit's verification record, git.ErrNoNote when
// the commit has none. A schema-1 note lifts into a single run.
func ReadNote(ctx context.Context, repo *git.Repo, sha string) (Note, error) {
	body, err := repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	if err != nil {
		return Note{}, err
	}
	var n Note
	if err := json.Unmarshal(body, &n); err != nil {
		return Note{}, fmt.Errorf("note on %s: %w", sha, err)
	}
	if n.Schema >= noteSchema && n.Runs != nil {
		return n, nil
	}
	var l legacyNote
	if err := json.Unmarshal(body, &l); err != nil {
		return Note{}, fmt.Errorf("note on %s: %w", sha, err)
	}
	key := l.Platform
	if key == "" {
		key = "(unrecorded)"
	}
	return Note{
		Schema: noteSchema, Sha: l.Sha, Tree: l.Tree, Port: l.Port,
		Runs: map[string]Run{key: {State: l.State, Job: l.Job, Handle: l.Handle, Detail: l.Detail}},
	}, nil
}

// LoadOrStartNote reads the commit's note, or begins one carrying the
// commit's identity — the read-modify-write every per-platform update
// goes through.
func LoadOrStartNote(ctx context.Context, repo *git.Repo, sha, port string) (Note, error) {
	n, err := ReadNote(ctx, repo, sha)
	if err == nil {
		if n.Runs == nil {
			n.Runs = map[string]Run{}
		}
		return n, nil
	}
	tree, terr := repo.RevParse(ctx, sha+"^{tree}")
	if terr != nil {
		return Note{}, terr
	}
	return Note{
		Schema: noteSchema, Sha: sha, Tree: tree, Port: port,
		Runs: map[string]Run{},
	}, nil
}

// PromotableVerdictFor reports the verification covering a tip — its
// own note, or any note over the identical tree, since a message-only
// amend moves the sha and not the content — and whether that verdict
// set clears promote's gate.
func PromotableVerdictFor(ctx context.Context, repo *git.Repo, tip string) (Note, bool, error) {
	if n, err := ReadNote(ctx, repo, tip); err == nil {
		return n, n.promotable(), nil
	}
	tree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return Note{}, false, err
	}
	noted, err := repo.NotesList(ctx, git.VerifyNotesRef)
	if err != nil {
		return Note{}, false, err
	}
	for _, sha := range noted {
		n, err := ReadNote(ctx, repo, sha)
		if err == nil && n.Tree == tree && n.promotable() {
			return n, true, nil
		}
	}
	return Note{}, false, nil
}
