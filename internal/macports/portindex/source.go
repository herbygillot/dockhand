package portindex

import (
	"context"
	"fmt"
	"sync"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// Source supplies the index of a tree: staged into the tree's root when it
// is not there yet, and opened from there after. Consumers that resolve
// names, discover dependents, select a survey's ports, or pack a source
// for verification ask a Source and never stage for themselves.
type Source interface {
	Index(context.Context, macports.Tree) (*Index, error)
}

// SourceFunc is a Source written as a function, for a test's fixture index.
type SourceFunc func(context.Context, macports.Tree) (*Index, error)

func (f SourceFunc) Index(ctx context.Context, tree macports.Tree) (*Index, error) {
	return f(ctx, tree)
}

// Stager is the Source that stages. It installs a tree's index into the
// tree's root once, Config resolving the indexer and Repo reading the
// sources, opens it from there after, and remembers what it staged, so a
// command that resolves names repeatedly against one tree indexes it once.
// A tree that names no platform is indexed for the native one.
//
// WithoutBase builds a tree's index from the nearest cached generation
// rather than from the tree's recorded base. Name lookup prefers that, as
// it needs no generation of master to resolve a name; verification and
// discovery keep the base, so a candidate's index derives from it.
type Stager struct {
	Repo   *git.Repository
	Config Config
	// NativePlatform supplies the platform for a tree that names none; nil
	// refuses such a tree.
	NativePlatform func(context.Context) (record.Platform, error)
	WithoutBase    bool

	mu     sync.Mutex
	staged map[string]bool
}

// Index stages the tree's index on first sight and opens it.
func (s *Stager) Index(ctx context.Context, tree macports.Tree) (*Index, error) {
	if s == nil || s.Repo == nil {
		return nil, fmt.Errorf("portindex: a repository is required to stage an index")
	}
	key := tree.Root() + "\x00" + string(tree.Source().Tree)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.staged[key] {
		return Open(tree.Root())
	}
	// A workspace projects a tree the repository already validated, and
	// may hold nothing yet; a plain materialization is checked.
	if !tree.Projected() {
		if err := macports.ValidatePortsTree(tree.Root(), s.Repo.Root); err != nil {
			return nil, err
		}
	}
	platform := tree.Platform()
	if platform.OS == "" {
		if s.NativePlatform == nil {
			return nil, fmt.Errorf("portindex: the tree names no platform to index for")
		}
		var err error
		if platform, err = s.NativePlatform(ctx); err != nil {
			return nil, err
		}
	}
	source := tree.Source()
	if s.WithoutBase {
		source.Base = ""
	}
	if err := Stage(ctx, s.Repo, source, platform, s.Config, tree); err != nil {
		return nil, err
	}
	if s.staged == nil {
		s.staged = map[string]bool{}
	}
	s.staged[key] = true
	return Open(tree.Root())
}
