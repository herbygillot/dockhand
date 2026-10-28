package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/model"
)

// An updateStep is one commit of a directory whose edits are dockhand's
// alone and update several of its ports: an update, with the checksum
// refreshes and revision bumps made after it riding it, and the tree its
// directory's files are as those edits left them.
type updateStep struct {
	update model.Edit
	edits  []model.Edit
	paths  []string
	tree   string
}

// updateSteps are a directory's edits as a commit for each update, in the
// order made, where they update more than one of its ports: MacPorts'
// history has a commit each, "kubectl-1.35: update to 1.35.9", where tidy
// made one of four subports' updates (field testing's kubectl, on
// v0.5.0). An update of the port the step before updated joins it. Each
// step's tree is the base's with the edits so far, read from the blobs
// update wrote, so no hunk is split. None where the edits update one port.
func updateSteps(ctx context.Context, repo *git.Repository, baseTree, directory string, edits []model.Edit) ([]updateStep, error) {
	var steps []updateStep
	ports := map[string]bool{}
	for _, edit := range edits {
		if edit.Directory != directory {
			continue
		}
		switch {
		case edit.Kind == model.EditUpdate && (len(steps) == 0 || steps[len(steps)-1].update.Port != edit.Port):
			steps = append(steps, updateStep{update: edit})
			ports[edit.Port] = true
		case edit.Kind == model.EditUpdate:
			steps[len(steps)-1].update = edit
		case len(steps) == 0:
			// An edit before any update starts no step of its own.
			return nil, nil
		}
		step := &steps[len(steps)-1]
		step.edits = append(step.edits, edit)
		for _, file := range edit.Files {
			if !slices.Contains(step.paths, file.Path) {
				step.paths = append(step.paths, file.Path)
			}
		}
	}
	if len(ports) < 2 {
		return nil, nil
	}
	tree := baseTree
	for i := range steps {
		step := &steps[i]
		slices.Sort(step.paths)
		after := map[string]model.ObjectID{}
		for _, edit := range step.edits {
			for _, file := range edit.Files {
				after[file.Path] = file.After
			}
		}
		var changes []git.FileEdit
		for _, path := range step.paths {
			state, _, err := repo.File(ctx, tree, path)
			if err != nil {
				return nil, err
			}
			change := git.FileEdit{Path: path, Before: state, Mode: state.Mode}
			if blob := after[path]; blob == "" {
				change.Delete, change.Mode = true, 0
			} else if change.After, err = repo.ReadBlob(ctx, string(blob)); err != nil {
				return nil, fmt.Errorf("the update's %s isn't kept, so tidy can't make its commit alone: %w", path, err)
			}
			if !state.Exists {
				change.Mode = 0o100644
			}
			changes = append(changes, change)
		}
		var err error
		if tree, err = repo.EditTree(ctx, tree, changes); err != nil {
			return nil, err
		}
		step.tree = tree
	}
	return steps, nil
}

// stepGroups are the commits of a directory's update steps, in the order
// made, each titled for its port's update and its files composed from its
// step's tree, the last from the working files. A history that has them
// already, a commit for each with the same subject, is theirs, so a tidy
// again keeps it; else the directory's commits are combined into the
// first.
func stepGroups(steps []updateStep, whole TidyGroup, working []string, closes []string, person, author git.Signature) ([]TidyGroup, error) {
	matched := len(whole.Combines) == len(steps)
	for i, commit := range whole.Combines {
		matched = matched && commit.Subject() == steps[i].update.Subject
	}
	var groups []TidyGroup
	for i, step := range steps {
		group := TidyGroup{Directory: whole.Directory, Ports: []string{step.update.Port}, Paths: step.paths, FromEdits: true,
			Working: slices.ContainsFunc(step.paths, func(p string) bool { return slices.Contains(working, p) })}
		if i < len(steps)-1 {
			group.From = step.tree
		}
		switch {
		case matched:
			group.Combines = whole.Combines[i : i+1]
		case i == 0:
			group.Combines = whole.Combines
		}
		body := ""
		if step.update.Release != nil {
			body = step.update.Release.CommitBody()
		}
		var tickets []string
		if i == len(steps)-1 {
			tickets = closes
		}
		group.Message = commitmsg.Compose(step.update.Subject, body, commitMessages(group.Combines), tickets, true)
		var err error
		if group.Author, err = groupAuthor(group, person, author); err != nil {
			return nil, err
		}
		if group.Author.Name == "" {
			group.Blocking = append(group.Blocking, fmt.Sprintf("the commit for %s combines commits by %s; choose the attribution with --author", step.update.Port, strings.Join(authorsOf(group.Combines), " and ")))
		}
		groups = append(groups, group)
	}
	return groups, nil
}
