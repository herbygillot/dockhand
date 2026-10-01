package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
)

// MaintainerReader reads the ways a source's port index writes a
// maintainer (portindex.Index.Spellings). MacPorts' port index, as the
// port reader stages it, is the real one.
type MaintainerReader interface {
	Spellings(ctx context.Context, source model.Source, spelling string) ([]portindex.MaintainerSpelling, error)
}

// MaintainerSuggestion is what a person whose config names no maintainer
// might set there (the flyctl run, macports/macports-ports#35069): the
// ways the port index's Portfiles write the GitHub login dockhand signs in
// as, most written first, each one a maintainers line can carry. Login is
// empty where it isn't known, and Spellings where nothing names it.
type MaintainerSuggestion struct {
	Login     string
	Spellings []portindex.MaintainerSpelling
}

// SuggestMaintainer reads how the port index at a commit writes the GitHub
// login dockhand signs in as, @login, for a person whose config names no
// maintainer. It only suggests: what can't be read, the login or the
// index, leaves it empty, and nothing is written.
func (e *Engine) SuggestMaintainer(ctx context.Context, commit model.ObjectID) MaintainerSuggestion {
	login, err := e.forge().AuthenticatedUser(ctx)
	if err != nil || login == "" {
		return MaintainerSuggestion{}
	}
	suggestion := MaintainerSuggestion{Login: login}
	ports, err := e.portReader()
	if err != nil {
		return suggestion
	}
	reader, ok := ports.(MaintainerReader)
	if !ok {
		return suggestion
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(commit)})
	if err != nil {
		return suggestion
	}
	found, err := reader.Spellings(ctx, model.Source{Commit: commit, Tree: model.ObjectID(trees[string(commit)]), Base: commit}, "@"+login)
	if err != nil {
		return suggestion
	}
	for _, spelling := range found {
		if macports.CheckMaintainers(spelling.Maintainer.String()) == nil {
			suggestion.Spellings = append(suggestion.Spellings, spelling)
		}
	}
	return suggestion
}

// maintainerPlaceholder is the maintainer suggested where none of the
// person's own can be.
const maintainerPlaceholder = "{@you example.org:you}"

// MaintainerWords say what to set as maintainer in dockhand's config: the
// entry most of the ports naming the person's login write, as they write
// it, and the others they write, to choose from; or, with nothing to
// suggest, a placeholder to fill in.
func MaintainerWords(s MaintainerSuggestion) string {
	if len(s.Spellings) == 0 {
		return fmt.Sprintf("set maintainer = %q in ~/.dockhand/config.toml", maintainerPlaceholder)
	}
	first, handle := s.Spellings[0], "@"+s.Login
	words := fmt.Sprintf("set maintainer = %q in ~/.dockhand/config.toml", first.Maintainer.String())
	if len(s.Spellings) == 1 {
		return words + ", as the ports that name " + handle + " write it"
	}
	var others []string
	for _, other := range s.Spellings[1:] {
		others = append(others, fmt.Sprintf("%q (%d)", other.Maintainer.String(), other.Portfiles))
	}
	return fmt.Sprintf("%s, as %d of the ports that name %s write it, or as others do: %s", words, first.Portfiles, handle, strings.Join(others, ", "))
}
