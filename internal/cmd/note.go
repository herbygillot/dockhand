package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/verify"
)

// verifyNote is a commit's verification record, stored as its git note
// under refs/notes/dockhand/verify: sha-keyed, local to this machine,
// read back by status. Schema 2 holds one run per platform, keyed by
// the resolved release name — a commit's verdict is a set, so a
// platform-floor investigation lives in one note instead of
// overwriting itself. Each run's Job is the serializable value the
// process that collects need not have submitted; Handle names a kept
// environment, machine-local by nature.
type verifyNote struct {
	Schema int                  `json:"schema"`
	Sha    string               `json:"sha"`
	Tree   string               `json:"tree"` // content identity: a message-only amend moves Sha, not Tree
	Port   string               `json:"port"`
	Runs   map[string]verifyRun `json:"runs"`
}

// verifyRun is one platform's verification: running, passed, failed,
// unsupported (the port declines the platform), canceled, superseded,
// deferred (no slot when asked), or errored.
type verifyRun struct {
	State  string     `json:"state"`
	Job    verify.Job `json:"job"`
	Handle string     `json:"handle,omitempty"`
	Detail string     `json:"detail,omitempty"`
	// Tested says the run included the port's test suite (`port test`)
	// after the install — promote's checklist vouches only for what a
	// note remembers.
	Tested bool `json:"tested,omitempty"`
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
func (n verifyNote) platforms() []string {
	out := make([]string, 0, len(n.Runs))
	for k := range n.Runs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// anyState reports whether any run is in the given state.
func (n verifyNote) anyState(s string) bool {
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
func (n verifyNote) promotable() bool {
	return n.anyState("passed") && !n.anyState("failed")
}

// writeNote records the note on its commit, replacing what was there:
// the note is the commit's current record, and history lives in the
// notes ref itself.
func writeNote(ctx context.Context, repo *git.Repo, n verifyNote) error {
	n.Schema = noteSchema
	body, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return repo.NoteWrite(ctx, git.VerifyNotesRef, n.Sha, body)
}

// readNote returns a commit's verification record, git.ErrNoNote when
// the commit has none. A schema-1 note lifts into a single run.
func readNote(ctx context.Context, repo *git.Repo, sha string) (verifyNote, error) {
	body, err := repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	if err != nil {
		return verifyNote{}, err
	}
	var n verifyNote
	if err := json.Unmarshal(body, &n); err != nil {
		return verifyNote{}, fmt.Errorf("note on %s: %w", sha, err)
	}
	if n.Schema >= noteSchema && n.Runs != nil {
		return n, nil
	}
	var l legacyNote
	if err := json.Unmarshal(body, &l); err != nil {
		return verifyNote{}, fmt.Errorf("note on %s: %w", sha, err)
	}
	key := l.Platform
	if key == "" {
		key = "(unrecorded)"
	}
	return verifyNote{
		Schema: noteSchema, Sha: l.Sha, Tree: l.Tree, Port: l.Port,
		Runs: map[string]verifyRun{key: {State: l.State, Job: l.Job, Handle: l.Handle, Detail: l.Detail}},
	}, nil
}

// loadOrStartNote reads the commit's note, or begins one carrying the
// commit's identity — the read-modify-write every per-platform update
// goes through.
func loadOrStartNote(ctx context.Context, repo *git.Repo, sha, port string) (verifyNote, error) {
	n, err := readNote(ctx, repo, sha)
	if err == nil {
		if n.Runs == nil {
			n.Runs = map[string]verifyRun{}
		}
		return n, nil
	}
	tree, terr := repo.RevParse(ctx, sha+"^{tree}")
	if terr != nil {
		return verifyNote{}, terr
	}
	return verifyNote{
		Schema: noteSchema, Sha: sha, Tree: tree, Port: port,
		Runs: map[string]verifyRun{},
	}, nil
}

// promotableVerdictFor reports the verification covering a tip — its
// own note, or any note over the identical tree, since a message-only
// amend moves the sha and not the content — and whether that verdict
// set clears promote's gate.
func promotableVerdictFor(ctx context.Context, repo *git.Repo, tip string) (verifyNote, bool, error) {
	if n, err := readNote(ctx, repo, tip); err == nil {
		return n, n.promotable(), nil
	}
	tree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return verifyNote{}, false, err
	}
	noted, err := repo.NotesList(ctx, git.VerifyNotesRef)
	if err != nil {
		return verifyNote{}, false, err
	}
	for _, sha := range noted {
		n, err := readNote(ctx, repo, sha)
		if err == nil && n.Tree == tree && n.promotable() {
			return n, true, nil
		}
	}
	return verifyNote{}, false, nil
}
