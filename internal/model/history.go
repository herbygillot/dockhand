package model

import (
	"fmt"
	"time"
)

// EditID identifies an authoring command's record.
type EditID string

// EditKind is what an authoring command did.
type EditKind string

const (
	EditUpdate    EditKind = "update"
	EditChecksums EditKind = "checksums"
	EditRevbump   EditKind = "revbump"
	// EditCreate is a new port's first Portfile.
	EditCreate EditKind = "create"
)

// EditedFile is one file an authoring command wrote: its blob before and
// after, empty where the file was absent.
type EditedFile struct {
	Path   string
	Before ObjectID
	After  ObjectID
}

// Edit is what an authoring command wrote in a branch's working files. It
// is kept so tidy can tell a port's edits that are exactly dockhand's from
// ones a person changed afterwards, and use the subject dockhand wrote.
type Edit struct {
	ID     EditID
	Branch BranchID
	Kind   EditKind
	Port   string
	// Directory is the port's directory, such as textproc/jq.
	Directory string
	// Subject is the commit subject the edit carries, such as
	// "jq: update to 1.8.1".
	Subject string
	Files   []EditedFile
	At      time.Time
	// Upstream is what comparing the old and new upstream archives found,
	// for an update that compared them; nil when it did not.
	Upstream *UpstreamComparison
}

// UpstreamComparison is what an update's upstream archives showed.
type UpstreamComparison struct {
	Changes []UpstreamChange `json:"changes"`
	// Problem says why the archives could not be compared.
	Problem string `json:"problem,omitempty"`
}

// Held reports whether anything found holds the update for a person's
// look before serve may submit it.
func (c *UpstreamComparison) Held() bool {
	if c == nil {
		return false
	}
	for _, change := range c.Changes {
		if change.Hold {
			return true
		}
	}
	return false
}

// UpstreamChange is one difference between the archives: a license file,
// a build file, or a declared dependency.
type UpstreamChange struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Hold    bool   `json:"hold"`
}

// Validate checks the rules every stored edit keeps.
func (e Edit) Validate() error {
	switch {
	case e.ID == "" || e.Branch == "":
		return invalid("edit %q has no ID or branch", e.ID)
	case e.Kind != EditUpdate && e.Kind != EditChecksums && e.Kind != EditRevbump && e.Kind != EditCreate:
		return invalid("edit %s has unknown kind %q", e.ID, e.Kind)
	case e.Port == "" || e.Directory == "" || e.Subject == "":
		return invalid("edit %s has no port, directory, or subject", e.ID)
	case len(e.Files) == 0:
		return invalid("edit %s changed no files", e.ID)
	case e.At.IsZero():
		return invalid("edit %s has no time", e.ID)
	}
	for _, file := range e.Files {
		if file.Path == "" || file.Before == file.After {
			return invalid("edit %s records an unchanged or unnamed file", e.ID)
		}
	}
	return nil
}

// CheckpointKind names what rewrote the history a checkpoint keeps.
type CheckpointKind string

const (
	CheckpointTidy   CheckpointKind = "tidy"
	CheckpointRebase CheckpointKind = "rebase"
)

// Checkpoint keeps a branch's history from before tidy or rebase rewrote
// it, for restore. Its Git ref, refs/dockhand/checkpoints/<name>, keeps
// the old commits reachable.
type Checkpoint struct {
	// Number counts a repository's checkpoints from 1, whatever their kind.
	Number int
	Kind   CheckpointKind
	Branch BranchID
	// Before is the branch head it replaced, and After the one it wrote.
	Before, After ObjectID
	At            time.Time
	RestoredAt    *time.Time
}

// Name is what people type: tidy-3, or rebase-4.
func (c Checkpoint) Name() string { return fmt.Sprintf("%s-%d", c.Kind, c.Number) }

// Ref is the Git ref that keeps the old history.
func (c Checkpoint) Ref() string { return "refs/dockhand/checkpoints/" + c.Name() }

// Validate checks the rules every stored checkpoint keeps.
func (c Checkpoint) Validate() error {
	switch {
	case c.Number <= 0 || c.Branch == "":
		return invalid("checkpoint %d has no number or branch", c.Number)
	case c.Kind != CheckpointTidy && c.Kind != CheckpointRebase:
		return invalid("checkpoint %d has unknown kind %q", c.Number, c.Kind)
	case c.Before == "" || c.After == "":
		return invalid("checkpoint %s has no heads", c.Name())
	case c.At.IsZero():
		return invalid("checkpoint %s has no time", c.Name())
	}
	return nil
}

// Acceptance acknowledges a failed revision-only target, or a failed extra,
// for one exact commit (Design v3 §3's publication rule).
type Acceptance struct {
	Branch BranchID
	Commit ObjectID
	Port   string
	At     time.Time
}

// Validate checks the rules every stored acceptance keeps.
func (a Acceptance) Validate() error {
	if a.Branch == "" || a.Commit == "" || a.Port == "" || a.At.IsZero() {
		return invalid("acceptance of %q is incomplete", a.Port)
	}
	return nil
}

// Review is one review dockhand made of a pull request: what it found at
// which head, and whether it was posted (Design v3 §6.11). The next review
// of the same pull request reads it to say which findings are resolved.
type Review struct {
	// Repository is the pull request's base repository, such as
	// macports/macports-ports.
	Repository string
	Number     int
	Head       ObjectID
	Findings   []ReviewFinding
	// Posted is how it was posted: "comment", "request-changes", or ""
	// when it was not.
	Posted string
	At     time.Time
}

// ReviewFinding is one finding of a review, as it was reported.
type ReviewFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Where    string `json:"where,omitempty"`
	Message  string `json:"message"`
}

// Validate checks the rules every stored review keeps.
func (r Review) Validate() error {
	switch {
	case r.Repository == "" || r.Number <= 0 || r.Head == "" || r.At.IsZero():
		return invalid("review of %s#%d is incomplete", r.Repository, r.Number)
	case r.Posted != "" && r.Posted != "comment" && r.Posted != "request-changes":
		return invalid("review of %s#%d was posted as %q", r.Repository, r.Number, r.Posted)
	}
	return nil
}
