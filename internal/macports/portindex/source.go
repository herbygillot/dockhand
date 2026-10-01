package portindex

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Source supplies the index of a tree: staged into the tree's root when it
// is not there yet, and opened from there after. Consumers that resolve
// names, discover dependents, select a survey's ports, or pack a source
// for verification ask a Source and never stage for themselves.
type Source interface {
	Index(context.Context, macports.Tree) (*Index, error)
}

// Stager is the Source that stages. It installs a tree's index into the
// tree's root once, Config resolving the indexer and Repo reading the
// sources, opens it from there after, and remembers what it staged, so a
// command that resolves names repeatedly against one tree indexes it once.
// A tree that names no platform is indexed for the native one.
//
// WithoutBase builds a tree's index from the nearest cached generation
// rather than from the tree's recorded base. Name lookup prefers that, as
// it needs no generation of master to resolve a name; verification keeps
// the base, so a candidate's index derives from it, and a snapshot, which
// has no commit, is bracketed against the mirror's index by its base's.
type Stager struct {
	Repo   *git.Repository
	Config Config
	// NativePlatform supplies the platform for a tree that names none; nil
	// refuses such a tree.
	NativePlatform func(context.Context) (model.Platform, error)
	WithoutBase    bool

	mu     sync.Mutex
	staged map[string]string
}

// Index stages the tree's index for its platform on first sight, and opens
// the generation it staged, which is that platform's whoever installs
// another into the same root: Tart stages two releases of one revision at
// once, in one workspace, and the second was handed the first's index.
func (s *Stager) Index(ctx context.Context, tree macports.Tree) (*Index, error) {
	if s == nil || s.Repo == nil {
		return nil, fmt.Errorf("portindex: a repository is required to stage an index")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	key := strings.Join([]string{tree.Root(), string(tree.Source().Tree), platform.OS, platform.Version, platform.Architecture}, "\x00")
	if entry, ok := s.staged[key]; ok {
		return Open(entry)
	}
	source := tree.Source()
	if s.WithoutBase {
		source.Base = ""
	}
	entry, err := Stage(ctx, s.Repo, source, platform, s.Config, tree)
	if err != nil {
		return nil, err
	}
	if s.staged == nil {
		s.staged = map[string]string{}
	}
	s.staged[key] = entry
	return Open(entry)
}
