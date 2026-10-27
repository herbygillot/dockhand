package engine

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

// restyleTidy adds to a tidy plan the commit `tidy --restyle` asks for,
// last and of its own, "<port>: whitespace": each Portfile the branch
// changes, restyled whole (portfile.Restyle; Deferred's "Fixing a
// Portfile's alignment automatically", reopened as opt-in at the person's
// word of 2026-10-27, "that can be done as a separate commit … behind a
// flag"). A Portfile whose port, any subport of it, evaluates otherwise
// restyled, or that can't be evaluated, is left as it was, and said.
func (e *Engine) restyleTidy(ctx context.Context, plan *TidyPlan) error {
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return err
	}
	changed, err := worktree.ChangedPaths(ctx, plan.BaseTree, plan.Final)
	if err != nil {
		return err
	}
	ports, err := e.capabilities()
	if err != nil {
		return err
	}
	var edits []git.FileEdit
	var names []string
	for _, name := range changed {
		if path.Base(name) != "Portfile" {
			continue
		}
		state, text, err := worktree.File(ctx, plan.Final, name)
		if err != nil {
			return err
		}
		if !state.Exists {
			continue
		}
		restyled, ok := portfile.Restyle(text)
		if !ok {
			continue
		}
		directory := path.Dir(name)
		edit := git.FileEdit{Path: name, Before: state, After: restyled, Mode: state.Mode}
		if why := e.restyleEvaluates(ctx, ports.Families, worktree, plan.Final, directory, edit); why != "" {
			plan.NotRestyled = append(plan.NotRestyled, name+": "+why)
			continue
		}
		edits = append(edits, edit)
		names = append(names, directoryName(directory))
	}
	if len(edits) == 0 {
		return nil
	}
	if plan.Restyled, err = worktree.EditTree(ctx, plan.Final, edits); err != nil {
		return err
	}
	author, err := worktree.Author(ctx)
	if err != nil {
		return err
	}
	author.When = e.now()
	slices.Sort(names)
	group := TidyGroup{Ports: names, Message: commitmsg.Compose(strings.Join(names, ", ")+": whitespace", "", nil, nil, true), Author: author, Working: true,
		Notes: []string{"restyled whole by --restyle, each port evaluating as it did"}}
	for _, edit := range edits {
		group.Paths = append(group.Paths, edit.Path)
	}
	plan.Restyle, plan.restyled, plan.Keep = &group, edits, false
	return nil
}

// restyleEvaluates says why a restyled Portfile can't be committed: its
// directory, every subport, evaluates otherwise than before, or can't be
// evaluated; empty where it evaluates the same.
func (e *Engine) restyleEvaluates(ctx context.Context, families FamilyReader, worktree *git.Repository, tree, directory string, edit git.FileEdit) string {
	if families == nil {
		return "nothing here evaluates it, to show restyling changes nothing"
	}
	restyled, err := worktree.EditTree(ctx, tree, []git.FileEdit{edit})
	if err != nil {
		return err.Error()
	}
	before, err := families.Family(ctx, model.Source{Tree: model.ObjectID(tree)}, directory)
	if err != nil {
		return "it couldn't be evaluated: " + err.Error()
	}
	after, err := families.Family(ctx, model.Source{Tree: model.ObjectID(restyled)}, directory)
	if err != nil {
		return "it couldn't be evaluated restyled: " + err.Error()
	}
	for _, change := range fidelity.SubportChanges(directory, &before, &after) {
		if change.Kind != model.SubportUnchanged {
			return fmt.Sprintf("restyled, %s evaluates otherwise", change.Port)
		}
	}
	return ""
}
