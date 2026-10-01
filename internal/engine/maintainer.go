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

// SuggestMaintainerAtMaster is SuggestMaintainer at master as fetched now,
// for a command that has no branch; empty where master can't be fetched.
func (e *Engine) SuggestMaintainerAtMaster(ctx context.Context) MaintainerSuggestion {
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return MaintainerSuggestion{}
	}
	return e.SuggestMaintainer(ctx, master)
}

// configFile is the configuration file a hint names: the one the command
// read, or the default where the engine wasn't told (the dogfood run with
// 251a1264, whose $DOCKHAND_CONFIG hints didn't name).
func (e *Engine) configFile() string {
	if e.ConfigFile != "" {
		return e.ConfigFile
	}
	return "~/.dockhand/config.toml"
}

// MaintainerHint is MaintainerWords for this engine's person, at master,
// naming the file the command read.
func (e *Engine) MaintainerHint(ctx context.Context) string {
	return MaintainerWords(e.SuggestMaintainerAtMaster(ctx), e.configFile())
}

// maintainerPlaceholder is the maintainer suggested where none of the
// person's own can be.
const maintainerPlaceholder = "{@you example.org:you}"

// MaintainerWords say what to set as maintainer in dockhand's config, the
// file named, as the command read it: the entry most of the ports naming
// the person's login write, as they write it, and the others they write,
// to choose from; or, with nothing to suggest, a placeholder to fill in.
func MaintainerWords(s MaintainerSuggestion, file string) string {
	if len(s.Spellings) == 0 {
		return fmt.Sprintf("set maintainer = %q in %s", maintainerPlaceholder, file)
	}
	first, handle := s.Spellings[0], "@"+s.Login
	words := fmt.Sprintf("set maintainer = %q in %s", first.Maintainer.String(), file)
	if len(s.Spellings) == 1 {
		return words + ", as the ports that name " + handle + " write it"
	}
	var others []string
	for _, other := range s.Spellings[1:] {
		others = append(others, fmt.Sprintf("%q (%d)", other.Maintainer.String(), other.Portfiles))
	}
	return fmt.Sprintf("%s, as %d of the ports that name %s write it, or as others do: %s", words, first.Portfiles, handle, strings.Join(others, ", "))
}
