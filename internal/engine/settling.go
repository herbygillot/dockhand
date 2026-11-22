package engine

import (
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
)

// WorkingFile is one of an unwritten edit's files as the worktree has it:
// there or not, its blob as hashing the file gives it, and its mode.
type WorkingFile struct {
	Present bool
	Blob    model.ObjectID
	Mode    string
}

// Settler is who settles an unwritten edit, which decides what a cut
// write does (docs/designs/preparation-and-request.md, "Settling").
type Settler int

const (
	// SettlerOwner is an update of the edit's port, a bump, a ship, or
	// serve for its own branches: it finishes the write.
	SettlerOwner Settler = iota
	// SettlerOther is every other command: it writes nothing.
	SettlerOther
	// SettlerTidyAttended is tidy with a person looking, who takes files
	// someone changed as their own.
	SettlerTidyAttended
)

// SettleAction is what settling an unwritten edit does.
type SettleAction string

const (
	// SettleRecord finishes the record: the files are written (step 5).
	SettleRecord SettleAction = "record"
	// SettleWrite writes Files from the kept blobs, then records.
	SettleWrite SettleAction = "write"
	// SettleGoOn says the edit wasn't written and goes on with the files
	// as they are.
	SettleGoOn SettleAction = "go-on"
	// SettleUnfinished says the write was cut short and stops, naming
	// `dockhand update <port>`.
	SettleUnfinished SettleAction = "unfinished"
	// SettleChanged stops: Files changed since, and nothing is written.
	SettleChanged SettleAction = "changed"
	// SettleTheirs commits the files as the person's and removes the
	// unwritten edit (tidy, attended).
	SettleTheirs SettleAction = "theirs"
	// SettlePrepareAgain removes the edit, whose kept blobs are gone and
	// whose files were never written, and prepares the port again.
	SettlePrepareAgain SettleAction = "prepare-again"
)

// SettleBlobsGone stops: the write was cut short and the kept blobs of
// Files, which it would finish, are gone; nothing is written.
const SettleBlobsGone SettleAction = "blobs-gone"

// Settling is what settling one unwritten edit does, and the files it
// names: the ones it writes, or the ones that stop it.
type Settling struct {
	Action SettleAction
	Files  []string
}

// settling decides what settling an unwritten edit does from its own
// files as the worktree has them, each against its two recorded sides,
// whole or not at all: a file outside the edit's list isn't read. missing
// are kept blobs Git no longer has, each its own answer, never a file
// that differs. A plan reads the answer and does nothing with it.
func settling(edit model.Edit, worktree map[string]WorkingFile, missing map[model.ObjectID]bool, settler Settler) Settling {
	var written, unwritten, changed []string
	for _, file := range edit.Files {
		state := worktree[file.Path]
		switch {
		case sideIs(state, file.After, file.AfterMode):
			written = append(written, file.Path)
		case sideIs(state, file.Before, file.BeforeMode):
			unwritten = append(unwritten, file.Path)
		default:
			changed = append(changed, file.Path)
		}
	}
	switch {
	case len(changed) > 0:
		if settler == SettlerTidyAttended {
			return Settling{Action: SettleTheirs, Files: changed}
		}
		return Settling{Action: SettleChanged, Files: changed}
	case len(unwritten) == 0:
		return Settling{Action: SettleRecord}
	}
	var gone []string
	for _, file := range edit.Files {
		if file.After != "" && missing[file.After] && slices.Contains(unwritten, file.Path) {
			gone = append(gone, file.Path)
		}
	}
	if len(written) == 0 {
		switch {
		case settler != SettlerOwner:
			return Settling{Action: SettleGoOn}
		case len(gone) > 0:
			return Settling{Action: SettlePrepareAgain, Files: gone}
		}
		return Settling{Action: SettleWrite, Files: unwritten}
	}
	switch {
	case len(gone) > 0:
		return Settling{Action: SettleBlobsGone, Files: gone}
	case settler != SettlerOwner:
		return Settling{Action: SettleUnfinished, Files: unwritten}
	}
	return Settling{Action: SettleWrite, Files: unwritten}
}

// sideIs reports whether a working file is one recorded side: absent
// where the side has no blob, else its blob, and its mode where one was
// recorded; an edit from before modes were kept compares blobs alone.
func sideIs(state WorkingFile, blob model.ObjectID, mode string) bool {
	if blob == "" {
		return !state.Present
	}
	return state.Present && state.Blob == blob && (mode == "" || state.Mode == mode)
}
