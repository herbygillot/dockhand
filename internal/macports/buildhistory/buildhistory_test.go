package buildhistory

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// site stands in for ports.macports.org's API, answering each port by
// name and counting what it's asked.
type site struct {
	*httptest.Server
	mu    sync.Mutex
	asked map[string]int
}

func newSite(t *testing.T, answers map[string]string) *site {
	s := &site{asked: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := r.URL.Query().Get("port_name")
		s.mu.Lock()
		s.asked[port]++
		s.mu.Unlock()
		if r.URL.Path != "/api/v1/builds/" || r.Header.Get("Accept") != "application/json" {
			http.NotFound(w, r)
			return
		}
		switch port {
		case "gone":
			http.NotFound(w, r)
		case "busy":
			http.Error(w, "busy", http.StatusBadGateway)
		default:
			answer, ok := answers[port]
			if !ok {
				answer = `{"count":0,"next":null,"previous":null,"results":[]}`
			}
			fmt.Fprint(w, answer)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *site) count(port string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[port]
}

// builds is the API's page of builds, each "builder status time".
func builds(port string, each ...string) string {
	var results []string
	for _, build := range each {
		fields := strings.SplitN(build, " ", 3)
		results = append(results, fmt.Sprintf(`{"port_name":%q,"builder_name":{"name":%q,"display_name":""},"build_id":1,"status":%q,"time_start":%q,"time_elapsed":"00:01:00","watcher_id":1}`,
			port, fields[0], fields[2], fields[1]))
	}
	return fmt.Sprintf(`{"count":%d,"next":null,"previous":null,"results":[%s]}`, len(results), strings.Join(results, ","))
}

// clock is a test's clock, moved by hand.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) pass(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

var bazel = builds("bazel",
	"15_arm64 2026-10-25T23:09:28Z build successful",
	"15_arm64 2026-10-25T22:50:06Z retry exception slave lost",
	"14_arm64 2026-10-25T20:31:10Z failed install-port",
	"10.14_x86_64 2026-02-14T22:34:29Z failed install-dependencies",
	"10.13_x86_64 2026-02-14T22:26:27Z failed install-dependencies",
	"14_arm64 2026-02-14T20:00:00Z build successful",
	"11_x86_64 2026-02-14T22:22:54Z build successful",
	"breachcraft-builder-1 2026-02-14T22:00:00Z build successful",
)

// A port's history is its builds on several releases, each builder's
// newest that passed or failed standing for it: a retry says nothing, and
// a builder whose name isn't a release's is left out.
func TestAPortsHistoryIsEachBuildersLatest(t *testing.T) {
	s := newSite(t, map[string]string{"bazel": bazel})
	r := &Reader{Site: s.URL, HTTP: s.Client()}

	history, ok := r.History(t.Context(), "bazel")
	require.True(t, ok)
	require.Equal(t, "bazel", history.Port)
	latest, ok := history.Latest("15_arm64")
	require.True(t, ok)
	require.True(t, latest.Built(), "the retry before it says nothing")
	latest, ok = history.Latest("14_arm64")
	require.True(t, ok)
	require.True(t, latest.Failed(), "its newest build failed, though an older one built")
	require.Equal(t, "failed", latest.Words())
	_, ok = history.Latest("26_arm64")
	require.False(t, ok, "a builder it never built on says nothing")

	var builders []string
	for _, build := range history.Latests() {
		builders = append(builders, build.Builder)
	}
	require.Equal(t, []string{"10.13_x86_64", "10.14_x86_64", "11_x86_64", "14_arm64", "15_arm64"}, builders, "oldest release first, a builder not of a release left out")
	require.Equal(t, "failed on macOS 10.13 and 10.14 x86_64 at its dependencies; failed on macOS 14 arm64; built on the other 2", history.Words())
}

// A history every builder built says so; one with no builds says that.
func TestAHistorysWords(t *testing.T) {
	built := History{Builds: []Build{{Builder: "15_arm64", Status: statusBuilt}, {Builder: "26_arm64", Status: statusBuilt}}}
	require.Equal(t, "built on all 2 builders", built.Words())
	require.Equal(t, "built on its one builder, macOS 15 arm64", History{Builds: built.Builds[:1]}.Words())
	require.Equal(t, "no builds on ports.macports.org", History{Builds: []Build{}}.Words())
	require.Equal(t, "macOS 10.6 i386", Release("10.6_i386"))
	require.Equal(t, "breachcraft-builder-1", Release("breachcraft-builder-1"))
}

// A platform names MacPorts' builder for its release and architecture.
func TestAPlatformsBuilder(t *testing.T) {
	builder, ok := Builder(model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"})
	require.True(t, ok)
	require.Equal(t, "15_arm64", builder)
	builder, ok = Builder(model.Platform{OS: "darwin", Version: "10", Architecture: "x86_64"})
	require.True(t, ok)
	require.Equal(t, "10.6_x86_64", builder)
	_, ok = Builder(model.Platform{OS: "linux", Version: "6", Architecture: "x86_64"})
	require.False(t, ok)
	_, ok = Builder(model.Platform{OS: "darwin", Version: "99", Architecture: "arm64"})
	require.False(t, ok, "a release not known has no builder's name")
}

// A port the site has no builds of has an empty history, kept like any
// other; a 404, a 5xx, or a site that doesn't answer says nothing and
// keeps nothing.
func TestAPortWithNoneAndASiteThatRefuses(t *testing.T) {
	s := newSite(t, nil)
	cache := t.TempDir()
	r := &Reader{Site: s.URL, Cache: cache, HTTP: s.Client()}

	history, ok := r.History(t.Context(), "newport")
	require.True(t, ok)
	require.Empty(t, history.Builds)
	require.Equal(t, "no builds on ports.macports.org", history.Words())
	_, ok = r.History(t.Context(), "newport")
	require.True(t, ok)
	require.Equal(t, 1, s.count("newport"), "kept")

	for _, port := range []string{"gone", "busy"} {
		_, ok = r.History(t.Context(), port)
		require.False(t, ok, port)
		require.NoFileExists(t, filepath.Join(cache, port+".json"))
		_, ok = r.History(t.Context(), port)
		require.False(t, ok)
		require.Equal(t, 2, s.count(port), "%s isn't kept, so it's asked again", port)
	}

	s.Close()
	_, ok = (&Reader{Site: s.URL, HTTP: s.Client()}).History(t.Context(), "bazel")
	require.False(t, ok, "a site that doesn't answer says nothing")
	_, ok = (*Reader)(nil).History(t.Context(), "bazel")
	require.False(t, ok, "a nil reader reads nothing")
	_, ok = r.History(t.Context(), "../etc")
	require.False(t, ok, "a name that isn't a port's isn't asked")
}

// A history is kept a day, in the reader's life and in its cache, where
// another reader finds it, and asked again once it's a day old.
func TestAHistoryIsKeptADay(t *testing.T) {
	s := newSite(t, map[string]string{"bazel": bazel})
	when := &clock{at: time.Now()}
	cache := t.TempDir()
	r := &Reader{Site: s.URL, Cache: cache, HTTP: s.Client(), Now: when.now}

	_, ok := r.History(t.Context(), "bazel")
	require.True(t, ok)
	require.FileExists(t, filepath.Join(cache, "bazel.json"))
	when.pass(23 * time.Hour)
	_, ok = r.History(t.Context(), "bazel")
	require.True(t, ok)
	another := &Reader{Site: s.URL, Cache: cache, HTTP: s.Client(), Now: when.now}
	history, ok := another.History(t.Context(), "bazel")
	require.True(t, ok)
	require.Len(t, history.Builds, 8, "kept whole")
	require.Equal(t, 1, s.count("bazel"), "asked once a day")

	when.pass(2 * time.Hour)
	_, ok = another.History(t.Context(), "bazel")
	require.True(t, ok)
	require.Equal(t, 2, s.count("bazel"), "a day-old history is asked again")

	require.NoError(t, os.WriteFile(filepath.Join(cache, "bazel.json"), []byte("not json"), 0o644))
	now := when.now()
	require.NoError(t, os.Chtimes(filepath.Join(cache, "bazel.json"), now, now))
	_, ok = (&Reader{Site: s.URL, Cache: cache, HTTP: s.Client(), Now: when.now}).History(t.Context(), "bazel")
	require.True(t, ok)
	require.Equal(t, 3, s.count("bazel"), "what can't be read is asked again")
}
