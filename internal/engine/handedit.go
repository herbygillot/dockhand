package engine

import (
	"context"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// HandEdit is an update dockhand refused, whose attempt it wrote for the
// person to finish, as a revision's files stand on it (update
// --hand-edit, Then's item 3): the port's directory, the refusal in its
// words, and whether the directory is the attempt as written or was
// adjusted by hand since.
type HandEdit struct {
	Directory string
	Words     string
	Adjusted  bool
}

// State says the attempt as the revision has it.
func (h HandEdit) State() string {
	if h.Adjusted {
		return "dockhand's attempt, adjusted by hand"
	}
	return "dockhand's attempt, as written"
}

// Line is the hand edit as status, tidy, and the pull request say it.
func (h HandEdit) Line() string {
	return "hand edit (" + h.State() + "): the update was refused, " + h.Words
}

// handEdits are a branch's hand edits as a tree has its files, by
// directory: each directory's latest attempt, kept through tidy, which
// commits it as the person's; one whose directory is the base's again
// lapsed with it.
func (e *Engine) handEdits(ctx context.Context, branch model.BranchID, base, tree string) (map[string]HandEdit, error) {
	return e.handEditsIn(ctx, branch, base, func() string { return tree })
}

// handEditsIn is handEdits, the tree asked for only where the branch has a
// hand edit: status's is its working files, which take capturing.
func (e *Engine) handEditsIn(ctx context.Context, branch model.BranchID, base string, treeOf func() string) (map[string]HandEdit, error) {
	var decisions []model.Decision
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		decisions, err = r.BranchDecisions(branch, model.FindingHandEdit)
		return err
	})
	if err != nil || len(decisions) == 0 {
		return nil, err
	}
	tree := treeOf()
	found := map[string]HandEdit{}
	for _, decision := range decisions {
		portfile := decision.Subject + "/Portfile"
		now, text, err := e.Repo.File(ctx, tree, portfile)
		if err != nil || !now.Exists {
			continue
		}
		if was, baseText, err := e.Repo.File(ctx, base, portfile); err == nil && was.Exists && string(baseText) == string(text) {
			delete(found, decision.Subject)
			continue
		}
		found[decision.Subject] = HandEdit{Directory: decision.Subject, Words: decision.Words,
			Adjusted: attemptDigest([]git.FileEdit{{Path: portfile, After: text}}) != decision.Evidence}
	}
	return found, nil
}

// submittedHandEdits are a branch's hand edits as the tree it submits has
// them, by directory, for the pull request; none where they can't be read,
// as the record then says nothing.
func (e *Engine) submittedHandEdits(ctx context.Context, branch model.Branch, tree string) []HandEdit {
	found, err := e.handEdits(ctx, branch.ID, string(branch.Base), tree)
	if err != nil {
		return nil
	}
	var edits []HandEdit
	for _, directory := range slices.Sorted(maps.Keys(found)) {
		edits = append(edits, found[directory])
	}
	return edits
}
