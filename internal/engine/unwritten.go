package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// settlingOf is what settling an unwritten edit would do, from its own
// files as the worktree has them and the kept blobs Git still has (#194,
// "Settling"). It reads, and writes nothing.
func (e *Engine) settlingOf(ctx context.Context, worktree *git.Repository, edit model.Edit, settler Settler) (Settling, error) {
	files := map[string]WorkingFile{}
	var kept []string
	for _, file := range edit.Files {
		state, err := worktree.WorkingFile(ctx, file.Path)
		if errors.Is(err, git.ErrWorkingFile) {
			// Something other than a file in the way is neither side.
			files[file.Path] = WorkingFile{Present: true, Blob: "?"}
			continue
		}
		if err != nil {
			return Settling{}, err
		}
		files[file.Path] = WorkingFile{Present: state.Exists, Blob: model.ObjectID(state.Blob), Mode: modeWords(state)}
		if file.After != "" {
			kept = append(kept, string(file.After))
		}
	}
	gone, err := worktree.MissingObjects(ctx, kept)
	if err != nil {
		return Settling{}, err
	}
	missing := map[model.ObjectID]bool{}
	for object := range gone {
		missing[model.ObjectID(object)] = true
	}
	return settling(edit, files, missing, settler), nil
}

// modeWords is a file's mode as an edit records it, 100644; empty where
// it's absent.
func modeWords(state git.FileState) string {
	if !state.Exists {
		return ""
	}
	return strconv.FormatUint(uint64(state.Mode), 8)
}

// Settled is what settling did with one unwritten edit.
type Settled struct {
	Edit     model.Edit
	Settling Settling
	// Again is an edit removed to be prepared again, its kept blobs
	// gone and its files never written.
	Again bool
}

// settleUnwritten settles a branch's unwritten edits under its lock, before the
// command reads its edits: each finished, gone on with, or stopping the
// command, as settling says for the settler (#194). An edit whose files
// changed since stops every settler but tidy with a person there; a cut
// write stops all but the edit's own commands, naming update.
func (e *Engine) settleUnwritten(ctx context.Context, branch model.Branch, settlerFor func(model.Edit) Settler) ([]Settled, error) {
	var unwritten []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		unwritten, err = r.UnwrittenEdits(branch.ID)
		return err
	}); err != nil || len(unwritten) == 0 {
		return nil, err
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return nil, err
	}
	var settled []Settled
	for _, edit := range unwritten {
		decided, err := e.settlingOf(ctx, worktree, edit, settlerFor(edit))
		if err != nil {
			return settled, err
		}
		done := Settled{Edit: edit, Settling: decided}
		switch decided.Action {
		case SettleRecord:
			if err := e.recordWritten(ctx, branch, edit); err != nil {
				return settled, err
			}
			progress.Report(ctx, "Finished recording %s, which an earlier run wrote", edit.Subject)
		case SettleWrite:
			if err := e.writeKept(ctx, worktree, edit, decided.Files); err != nil {
				return settled, err
			}
			if err := e.recordWritten(ctx, branch, edit); err != nil {
				return settled, err
			}
			progress.Report(ctx, "Finished %s, which an earlier run began", edit.Subject)
		case SettleGoOn:
			progress.Report(ctx, "%s was recorded and never written; going on with the files as they are", edit.Subject)
		case SettleReplaced:
			if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.RemoveUnwritten(edit.ID) }); err != nil {
				return settled, err
			}
			progress.Report(ctx, "%s was never written; the version you named replaces it", edit.Subject)
		case SettlePrepareAgain:
			if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.RemoveUnwritten(edit.ID) }); err != nil {
				return settled, err
			}
			done.Again = true
			progress.Report(ctx, "%s was never written and Git no longer has what it would write; it is prepared again", edit.Subject)
		case SettleUnfinished:
			return settled, refusal.New(refusal.Attention, "%s was cut short: %s aren't written yet; dockhand update %s finishes it", edit.Subject, strings.Join(decided.Files, ", "), edit.Port)
		case SettleBlobsGone:
			return settled, refusal.New(refusal.Attention, "%s was cut short, and Git no longer has what it would write of %s; nothing was written", edit.Subject, strings.Join(decided.Files, ", "))
		case SettleChanged:
			return settled, refusal.New(refusal.Attention, "%s was recorded before its files were written, and %s changed since; nothing was written. Look at the files, then dockhand tidy commits them as yours", edit.Subject, strings.Join(decided.Files, ", "))
		case SettleTheirs:
			// tidy, with the person there, commits the files as theirs
			// and removes the edit (tidy's own step).
		}
		settled = append(settled, done)
	}
	return settled, nil
}

// writeKept writes an unwritten edit's files that aren't written yet from
// the blobs it kept, each checked again just before (ApplyToWorkingFiles).
func (e *Engine) writeKept(ctx context.Context, worktree *git.Repository, edit model.Edit, paths []string) error {
	var edits []git.FileEdit
	for _, file := range edit.Files {
		if !containsPath(paths, file.Path) {
			continue
		}
		before, err := worktree.WorkingFile(ctx, file.Path)
		if err != nil {
			return err
		}
		if file.After == "" {
			edits = append(edits, git.FileEdit{Path: file.Path, Before: before, Delete: true})
			continue
		}
		data, err := worktree.ReadBlob(ctx, string(file.After))
		if err != nil {
			return err
		}
		mode := uint32(0o100644)
		if file.AfterMode == "100755" {
			mode = 0o100755
		}
		edits = append(edits, git.FileEdit{Path: file.Path, Before: before, After: data, Mode: mode})
	}
	if err := expandFor(ctx, worktree, paths); err != nil {
		return err
	}
	return worktree.ApplyToWorkingFiles(ctx, edits)
}

func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

// recordWritten is the update's step 5: the edit's files are written, and
// the event says the edit. Its witness passes only where Written is set.
func (e *Engine) recordWritten(ctx context.Context, branch model.Branch, edit model.Edit) error {
	err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.SetWritten(edit.ID, e.now()); err != nil {
			return err
		}
		// The version it concludes, as the update's own step 5 records it.
		if edit.Kind == model.EditUpdate {
			if err := concludeVersion(tx, branch.ID, edit.Port, e.requestSettings()); err != nil {
				return err
			}
		}
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: model.EventBranchEdit, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s (%s)", edit.Port, editChange(edit), listEditedPaths(edit))})
		return err
	}, editWritten(edit.ID))
	if err != nil {
		return fmt.Errorf("the files of %s are written and its record isn't complete; the next command run on the branch completes it: %w", edit.Subject, err)
	}
	return nil
}

// listEditedPaths are an edit's files as an event names them.
func listEditedPaths(edit model.Edit) string {
	var paths []string
	for _, file := range edit.Files {
		paths = append(paths, file.Path)
	}
	return listPaths(paths)
}

// editRecordedUnwritten witnesses step 3: the edit, by its ID, written or
// not, since Edits hides an unwritten one (#194).
func editRecordedUnwritten(id model.EditID) func(store.Reader) bool {
	return func(r store.Reader) bool {
		_, found, err := r.Edit(id)
		return err == nil && found
	}
}

// editWritten witnesses step 5: the edit, by its ID, with Written set.
func editWritten(id model.EditID) func(store.Reader) bool {
	return func(r store.Reader) bool {
		edit, found, err := r.Edit(id)
		return err == nil && found && !edit.Written.IsZero()
	}
}

// settled settles a branch's unwritten edits under its lock, as a command
// does before it reads the branch's edits (#194): the settler each edit
// has for this command. A plan settles nothing, and calls none of this.
func (e *Engine) settled(ctx context.Context, branch *model.Branch, settlerFor func(model.Edit) Settler) ([]Settled, error) {
	if branch.ID == "" || branch.Name == "" {
		return nil, nil
	}
	var settled []Settled
	err := e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch, _ *heldLock) error {
		*branch = current
		// A rename cut short is finished first, at the lock, however the
		// branch was come by (#194, L4 and L5).
		if err := e.finishRename(ctx, branch); err != nil {
			return err
		}
		var err error
		settled, err = e.settleUnwritten(ctx, *branch, settlerFor)
		return err
	})
	return settled, err
}

// heldLock is a branch name's lock as lockedBranch holds it for fn's
// call: a rename takes one, so a rename without its old name's lock can't
// be written to work (Codex's confirmation of #194, P3). Only lockedBranch
// makes one, and it is spent once fn returns.
type heldLock struct {
	name     string
	released bool
}

// lockedBranch runs fn holding the branch's lock, by its ID and not a name
// read before (#194, L7; Codex's read of 2a, P2): it takes the name's
// lock, reads the branch again by ID under it, and where the ID carries
// another name now, lets go and takes that name's, a few rounds at most.
// fn gets the branch as read under the lock. A branch gone settles nothing.
func (e *Engine) lockedBranch(ctx context.Context, id model.BranchID, name string, fn func(context.Context, model.Branch, *heldLock) error) error {
	for range 4 {
		var moved string
		ran := false
		err := e.Repo.WithBranchLock(ctx, name, func(ctx context.Context) error {
			var current model.Branch
			err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
				var err error
				current, err = r.Branch(id)
				return err
			})
			switch {
			case errors.Is(err, store.ErrNotFound):
				ran = true
				return nil
			case err != nil:
				return err
			case current.Name != name:
				moved = current.Name
				return nil
			}
			ran = true
			held := &heldLock{name: name}
			defer func() { held.released = true }()
			return fn(ctx, current, held)
		})
		if err != nil || ran {
			return err
		}
		name = moved
	}
	return refusal.New(refusal.Attention, "%s keeps being renamed while dockhand takes its lock; run this again once the other command ends", strings.TrimPrefix(name, model.BranchPrefix))
}

// SettlerFor is one settler for every edit.
func SettlerFor(settler Settler) func(model.Edit) Settler {
	return func(model.Edit) Settler { return settler }
}

// UnfinishedGate is what a check's proposal and a submission's plan ask of
// a branch's preparation (#194, T3): its unwritten edits settled as any
// other command settles them, then the verdict. With nobody there, an
// unfinished preparation stops, naming the command that finishes it: the
// files as they are aren't the update that was asked. A person is told
// what isn't in the files, and goes on.
func (e *Engine) UnfinishedGate(ctx context.Context, branch model.Branch, unattended bool, doing string) error {
	if branch.ID == "" {
		return nil
	}
	if _, err := e.settled(ctx, &branch, SettlerFor(SettlerOther)); err != nil {
		if unattended || refusal.Of(err) == nil {
			return err
		}
		progress.Report(ctx, "%s; %s the files as they are", refusal.WordsOf(err), doing)
	}
	prepared, err := e.preparation(ctx, branch, !unattended)
	if err != nil || prepared.Disposition != branchstate.UnfinishedPreparation {
		return err
	}
	if err := refusedIn(branch); err != nil {
		return err
	}
	words := fmt.Sprintf("%s's preparation isn't finished: %s", branch.ShortName(), strings.Join(prepared.Reasons, "; "))
	if unattended {
		return refusal.New(refusal.Attention, "%s; dockhand ship -b %s finishes it", words, branch.ShortName())
	}
	progress.Report(ctx, "%s; %s the files as they are", words, doing)
	return nil
}

// SayUnsettled says, for a plan, what settling would do with the branch's
// unwritten edits, and does none of it (#194, T2): no file is written,
// Written is not set, nothing is recorded.
func (e *Engine) SayUnsettled(ctx context.Context, branch model.Branch, settlerFor func(model.Edit) Settler) error {
	if branch.ID == "" {
		return nil
	}
	var unwritten []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		unwritten, err = r.UnwrittenEdits(branch.ID)
		return err
	}); err != nil || len(unwritten) == 0 {
		return err
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return err
	}
	for _, edit := range unwritten {
		decided, err := e.settlingOf(ctx, worktree, edit, settlerFor(edit))
		if err != nil {
			return err
		}
		progress.Report(ctx, "%s was recorded before its files were written: %s (not done in a plan)", edit.Subject, settlingWords(decided))
	}
	return nil
}

// settlingWords say what settling would do.
func settlingWords(s Settling) string {
	files := strings.Join(s.Files, ", ")
	switch s.Action {
	case SettleRecord:
		return "its files are written, so its record would be finished"
	case SettleWrite:
		return "it would write " + files + " and finish it"
	case SettleGoOn:
		return "none of its files is written, and the command would go on with the files as they are"
	case SettleUnfinished:
		return "it was cut short, and " + files + " aren't written; the command would stop"
	case SettleChanged:
		return files + " changed since; the command would stop"
	case SettleTheirs:
		return files + " changed since; tidy would commit them as yours"
	case SettleReplaced:
		return "none of its files is written, and the version you named would replace it"
	case SettlePrepareAgain:
		return "Git no longer has what it would write; it would be prepared again"
	case SettleBlobsGone:
		return "it was cut short, and Git no longer has what it would write of " + files + "; the command would stop"
	}
	return string(s.Action)
}

// refusedReasons are the actions the branch's request has refused, each
// with why, read fresh by the caller's branch.
func refusedReasons(branch model.Branch) []string {
	if branch.Request == nil {
		return nil
	}
	var reasons []string
	for _, target := range branch.Request.Targets {
		for _, outcome := range target.Outcomes {
			if outcome.Result == model.ActionRefused {
				reasons = append(reasons, outcome.Why)
			}
		}
	}
	for _, outcome := range branch.Request.Outcomes {
		if outcome.Result == model.ActionRefused {
			reasons = append(reasons, outcome.Why)
		}
	}
	return reasons
}

// refusedIn is the one judgement of a branch's refused actions, which every
// gate asks: a refusal is kept and lets nothing go on, whoever is there,
// since every gate that asks whether the actions are concluded asks for
// done or already so (#195, "Readiness"). Nil where none stands.
func refusedIn(branch model.Branch) error {
	if refused := refusedReasons(branch); len(refused) > 0 {
		return refusal.New(refusal.Attention, "%s: %s; nothing goes on until the ask changes", branch.ShortName(), strings.Join(refused, "; "))
	}
	return nil
}

// refusedGate is refusedIn of the branch as the store has it now.
func (e *Engine) refusedGate(ctx context.Context, branch model.Branch) error {
	if branch.ID == "" {
		return nil
	}
	current, err := e.Branch(ctx, branch.ID)
	if err != nil {
		return err
	}
	return refusedIn(current)
}
