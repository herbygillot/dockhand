package engine

import (
	"context"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// RevisionAnalysis is what a revision changes against its base, analyzed
// once and read by its change records, its assessments, and its plan,
// where each derived it for itself (C1, Codex's review of 386ac2cc,
// finding 4): the base commit and its tree, the revision's tree, the paths
// between them, and the ports and _resources those paths reach. It's
// keyed by tree and base, as the records and assessments are.
type RevisionAnalysis struct {
	Base, BaseTree, Tree model.ObjectID
	Paths                []string
	Scope                macports.Scope
}

// Sources are the base's source and the revision's, as an evaluation
// reads each.
func (a RevisionAnalysis) Sources() [2]model.Source {
	return [2]model.Source{{Commit: a.Base, Tree: a.BaseTree, Base: a.Base}, {Tree: a.Tree, Base: a.Base}}
}

// revisionKey keys an analysis: a revision's tree against its base.
type revisionKey struct{ base, tree model.ObjectID }

// analyze is the revision's analysis, made once for each tree and base in
// the engine's life, since both are Git objects, which don't change.
func (e *Engine) analyze(ctx context.Context, base, tree model.ObjectID) (RevisionAnalysis, error) {
	key := revisionKey{base, tree}
	e.lazy.Lock()
	analysis, ok := e.analyses[key]
	e.lazy.Unlock()
	if ok {
		return analysis.copy(), nil
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return RevisionAnalysis{}, err
	}
	analysis = RevisionAnalysis{Base: base, BaseTree: model.ObjectID(trees[string(base)]), Tree: tree}
	if analysis.Paths, err = e.Repo.ChangedPaths(ctx, string(analysis.BaseTree), string(tree)); err != nil {
		return RevisionAnalysis{}, err
	}
	if analysis.Scope, err = placedScope(ctx, e.Repo, string(analysis.BaseTree), string(tree), analysis.Paths); err != nil {
		return RevisionAnalysis{}, err
	}
	e.keepAnalysis(analysis)
	return analysis.copy(), nil
}

// placedScope is the scope of the paths changed between two trees, each
// changed directory placed by its Portfile on each side, so a removed one
// is no port to build and a moved one is read at its base's directory
// (macports.Scope.Placed).
func placedScope(ctx context.Context, repo *git.Repository, before, after string, changed []string) (macports.Scope, error) {
	scope := macports.ScopeOf(changed)
	var portfiles []string
	for _, directory := range scope.Ports {
		portfiles = append(portfiles, directory+"/Portfile")
	}
	held := func(tree string) (map[string]bool, error) {
		blobs, err := repo.FileBlobs(ctx, tree, portfiles)
		if err != nil {
			return nil, err
		}
		directories := map[string]bool{}
		for file := range blobs {
			directories[strings.TrimSuffix(file, "/Portfile")] = true
		}
		return directories, nil
	}
	inBase, err := held(before)
	if err != nil {
		return macports.Scope{}, err
	}
	inHead, err := held(after)
	if err != nil {
		return macports.Scope{}, err
	}
	return scope.Placed(inBase, inHead), nil
}

// keepAnalysis keeps a revision's analysis for analyze, by its base and
// tree, which never change: status read every branch's in one diff-tree
// (Then's item 6).
func (e *Engine) keepAnalysis(analysis RevisionAnalysis) {
	e.lazy.Lock()
	defer e.lazy.Unlock()
	if e.analyses == nil {
		e.analyses = map[revisionKey]RevisionAnalysis{}
	}
	e.analyses[revisionKey{analysis.Base, analysis.Tree}] = analysis
}

// copy is the analysis with slices of its own, so a reader that changes
// one leaves the engine's as it was.
func (a RevisionAnalysis) copy() RevisionAnalysis {
	a.Paths = slices.Clone(a.Paths)
	a.Scope.Ports = slices.Clone(a.Scope.Ports)
	a.Scope.Removed = slices.Clone(a.Scope.Removed)
	a.Scope.Moved = slices.Clone(a.Scope.Moved)
	return a
}
