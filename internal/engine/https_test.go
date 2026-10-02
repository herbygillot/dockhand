package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

type roundTrips func(*http.Request) (*http.Response, error)

func (f roundTrips) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The probe create and update ask with doesn't count an HTTPS URL that
// redirects to plain HTTP as answering over HTTPS: fetching refuses that
// downgrade, and so does the probe now (the helper-ownership review's
// finding 2, its probe as a regression test).
func TestTheHTTPSProbeRejectsADowngrade(t *testing.T) {
	t.Parallel()
	probe := requestProbe{client: &http.Client{Transport: roundTrips(func(r *http.Request) (*http.Response, error) {
		status, header := http.StatusOK, http.Header{}
		if r.URL.Scheme == "https" && r.URL.Path == "/" {
			status = http.StatusFound
			header.Set("Location", "http://example.invalid/")
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	require.False(t, probe.Answers(t.Context(), "https://example.invalid/"))
	require.True(t, probe.Answers(t.Context(), "https://example.invalid/secure"))
}

// gatedProbe answers as testsupport.HTTPSAnswers does, once its gate opens, and counts
// each URL's asks, the asks in flight, and the most in flight at once.
type gatedProbe struct {
	answers testsupport.HTTPSAnswers
	open    chan struct{}
	mu      sync.Mutex
	asked   map[string]int
	flying  int
	most    int
}

// newGatedProbe is a probe whose gate is shut, for the test to open, or
// open already.
func newGatedProbe(answers testsupport.HTTPSAnswers, shut bool) *gatedProbe {
	probe := &gatedProbe{answers: answers, open: make(chan struct{}), asked: map[string]int{}}
	if !shut {
		close(probe.open)
	}
	return probe
}

func (g *gatedProbe) Answers(ctx context.Context, url string) bool {
	g.mu.Lock()
	g.asked[url]++
	g.flying++
	g.most = max(g.most, g.flying)
	g.mu.Unlock()
	select {
	case <-g.open:
	case <-ctx.Done():
	}
	g.mu.Lock()
	g.flying--
	g.mu.Unlock()
	return g.answers[url]
}

func (g *gatedProbe) inFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.flying
}

// A port's plain-HTTP URLs are asked over HTTPS a few at once, not each in
// turn for up to ten seconds, and each once, and they're said in the order
// the port names them (the update-workflow review's efficiency item).
func TestAPortsURLsAreAskedTogetherEachOnce(t *testing.T) {
	t.Parallel()
	var sites []string
	for i := range 6 {
		sites = append(sites, fmt.Sprintf("http://mirror%d.example/jq/", i))
	}
	info := macports.PortInfo{Options: map[string]string{"homepage": "http://jqlang.example/",
		"master_sites": strings.Join(append(sites, "http://mirror2.example/jq/:src", "http://jqlang.example/"), " ")}}
	probe := newGatedProbe(testsupport.HTTPSAnswers{"https://mirror3.example/jq/": true}, true)
	e := &Engine{HTTPS: probe}
	said := make(chan []PlainURL)
	go func() { said <- e.plainHTTP(t.Context(), info, nil) }()
	require.Eventually(t, func() bool { return probe.inFlight() == httpsAsks }, 5*time.Second, time.Millisecond, "asked together, not in turn")
	time.Sleep(50 * time.Millisecond) // were there no bound, the rest would be asked by now
	require.Equal(t, httpsAsks, probe.inFlight(), "no more than that at once")
	close(probe.open)
	plain := <-said
	require.Equal(t, httpsAsks, probe.most)
	want := []PlainURL{{PlainURL: macports.PlainURL{Option: "homepage", URL: "http://jqlang.example/"}, HTTPS: "https://jqlang.example/"}}
	for i, site := range sites {
		want = append(want, PlainURL{PlainURL: macports.PlainURL{Option: "master_sites", URL: site}, HTTPS: "https://" + strings.TrimPrefix(site, "http://"), Answers: i == 3})
	}
	require.Equal(t, want, plain)
	for _, url := range want {
		require.Equal(t, 1, probe.asked[url.HTTPS], url.HTTPS)
	}
	require.Nil(t, e.plainHTTP(t.Context(), macports.PortInfo{Options: map[string]string{"homepage": "https://jqlang.example/"}}, nil))
}

// Answers a command has had already aren't asked again, and new ones join
// them: create asks a homepage before writing it, and its checksum refresh
// would otherwise ask again (batch 21).
func TestAnAnswerHadIsntAskedAgain(t *testing.T) {
	t.Parallel()
	probe := newGatedProbe(testsupport.HTTPSAnswers{"https://jqlang.example/": true}, false)
	e := &Engine{HTTPS: probe}
	answered := map[string]bool{"https://dl.example/": false}
	plain := e.plainHTTP(t.Context(), macports.PortInfo{Options: map[string]string{"homepage": "http://jqlang.example/", "master_sites": "http://dl.example/"}}, answered)
	require.Len(t, plain, 2)
	require.True(t, plain[0].Answers)
	require.False(t, plain[1].Answers)
	require.Zero(t, probe.asked["https://dl.example/"], "answered already")
	require.Equal(t, 1, probe.asked["https://jqlang.example/"])
	require.Equal(t, map[string]bool{"https://dl.example/": false, "https://jqlang.example/": true}, answered)
	e.plainHTTP(t.Context(), macports.PortInfo{Options: map[string]string{"homepage": "http://jqlang.example/"}}, answered)
	require.Equal(t, 1, probe.asked["https://jqlang.example/"], "asked once in all")
}
