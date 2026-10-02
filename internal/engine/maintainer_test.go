package engine

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// maintainedPorts are fake ports whose index writes maintainers as given,
// by the spelling asked for, and which keep the sources asked about.
type maintainedPorts struct {
	fakePorts
	spellings map[string][]portindex.MaintainerSpelling
	asked     *[]model.Source
}

func (p maintainedPorts) Spellings(_ context.Context, source model.Source, spelling string) ([]portindex.MaintainerSpelling, error) {
	*p.asked = append(*p.asked, source)
	return p.spellings[spelling], nil
}

// signedOut is a forge nobody is signed in to.
type signedOut struct{ *forgetest.GitHub }

func (signedOut) AuthenticatedUser(context.Context) (string, error) {
	return "", errors.New("github: not signed in")
}

// The maintainer suggested to a person whose config names none is their
// own line, as the port index's Portfiles write the GitHub login dockhand
// signs in as: the one most write, with the others named to choose from.
// One a maintainers line can't carry isn't suggested. Where the login
// isn't known, or nothing names it, the placeholder stays (the flyctl
// run, macports/macports-ports#35069).
func TestTheMaintainerSuggestedIsTheLineThePortsWrite(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	e.Forge = forgetest.New("", "")
	var asked []model.Source
	own := macports.Maintainer{"example.org:ada", "@ada"}
	e.PortReader = maintainedPorts{asked: &asked, spellings: map[string][]portindex.MaintainerSpelling{"@ada": {
		{Maintainer: own, Portfiles: 41},
		{Maintainer: macports.Maintainer{"@ada"}, Portfiles: 3},
		{Maintainer: macports.Maintainer{"@ada", "ada$x"}, Portfiles: 1},
	}}}
	base := model.ObjectID(testsupport.Git(t, f.clone, "rev-parse", "HEAD"))
	tree := model.ObjectID(testsupport.Git(t, f.clone, "rev-parse", "HEAD^{tree}"))

	suggestion := e.SuggestMaintainer(t.Context(), base)
	require.Equal(t, []model.Source{{Commit: base, Tree: tree, Base: base}}, asked, "the index of the commit given")
	require.Equal(t, MaintainerSuggestion{Login: "ada", Spellings: []portindex.MaintainerSpelling{{Maintainer: own, Portfiles: 41}, {Maintainer: macports.Maintainer{"@ada"}, Portfiles: 3}}}, suggestion)
	require.Equal(t, `set maintainer = "{example.org:ada @ada}" in ~/.dockhand/config.toml, as 41 of the ports that name @ada write it, or as others do: "@ada" (3)`, MaintainerWords(suggestion, "~/.dockhand/config.toml"))
	suggestion.Spellings = suggestion.Spellings[:1]
	require.Equal(t, `set maintainer = "{example.org:ada @ada}" in ~/.dockhand/config.toml, as the ports that name @ada write it`, MaintainerWords(suggestion, "~/.dockhand/config.toml"))

	placeholder := `set maintainer = "{@you example.org:you}" in ~/.dockhand/config.toml`
	e.PortReader = maintainedPorts{asked: &asked}
	require.Equal(t, placeholder, MaintainerWords(e.SuggestMaintainer(t.Context(), base), "~/.dockhand/config.toml"), "nothing names the login")
	e.Forge = signedOut{}
	asked = nil
	require.Equal(t, MaintainerSuggestion{}, e.SuggestMaintainer(t.Context(), base))
	require.Empty(t, asked, "with no login, the index isn't read")
	require.Equal(t, placeholder, MaintainerWords(MaintainerSuggestion{}, "~/.dockhand/config.toml"))
}

// serve.for_outdated without a maintainer suggests the person's own line,
// from master as fetched, rather than a placeholder, and writes nothing.
func TestServeSuggestsTheMaintainerLineThePortsWrite(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	e.Forge = forgetest.New("", "")
	var asked []model.Source
	e.PortReader = maintainedPorts{asked: &asked, spellings: map[string][]portindex.MaintainerSpelling{"@ada": {{Maintainer: macports.Maintainer{"example.org:ada", "@ada"}, Portfiles: 2}}}}
	var said []string
	morning := time.Date(2026, 9, 30, 8, 0, 0, 0, time.Local)
	s := newServer(e, ServeOptions{Outdated: ServeOutdated{Mode: "list"}, Say: func(line string) { said = append(said, line) }, Now: func() time.Time { return morning }})
	(&outdatedScanner{s: s}).maybe(t.Context())
	require.Equal(t, []string{`serve: serve.for_outdated needs to know your ports: set maintainer = "{example.org:ada @ada}" in ~/.dockhand/config.toml, as the ports that name @ada write it`}, said)
	require.Len(t, asked, 1)
	require.Equal(t, f.upstreamMaster(t), asked[0].Commit, "master as fetched")

	// A serve started once the day's look has run says it at once, naming
	// the file the command read, and the look doesn't say it again (the
	// dogfood run with 251a1264).
	said = nil
	e.ConfigFile = "/tmp/elsewhere.toml"
	scanner := &outdatedScanner{s: s}
	scanner.announce(t.Context())
	require.Equal(t, []string{`serve: serve.for_outdated needs to know your ports: set maintainer = "{example.org:ada @ada}" in /tmp/elsewhere.toml, as the ports that name @ada write it`}, said)
	require.NoError(t, os.Remove(e.serveFile("outdated.stamp")))
	scanner.maybe(t.Context())
	require.Len(t, said, 1, "said once")
}
