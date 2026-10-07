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
	t.Parallel()
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

	placeholder := `set maintainer to you as a Portfile's maintainers line writes you, such as "@you" for your GitHub login, or "{example.org:you @you}" with your email too, in ~/.dockhand/config.toml`
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
	t.Parallel()
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

// limitedReader is upstream discovery under GitHub's rate limit: the
// day's look finds jq outdated and fd held by the limit, until lifted,
// when fd is found outdated, unless the limit holds again.
type limitedReader struct {
	asked   []OutdatedRequest
	lifted  time.Time
	holdsOn bool
}

func (l *limitedReader) Outdated(_ context.Context, _ model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error) {
	l.asked = append(l.asked, request)
	fd := OutdatedPort{Port: "fd", Current: "10.1", Problem: "GitHub's rate limit for your login resets in 18 minutes", RetryAt: l.lifted}
	if len(request.Ports) > 0 {
		if !l.holdsOn {
			fd = OutdatedPort{Port: "fd", Current: "10.1", Newest: "10.2", Outdated: true}
		}
		return []OutdatedPort{fd}, nil
	}
	return []OutdatedPort{{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true}, fd}, nil
}

// The day's look a rate limit cut short goes over those ports once, just
// after it lifts, and isn't done until then; a limit that holds again
// leaves the day done without them, said once (the rc6 full stage, D-N4).
func TestTheDaysLookRetriesWhatARateLimitHeld(t *testing.T) {
	t.Parallel()
	for _, holdsOn := range []bool{false, true} {
		f := setup(t)
		e := f.open(t)
		e.Forge = forgetest.New("", "")
		morning := time.Date(2026, 10, 7, 8, 0, 0, 0, time.Local)
		reader := &limitedReader{lifted: morning.Add(18 * time.Minute), holdsOn: holdsOn}
		e.OutdatedReader = reader
		now := morning
		var said []string
		s := newServer(e, ServeOptions{Outdated: ServeOutdated{Maintainers: []string{"@ada"}, Mode: "list"}, Say: func(line string) { said = append(said, line) }, Now: func() time.Time { return now }})
		scanner := &outdatedScanner{s: s}
		scanner.maybe(t.Context())
		require.Contains(t, said, "serve: GitHub's rate limit kept 1 port from the day's look; it looks at them again at "+morning.Add(19*time.Minute).Format("15:04 MST"))
		_, err := os.Stat(e.serveFile("outdated.stamp"))
		require.ErrorIs(t, err, os.ErrNotExist, "the day isn't done")

		now = morning.Add(5 * time.Minute)
		scanner.maybe(t.Context())
		require.Len(t, reader.asked, 1, "not before the limit lifts")

		now = morning.Add(20 * time.Minute)
		said = nil
		scanner.maybe(t.Context())
		require.Len(t, reader.asked, 2)
		require.Equal(t, []string{"fd"}, reader.asked[1].Ports, "only what the limit held")
		_, err = os.Stat(e.serveFile("outdated.stamp"))
		require.NoError(t, err, "the day is done once the retry ran")
		if holdsOn {
			require.Contains(t, said, "serve: GitHub's rate limit kept 1 port from the day's look again; it's done for today without them")
			continue
		}
		look, ok := e.LastOutdatedLook()
		require.True(t, ok)
		require.ElementsMatch(t, []string{"jq", "fd"}, look.Outdated, "the retry adds to the day's look")
	}
}
