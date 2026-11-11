package workspace

import (
	"context"
	"errors"
	"sync"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// Registry hands out one workspace per source within a process and closes
// it when the last holder releases it. The providers, staging, treequery,
// the survey, and editprep work on the same tree, and share one whole-tree
// workspace through it rather than materializing the tree twice; overlays are not
// registered. A nil Registry shares nothing: Acquire opens a workspace of
// its own and release closes it.
type Registry struct {
	mu   sync.Mutex
	held map[holdingKey]*holding
}

type holdingKey struct {
	repo   string
	source model.Source
}

type holding struct {
	workspace *Workspace
	holders   int
}

// Acquire returns the workspace for the source, opening it on first use,
// and the release that gives it back. The workspace holds whatever scope
// earlier holders ensured; a holder that needs more ensures it. It is
// closed when the last holder releases it.
func (r *Registry) Acquire(ctx context.Context, repo *git.Repository, source model.Source) (*Workspace, func() error, error) {
	if r == nil {
		w, err := Open(ctx, repo, source)
		if err != nil {
			return nil, nil, err
		}
		return w, w.Close, nil
	}
	if repo == nil {
		return nil, nil, errors.New("workspace: a repository is required")
	}
	// A workspace another holder opened is staged already; the door is
	// asked for each holder's work all the same.
	if err := admit(ctx); err != nil {
		return nil, nil, err
	}
	key := holdingKey{repo: repo.CommonDir, source: source}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held == nil {
		r.held = map[holdingKey]*holding{}
	}
	entry := r.held[key]
	if entry == nil {
		w, err := Open(ctx, repo, source)
		if err != nil {
			return nil, nil, err
		}
		entry = &holding{workspace: w}
		r.held[key] = entry
	}
	entry.holders++
	var once sync.Once
	release := func() error {
		var err error
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			entry.holders--
			if entry.holders > 0 || r.held[key] != entry {
				return
			}
			delete(r.held, key)
			err = entry.workspace.Close()
		})
		return err
	}
	return entry.workspace, release, nil
}
