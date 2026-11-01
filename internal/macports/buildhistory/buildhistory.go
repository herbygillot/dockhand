// Package buildhistory reads what MacPorts' builders made of a port, as
// ports.macports.org keeps their build history: whether a port built on
// each macOS release before, which says whether a failure there is the
// branch's own or one the builders had too, to choose between a patch and
// known_fail.
package buildhistory

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// Ports is the site MacPorts' web application serves, whose API keeps the
// builders' history.
const Ports = "https://ports.macports.org"

// How long a port's history is taken as said: the builders build a commit
// some hours after it lands, so a history kept is asked again a day on, as
// packages.macports.org's listings are. maxHistory is the most of an
// answer read; askWait how long one is waited for.
const (
	historyKept = 24 * time.Hour
	maxHistory  = 1 << 20
	askWait     = 5 * time.Second
)

// Statuses the site gives a build, as its API's filter names them; any
// other, a retry or a builder lost, says nothing of the port.
const (
	statusBuilt        = "build successful"
	statusFailed       = "failed install-port"
	statusDependencies = "failed install-dependencies"
)

// Build is one of a port's builds on a builder.
type Build struct {
	// Builder is the builder's name, 15_arm64: the macOS release's product
	// version and the architecture it builds for.
	Builder string `json:"builder"`
	// Status is the site's word for how it ended, "build successful",
	// "failed install-port", or "failed install-dependencies".
	Status  string    `json:"status"`
	Started time.Time `json:"started"`
}

// Built is a build that succeeded.
func (b Build) Built() bool { return b.Status == statusBuilt }

// Failed is a build that failed, the port's own install or one of its
// dependencies'.
func (b Build) Failed() bool { return strings.HasPrefix(b.Status, "failed") }

// AtDependencies is a build that failed installing the port's
// dependencies, before the port's own.
func (b Build) AtDependencies() bool { return b.Status == statusDependencies }

// decisive is a build that says something of the port, passed or failed.
func (b Build) decisive() bool { return b.Built() || b.Failed() }

// Words say how a build ended, "built", "failed", or "failed at its
// dependencies", and the site's own word for any other.
func (b Build) Words() string {
	switch b.Status {
	case statusBuilt:
		return "built"
	case statusFailed:
		return "failed"
	case statusDependencies:
		return "failed at its dependencies"
	}
	return b.Status
}

// Release words a builder's macOS release and architecture, "macOS 15
// arm64", or its name where it isn't one of a release.
func Release(builder string) string {
	product, architecture, ok := parse(builder)
	if !ok {
		return builder
	}
	return "macOS " + product + " " + architecture
}

// parse is a builder's name, 10.6_i386 or 15_arm64, as its release's
// product version and its architecture; false for a name of neither.
func parse(builder string) (product, architecture string, ok bool) {
	product, architecture, ok = strings.Cut(builder, "_")
	if !ok || product == "" || architecture == "" {
		return "", "", false
	}
	for _, part := range strings.Split(product, ".") {
		if _, err := strconv.Atoi(part); err != nil {
			return "", "", false
		}
	}
	return product, architecture, true
}

// version is a product version's numbers, for ordering releases.
func version(product string) []int {
	var numbers []int
	for _, part := range strings.Split(product, ".") {
		n, _ := strconv.Atoi(part)
		numbers = append(numbers, n)
	}
	return numbers
}

// Builder is the name of MacPorts' builder for a platform, 15_arm64 for
// darwin 24 on arm64; false where the platform isn't macOS's or its
// release isn't known.
func Builder(platform model.Platform) (string, bool) {
	if platform.OS != "darwin" && platform.OS != "" {
		return "", false
	}
	darwin, err := strconv.Atoi(platform.Version)
	if err != nil || platform.Architecture == "" {
		return "", false
	}
	product, err := macos.ProductForDarwin(darwin)
	if err != nil {
		return "", false
	}
	return product + "_" + platform.Architecture, true
}

// History is a port's builds, newest first, as the site's first page of
// them lists them: its last fifty or so, which reach every builder that
// built it since its last few commits.
type History struct {
	Port   string  `json:"port"`
	Builds []Build `json:"builds"`
}

// Latest is the newest build on a builder that passed or failed; false
// where the history has none there.
func (h History) Latest(builder string) (Build, bool) {
	for _, build := range h.Builds {
		if build.Builder == builder && build.decisive() {
			return build, true
		}
	}
	return Build{}, false
}

// Latests are each builder's newest build that passed or failed, oldest
// release first, Intel before Apple silicon within one; builders whose
// names aren't of a release are left out.
func (h History) Latests() []Build {
	var latest []Build
	for _, build := range h.Builds {
		if _, _, ok := parse(build.Builder); !ok || !build.decisive() {
			continue
		}
		if !slices.ContainsFunc(latest, func(b Build) bool { return b.Builder == build.Builder }) {
			latest = append(latest, build)
		}
	}
	slices.SortFunc(latest, func(a, b Build) int {
		aProduct, aArchitecture, _ := parse(a.Builder)
		bProduct, bArchitecture, _ := parse(b.Builder)
		return cmp.Or(slices.Compare(version(aProduct), version(bProduct)), cmp.Compare(bArchitecture, aArchitecture))
	})
	return latest
}

// Words say what each builder's latest build made of the port, "built on
// all 24 builders", or which releases failed and how, "failed on macOS
// 10.12 and 10.13 x86_64 at its dependencies; built on the other 22"; "no
// builds on ports.macports.org" for a port it has none of.
func (h History) Words() string {
	latest := h.Latests()
	if len(latest) == 0 {
		return "no builds on ports.macports.org"
	}
	var built int
	var failures []string
	// releases are the failed builders' releases by architecture and how
	// they failed, in the order first met.
	type failure struct{ architecture, words string }
	releases := map[failure][]string{}
	var order []failure
	for _, build := range latest {
		if build.Built() {
			built++
			continue
		}
		product, architecture, _ := parse(build.Builder)
		key := failure{architecture, build.Words()}
		if _, ok := releases[key]; !ok {
			order = append(order, key)
		}
		releases[key] = append(releases[key], product)
	}
	if len(order) == 0 {
		if built == 1 {
			return "built on its one builder, " + Release(latest[0].Builder)
		}
		return "built on all " + strconv.Itoa(built) + " builders"
	}
	for _, key := range order {
		words := "failed on macOS " + prose.And(releases[key]) + " " + key.architecture
		if rest, ok := strings.CutPrefix(key.words, "failed"); ok {
			words += rest
		} else {
			words += " (" + key.words + ")"
		}
		failures = append(failures, words)
	}
	if built == 0 {
		return strings.Join(failures, "; ")
	}
	return strings.Join(failures, "; ") + "; built on the other " + strconv.Itoa(built)
}

// Reader reads ports' build histories from the site, once a port: each
// answer is kept a day, in the reader's life and in Cache, and one the
// site didn't give, an error, a refusal, or no answer within askWait, says
// nothing and isn't kept. The site needs no credential and promises
// nothing beyond its API's documentation, so nothing that reads it fails
// for it. A nil Reader reads nothing.
type Reader struct {
	// Site is the web application, ports.macports.org where empty.
	Site string
	// Cache is where a port's history is kept, <port>.json; kept for the
	// reader's life alone where empty.
	Cache string
	// HTTP is the client the site is asked with, fetch.Client where nil.
	HTTP *http.Client
	// Now is the clock what's kept is aged by, time.Now where nil.
	Now func() time.Time

	mu   sync.Mutex
	kept map[string]keptHistory
}

// keptHistory is a history had in the reader's life, and when.
type keptHistory struct {
	history History
	at      time.Time
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reader) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return fetch.Client
}

func (r *Reader) site() string {
	if r.Site != "" {
		return strings.TrimSuffix(r.Site, "/")
	}
	return Ports
}

// answer is the API's page of builds (docs/API_v1/ENDPOINTS.md in
// macports/macports-webapp, "All builds"), filtered by port_name, the
// filter its browsable API offers.
type answer struct {
	Results []struct {
		Builder struct {
			Name string `json:"name"`
		} `json:"builder_name"`
		Status  string    `json:"status"`
		Started time.Time `json:"time_start"`
	} `json:"results"`
}

// History is a port's build history: as kept within a day, else asked of
// the site's builds, its first page, newest first, and kept. A port the
// site has no builds of has an empty history, which is kept too; false
// where the site gave no answer, which says nothing of the port.
func (r *Reader) History(ctx context.Context, port string) (History, bool) {
	if r == nil || port == "" || strings.ContainsAny(port, "/\\") || strings.HasPrefix(port, ".") {
		return History{}, false
	}
	if history, ok := r.keptHistory(port); ok {
		return history, true
	}
	ctx, cancel := context.WithTimeout(ctx, askWait)
	defer cancel()
	address := r.site() + "/api/v1/builds/?" + url.Values{"port_name": {port}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return History{}, false
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	request.Header.Set("Accept", "application/json")
	response, err := r.client().Do(request)
	if err != nil {
		return History{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return History{}, false
	}
	var page answer
	if err := json.NewDecoder(io.LimitReader(response.Body, maxHistory)).Decode(&page); err != nil {
		return History{}, false
	}
	history := History{Port: port, Builds: []Build{}}
	for _, result := range page.Results {
		history.Builds = append(history.Builds, Build{Builder: result.Builder.Name, Status: result.Status, Started: result.Started})
	}
	r.keep(port, history)
	return history, true
}

// keptHistory is a port's history as kept within historyKept: in the
// reader's life, else in Cache.
func (r *Reader) keptHistory(port string) (History, bool) {
	now := r.now()
	r.mu.Lock()
	kept, ok := r.kept[port]
	r.mu.Unlock()
	if ok && now.Sub(kept.at) < historyKept {
		return kept.history, true
	}
	if r.Cache == "" {
		return History{}, false
	}
	path := filepath.Join(r.Cache, port+".json")
	info, err := os.Stat(path)
	if err != nil || now.Sub(info.ModTime()) >= historyKept {
		return History{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return History{}, false
	}
	var history History
	if json.Unmarshal(data, &history) != nil || history.Port != port || history.Builds == nil {
		return History{}, false
	}
	return history, true
}

// keep keeps a port's history, in the reader's life and in Cache, aged
// from now by the reader's clock.
func (r *Reader) keep(port string, history History) {
	now := r.now()
	r.mu.Lock()
	if r.kept == nil {
		r.kept = map[string]keptHistory{}
	}
	r.kept[port] = keptHistory{history: history, at: now}
	r.mu.Unlock()
	if r.Cache == "" {
		return
	}
	data, err := json.Marshal(history)
	if err != nil {
		return
	}
	path := filepath.Join(r.Cache, port+".json")
	if atomicfile.Write(path, data, 0o644) == nil {
		_ = os.Chtimes(path, now, now)
	}
}
