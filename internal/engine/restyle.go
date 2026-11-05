package engine

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
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
	// After the declaration's commit, where tidy writes one.
	from := plan.Final
	if plan.Declare != nil {
		from = plan.Declared
	}
	changed, err := worktree.ChangedPaths(ctx, plan.BaseTree, from)
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
		state, text, err := worktree.File(ctx, from, name)
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
		if why := e.restyleEvaluates(ctx, ports.Families, worktree, from, directory, edit); why != "" {
			plan.NotRestyled = append(plan.NotRestyled, name+": "+why)
			continue
		}
		edits = append(edits, edit)
		names = append(names, directoryName(directory))
	}
	if len(edits) == 0 {
		return nil
	}
	if plan.Restyled, err = worktree.EditTree(ctx, from, edits); err != nil {
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
	for _, change := range fidelity.SubportChanges(directory, &before, &after, nil) {
		if change.Kind != model.SubportUnchanged {
			return fmt.Sprintf("%s evaluates differently after restyling", change.Port)
		}
	}
	return ""
}

// pathStyleTidy adds to a tidy plan --restyle's commit after the
// whitespace one, "<port>: path-style dependencies": each literal port:
// dependency of each Portfile the branch changes written as the tree
// writes it path-style, where another port can satisfy it, as the
// path-style notice offers (commitrules.CheckDependencies, through the
// same providers and treeSpec). A Portfile that evaluates otherwise, its
// dependencies read by phase and port alone, or can't be evaluated, is
// left as it was, and said.
func (e *Engine) pathStyleTidy(ctx context.Context, plan *TidyPlan) error {
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return err
	}
	tree := plan.Final
	if plan.Declare != nil {
		tree = plan.Declared
	}
	if plan.Restyle != nil {
		tree = plan.Restyled
	}
	changed, err := worktree.ChangedPaths(ctx, plan.BaseTree, tree)
	if err != nil {
		return err
	}
	ports, err := e.capabilities()
	if err != nil {
		return err
	}
	providers := e.providers(ctx, tree)
	if providers == nil {
		return nil
	}
	var edits []git.FileEdit
	var names, rewrites []string
	for _, name := range changed {
		if path.Base(name) != "Portfile" {
			continue
		}
		state, text, err := worktree.File(ctx, tree, name)
		if err != nil {
			return err
		}
		if !state.Exists {
			continue
		}
		rewritten, words := pathStyled(text, providers)
		if len(words) == 0 {
			continue
		}
		directory := path.Dir(name)
		edit := git.FileEdit{Path: name, Before: state, After: rewritten, Mode: state.Mode}
		if why := e.pathStyleEvaluates(ctx, ports.Families, worktree, tree, directory, edit); why != "" {
			plan.NotRestyled = append(plan.NotRestyled, name+": "+why)
			continue
		}
		edits = append(edits, edit)
		names = append(names, directoryName(directory))
		rewrites = append(rewrites, words...)
	}
	if len(edits) == 0 {
		return nil
	}
	if plan.PathStyled, err = worktree.EditTree(ctx, tree, edits); err != nil {
		return err
	}
	author, err := worktree.Author(ctx)
	if err != nil {
		return err
	}
	author.When = e.now()
	slices.Sort(names)
	body := "Writes " + prose.And(rewrites) + "."
	group := TidyGroup{Ports: names, Message: commitmsg.Compose(strings.Join(names, ", ")+": path-style dependencies", body, nil, nil, true), Author: author, Working: true,
		Notes: []string{"rewritten by --restyle, each port depending on the same ports"}}
	for _, edit := range edits {
		group.Paths = append(group.Paths, edit.Path)
	}
	plan.PathStyle, plan.pathStyled, plan.Keep = &group, edits, false
	return nil
}

// pathStyled is a Portfile with each literal port: dependency the
// path-style rule would offer a form for written so, and the words that
// say each rewrite, "pkgconfig as path:bin/pkg-config:pkgconfig, which
// pkgconf satisfies too", once a port.
func pathStyled(text []byte, providers commitrules.Providers) ([]byte, []string) {
	dependencies := portfile.PortDependencies(text)
	var words []string
	said := map[string]bool{}
	// From the last, so each earlier word's span still holds.
	for i := len(dependencies) - 1; i >= 0; i-- {
		dependency := dependencies[i]
		others, spec := providers(dependency.Port)
		if len(others) == 0 || spec == "" {
			continue
		}
		text = portfile.RewriteDependency(text, dependency, spec)
		if !said[dependency.Port] {
			said[dependency.Port] = true
			words = append([]string{fmt.Sprintf("%s as %s, which %s satisfies too", dependency.Port, spec, strings.Join(others, " or "))}, words...)
		}
	}
	return text, words
}

// pathStyleEvaluates says why a path-styled Portfile can't be committed:
// a subport evaluates otherwise, its dependencies read by phase and port
// alone, or it can't be evaluated; empty where it evaluates the same.
func (e *Engine) pathStyleEvaluates(ctx context.Context, families FamilyReader, worktree *git.Repository, tree, directory string, edit git.FileEdit) string {
	if families == nil {
		return "nothing here evaluates it, to show writing it path-style changes nothing"
	}
	styled, err := worktree.EditTree(ctx, tree, []git.FileEdit{edit})
	if err != nil {
		return err.Error()
	}
	before, err := families.Family(ctx, model.Source{Tree: model.ObjectID(tree)}, directory)
	if err != nil {
		return "it couldn't be evaluated: " + err.Error()
	}
	after, err := families.Family(ctx, model.Source{Tree: model.ObjectID(styled)}, directory)
	if err != nil {
		return "it couldn't be evaluated path-style: " + err.Error()
	}
	for _, change := range fidelity.SubportChanges(directory, fidelity.DependenciesByPort(&before), fidelity.DependenciesByPort(&after), nil) {
		if change.Kind != model.SubportUnchanged {
			return fmt.Sprintf("%s evaluates differently after rewriting dependencies in path style", change.Port)
		}
	}
	return ""
}
