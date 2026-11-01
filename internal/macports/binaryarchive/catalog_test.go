package binaryarchive

import (
	"archive/tar"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// site stands in for packages.macports.org, counting what it's asked by
// path.
type site struct {
	*httptest.Server
	mu    sync.Mutex
	asked map[string]int
}

func newSite(t *testing.T, serve func(w http.ResponseWriter, r *http.Request)) *site {
	s := &site{asked: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.asked[r.URL.Path]++
		s.mu.Unlock()
		serve(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *site) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[path]
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

// A port's listing is asked of its directory once and kept a day, in the
// catalog's life and in its directory, where another catalog finds it; a
// read that may not fetch has what's kept and nothing else.
func TestAListingIsAvailableAndKeptADay(t *testing.T) {
	s := newSite(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<a href="jq-1.8.1_0.darwin_25.arm64.tbz2">`)
	})
	when := &clock{at: time.Now()}
	listings := t.TempDir()
	c := &Catalog{Site: s.URL, Listings: listings, HTTP: s.Client(), Now: when.now}

	_, availability := c.Listing(t.Context(), "jq", false)
	require.Equal(t, Unavailable, availability, "nothing kept, and not asked")
	require.Zero(t, s.count("/jq/"))

	listing, availability := c.Listing(t.Context(), "jq", true)
	require.Equal(t, Available, availability)
	require.True(t, Listed(listing, "jq", "1.8.1", "0", 25, "arm64"))
	require.Equal(t, 1, s.count("/jq/"), "asked of the port's directory")
	kept, err := os.ReadFile(filepath.Join(listings, "jq.html"))
	require.NoError(t, err)
	require.Equal(t, listing, kept, "kept as the site's page")

	_, availability = c.Listing(t.Context(), "jq", true)
	require.Equal(t, Available, availability)
	require.Equal(t, 1, s.count("/jq/"), "kept in the catalog's life")
	another := &Catalog{Site: s.URL, Listings: listings, HTTP: s.Client(), Now: when.now}
	listing, availability = another.Listing(t.Context(), "jq", false)
	require.Equal(t, Available, availability, "kept in its directory, for the next process")
	require.Equal(t, kept, listing)

	when.pass(25 * time.Hour)
	_, availability = another.Listing(t.Context(), "jq", false)
	require.Equal(t, Unavailable, availability, "a day-old listing isn't kept")
	_, availability = c.Listing(t.Context(), "jq", true)
	require.Equal(t, Available, availability)
	require.Equal(t, 2, s.count("/jq/"), "a day-old listing is asked again")
}

// A port with no directory is absent, kept so a day as an empty listing;
// a site that doesn't answer, or answers with neither, leaves the listing
// unavailable and keeps nothing.
func TestAListingIsAbsentOrUnavailable(t *testing.T) {
	failing := true
	var mu sync.Mutex
	s := newSite(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/flaky/" && failing {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	})
	when := &clock{at: time.Now()}
	listings := t.TempDir()
	c := &Catalog{Site: s.URL, Listings: listings, HTTP: s.Client(), Now: when.now}

	listing, availability := c.Listing(t.Context(), "gone", true)
	require.Equal(t, Absent, availability)
	require.Empty(t, listing)
	info, err := os.Stat(filepath.Join(listings, "gone.html"))
	require.NoError(t, err)
	require.Zero(t, info.Size(), "an absent port is kept as an empty listing")
	_, availability = c.Listing(t.Context(), "gone", true)
	require.Equal(t, Absent, availability)
	require.Equal(t, 1, s.count("/gone/"), "absent for a day")
	when.pass(25 * time.Hour)
	_, availability = c.Listing(t.Context(), "gone", true)
	require.Equal(t, Absent, availability)
	require.Equal(t, 2, s.count("/gone/"), "asked again a day on")

	_, availability = c.Listing(t.Context(), "flaky", true)
	require.Equal(t, Unavailable, availability, "an error status says nothing")
	require.NoFileExists(t, filepath.Join(listings, "flaky.html"))
	mu.Lock()
	failing = false
	mu.Unlock()
	_, availability = c.Listing(t.Context(), "flaky", true)
	require.Equal(t, Absent, availability, "nothing was kept, so it's asked again")

	s.Close()
	_, availability = c.Listing(t.Context(), "jq", true)
	require.Equal(t, Unavailable, availability, "a site that doesn't answer")
	_, availability = (*Catalog)(nil).Listing(t.Context(), "jq", true)
	require.Equal(t, Unavailable, availability, "a nil catalog has nothing")
}

// An archive's reading is had as it downloads from its port's directory
// and kept by its name, the archive not; a read that may not fetch has
// what's kept and nothing else.
func TestAnArchivesReadingIsAvailableAndKept(t *testing.T) {
	bzip, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("no bzip2 to pack an archive with")
	}
	var packed bytes.Buffer
	w := tar.NewWriter(&packed)
	for _, member := range []struct{ name, body string }{{"+CONTENTS", "@name jq\n"}, {"./opt/local/bin/jq", "#!/bin/sh\n"}} {
		require.NoError(t, w.WriteHeader(&tar.Header{Name: member.name, Mode: 0o755, Size: int64(len(member.body)), Typeflag: tar.TypeReg}))
		_, err := w.Write([]byte(member.body))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	command := exec.CommandContext(t.Context(), bzip, "-c")
	command.Stdin = &packed
	archive, err := command.Output()
	require.NoError(t, err)
	s := newSite(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	readings := filepath.Join(t.TempDir(), "master")
	c := &Catalog{Site: s.URL, Readings: readings, HTTP: s.Client()}
	name := ArchiveName("jq", "1.8.1", "0", nil, 25, "arm64")

	_, availability := c.Reading(t.Context(), "jq", name, false)
	require.Equal(t, Unavailable, availability, "nothing kept, and not asked")
	reading, availability := c.Reading(t.Context(), "jq", name, true)
	require.Equal(t, Available, availability)
	require.Equal(t, []string{"/opt/local/bin/jq"}, reading.Installed)
	require.Equal(t, 1, s.count("/jq/"+name), "served from the port's directory")
	kept, err := filepath.Glob(filepath.Join(readings, "*"))
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(readings, name+".json")}, kept, "the reading, and no archive")

	again, availability := (&Catalog{Readings: readings}).Reading(t.Context(), "jq", name, false)
	require.Equal(t, Available, availability, "kept for another catalog")
	require.Equal(t, reading, again)
	require.Equal(t, 1, s.count("/jq/"+name))
}

// An archive the site doesn't serve is absent, marked so, and asked again
// once the mark is a day old; a site that doesn't answer leaves it
// unavailable and marks nothing.
func TestAnArchivesReadingIsAbsentOrUnavailable(t *testing.T) {
	s := newSite(t, http.NotFound)
	when := &clock{at: time.Now()}
	readings := t.TempDir()
	c := &Catalog{Site: s.URL, Readings: readings, HTTP: s.Client(), Now: when.now}
	name := ArchiveName("jq", "1.8.1", "0", nil, 24, "arm64")
	path := "/jq/" + name

	_, availability := c.Reading(t.Context(), "jq", name, true)
	require.Equal(t, Absent, availability)
	require.FileExists(t, filepath.Join(readings, name+".json.none"))
	_, availability = c.Reading(t.Context(), "jq", name, false)
	require.Equal(t, Absent, availability, "the mark is kept, and read without fetching")
	_, availability = c.Reading(t.Context(), "jq", name, true)
	require.Equal(t, Absent, availability)
	require.Equal(t, 1, s.count(path), "not asked again within the day")
	when.pass(25 * time.Hour)
	_, availability = c.Reading(t.Context(), "jq", name, false)
	require.Equal(t, Unavailable, availability, "a day-old mark says nothing")
	_, availability = c.Reading(t.Context(), "jq", name, true)
	require.Equal(t, Absent, availability)
	require.Equal(t, 2, s.count(path), "a day-old mark is asked again")

	s.Close()
	other := ArchiveName("jq", "1.8.1", "0", nil, 25, "arm64")
	_, availability = c.Reading(t.Context(), "jq", other, true)
	require.Equal(t, Unavailable, availability, "a site that doesn't answer")
	require.NoFileExists(t, filepath.Join(readings, other+".json.none"))
	_, availability = (&Catalog{Site: s.URL}).Reading(t.Context(), "jq", other, true)
	require.Equal(t, Unavailable, availability, "no reading is had with nowhere to keep it")
	_, availability = (*Catalog)(nil).Reading(t.Context(), "jq", other, true)
	require.Equal(t, Unavailable, availability, "a nil catalog has nothing")
}
