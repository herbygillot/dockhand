package binaryarchive

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/model"
)

// Availability is what an archive site's answer, or what's kept of it,
// says of a listing or an archive.
type Availability int

const (
	// Unavailable is an answer not had: nothing kept where only what's
	// kept is read, the site not answering, or its answer saying neither
	// what was asked for nor that it isn't there. It says nothing of the
	// site.
	Unavailable Availability = iota
	// Available is the listing or the archive's reading had.
	Available
	// Absent is a port with no directory on the site, so no archive, or an
	// archive the site doesn't serve.
	Absent
)

// How long what a site said is taken as said: its buildbots build a
// version some hours after it lands on master, so a listing kept, or an
// archive the site didn't serve, is asked again a day on (the install
// estimate, 2026-10-29). maxListing is the most of a listing read.
const (
	listingKept  = 24 * time.Hour
	notServedFor = 24 * time.Hour
	maxListing   = 4 << 20
)

// Catalog is what an archive site of MacPorts' kind, packages.macports.org,
// serves of ports' archives, as dockhand asks it: a port's directory
// listing, which says which archives the port has, and what an archive
// installs, read as it downloads (Contents). Each answer is kept, a
// listing a day and an archive's reading by its name, and one the site
// says isn't there is kept as absent for a day. A read that may not fetch
// is answered from what's kept alone. The engine decides which archive
// matters; the catalog fetches, keeps, and reads it. A nil Catalog has
// nothing and fetches nothing.
type Catalog struct {
	// Site is the archive site, packages.macports.org where empty.
	Site string
	// Listings is where a port's listing is kept, <port>.html, an absent
	// one empty; kept for the catalog's life alone where empty.
	Listings string
	// Readings is where an archive's reading is kept, <name>.json, and an
	// archive the site doesn't serve marked, <name>.json.none: only the
	// reading, where a large port's archive kept whole stayed as long as
	// anything was added beside it. No reading is had where it's empty.
	Readings string
	// HTTP is the client the site is asked with, fetch.Client where nil.
	HTTP *http.Client
	// Now is the clock what's kept is aged by, time.Now where nil.
	Now func() time.Time

	mu       sync.Mutex
	listings map[string]keptListing
}

// keptListing is a listing had in the catalog's life, and when.
type keptListing struct {
	listing []byte
	at      time.Time
}

// Reading is what an archive installs, as Contents read it, kept in
// place of the archive.
type Reading struct {
	Installed []string        `json:"installed"`
	Libraries []model.Library `json:"libraries"`
}

func (c *Catalog) site() string {
	if c.Site != "" {
		return c.Site
	}
	return Packages
}

func (c *Catalog) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Catalog) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return fetch.Client
}

// Listing is a port's directory listing on the site (Directory), for
// Listed: as kept within listingKept, in the catalog's life and else in
// Listings, or where mayFetch, asked of the site and kept. A port with no
// directory is Absent, its listing kept empty; one the site doesn't
// answer for, within the context's deadline, is Unavailable and isn't
// kept.
func (c *Catalog) Listing(ctx context.Context, port string, mayFetch bool) ([]byte, Availability) {
	if c == nil {
		return nil, Unavailable
	}
	if listing, ok := c.kept(port); ok {
		return listing, listed(listing)
	}
	if !mayFetch {
		return nil, Unavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, Directory(c.site(), port), nil)
	if err != nil {
		return nil, Unavailable
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	response, err := c.client().Do(request)
	if err != nil {
		return nil, Unavailable
	}
	defer response.Body.Close()
	var listing []byte
	switch response.StatusCode {
	case http.StatusOK:
		if listing, err = io.ReadAll(io.LimitReader(response.Body, maxListing)); err != nil {
			return nil, Unavailable
		}
	case http.StatusNotFound:
	default:
		return nil, Unavailable
	}
	now := c.now()
	c.mu.Lock()
	if c.listings == nil {
		c.listings = map[string]keptListing{}
	}
	c.listings[port] = keptListing{listing: listing, at: now}
	c.mu.Unlock()
	if c.Listings != "" {
		path := filepath.Join(c.Listings, port+".html")
		if atomicfile.Write(path, listing, 0o644) == nil {
			_ = os.Chtimes(path, now, now)
		}
	}
	return listing, listed(listing)
}

// listed is a listing had: an empty one is a port with no directory.
func listed(listing []byte) Availability {
	if len(listing) == 0 {
		return Absent
	}
	return Available
}

// kept is a port's listing as kept within listingKept: in the
// catalog's life, else in Listings.
func (c *Catalog) kept(port string) ([]byte, bool) {
	now := c.now()
	c.mu.Lock()
	kept, ok := c.listings[port]
	c.mu.Unlock()
	if ok && now.Sub(kept.at) < listingKept {
		return kept.listing, true
	}
	if c.Listings == "" {
		return nil, false
	}
	path := filepath.Join(c.Listings, port+".html")
	info, err := os.Stat(path)
	if err != nil || now.Sub(info.ModTime()) >= listingKept {
		return nil, false
	}
	listing, err := os.ReadFile(path)
	return listing, err == nil
}

// Reading is what a port's archive of a name installs: as kept in
// Readings, or where mayFetch, read as the site's archive downloads,
// bzip2 over the response's body and each library's header from its tar
// entry, and kept. An archive the site doesn't serve, for a license
// MacPorts doesn't distribute or a release its buildbots don't build, is
// Absent, marked so, and asked again once the mark is a day old; one the
// site doesn't answer for, or that can't be read, is Unavailable.
func (c *Catalog) Reading(ctx context.Context, port, name string, mayFetch bool) (Reading, Availability) {
	if c == nil || c.Readings == "" {
		return Reading{}, Unavailable
	}
	path := filepath.Join(c.Readings, name+".json")
	if data, err := os.ReadFile(path); err == nil {
		var reading Reading
		if json.Unmarshal(data, &reading) == nil && reading.Installed != nil {
			return reading, Available
		}
	}
	if info, err := os.Stat(path + ".none"); err == nil && c.now().Sub(info.ModTime()) < notServedFor {
		return Reading{}, Absent
	}
	if !mayFetch {
		return Reading{}, Unavailable
	}
	if err := os.MkdirAll(c.Readings, 0o755); err != nil {
		return Reading{}, Unavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, Directory(c.site(), port)+name, nil)
	if err != nil {
		return Reading{}, Unavailable
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	response, err := c.client().Do(request)
	if err != nil {
		return Reading{}, Unavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_ = os.WriteFile(path+".none", nil, 0o644)
		now := c.now()
		_ = os.Chtimes(path+".none", now, now)
		return Reading{}, Absent
	}
	if response.StatusCode != http.StatusOK {
		return Reading{}, Unavailable
	}
	files, libraries, err := Contents(response.Body)
	if err != nil {
		return Reading{}, Unavailable
	}
	reading := Reading{Installed: files, Libraries: libraries}
	if data, err := json.Marshal(reading); err == nil {
		if atomicfile.Write(path, data, 0o644) == nil {
			_ = os.Remove(path + ".none")
		}
	}
	return reading, Available
}
