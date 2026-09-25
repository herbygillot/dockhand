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
}

// Validate checks the rules every stored edit keeps.
func (e Edit) Validate() error {
	switch {
	case e.ID == "" || e.Branch == "":
		return invalid("edit %q has no ID or branch", e.ID)
	case e.Kind != EditUpdate && e.Kind != EditChecksums:
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

// Checkpoint keeps a branch's history from before tidy rewrote it, for
// restore. Its Git ref, refs/dockhand/checkpoints/<name>, keeps the old
// commits reachable.
type Checkpoint struct {
	// Number counts a repository's checkpoints from 1.
	Number int
	Branch BranchID
	// Before is the branch head tidy replaced, and After the one it wrote.
	Before, After ObjectID
	At            time.Time
	RestoredAt    *time.Time
}

// Name is what people type: tidy-3.
func (c Checkpoint) Name() string { return fmt.Sprintf("tidy-%d", c.Number) }

// Ref is the Git ref that keeps the old history.
func (c Checkpoint) Ref() string { return "refs/dockhand/checkpoints/" + c.Name() }

// Validate checks the rules every stored checkpoint keeps.
func (c Checkpoint) Validate() error {
	switch {
	case c.Number <= 0 || c.Branch == "":
		return invalid("checkpoint %d has no number or branch", c.Number)
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
