package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/verify"
)

// verifyNote is a commit's verification record, stored as its git note
// under refs/notes/dockhand/verify (D21): sha-keyed, local to this
// machine, and read back by status. The Job inside is D17's
// serializable value — the process that submitted is not the one that
// collects — and Handle names a kept worker, which is machine-local by
// nature.
type verifyNote struct {
	Schema   int        `json:"schema"`
	Sha      string     `json:"sha"`
	Tree     string     `json:"tree"` // content identity: a message-only amend moves Sha, not Tree
	Port     string     `json:"port"`
	Platform string     `json:"platform,omitempty"` // requested; empty means the provider's default
	State    string     `json:"state"`              // running | passed | failed | errored
	Job      verify.Job `json:"job"`
	Handle   string     `json:"handle,omitempty"` // kept environment on failure
	Detail   string     `json:"detail,omitempty"`
}

const noteSchema = 1

// writeNote records the note on its commit, replacing what was there:
// the note is the commit's current record, and history lives in the
// notes ref itself.
func writeNote(ctx context.Context, repo *git.Repo, n verifyNote) error {
	body, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return repo.NoteWrite(ctx, git.VerifyNotesRef, n.Sha, body)
}

// readNote returns a commit's verification record, git.ErrNoNote when
// the commit has none.
func readNote(ctx context.Context, repo *git.Repo, sha string) (verifyNote, error) {
	body, err := repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	if err != nil {
		return verifyNote{}, err
	}
	var n verifyNote
	if err := json.Unmarshal(body, &n); err != nil {
		return verifyNote{}, fmt.Errorf("note on %s: %w", sha, err)
	}
	return n, nil
}
