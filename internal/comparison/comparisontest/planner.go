// Package comparisontest has the fakes the comparison's tests and the
// engine's share: a planner serving its ports' archives over HTTP, and
// the tarballs it serves.
package comparisontest

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
)

// PlannedPort is a port as a fake planner has it in one tree: as
// evaluated, and its archives by name, each file's contents by path below
// the archive's top; or a port fetched with Git, or with no source.
type PlannedPort struct {
	Info     macports.PortInfo
	Archives map[string]map[string]string
	NoSource bool
}

// Planner stands in for MacPorts' fetch plans, serving each archive
// its ports name over HTTP, and counting what's fetched.
type Planner struct {
	t       *testing.T
	ports   map[model.ObjectID]map[string]PlannedPort
	Server  *httptest.Server
	Served  map[string][]byte
	Fetches *atomic.Int64
	// Refusing is the status the server answers with instead, while it's
	// set, and Mirror the status its mirror answers with, where set.
	Refusing, Mirror *atomic.Int64
}

// NewPlanner is a Planner serving nothing yet.
func NewPlanner(t *testing.T) *Planner {
	p := &Planner{t: t, ports: map[model.ObjectID]map[string]PlannedPort{}, Served: map[string][]byte{}, Fetches: &atomic.Int64{}, Refusing: &atomic.Int64{}, Mirror: &atomic.Int64{}}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status := p.Mirror.Load(); status != 0 && strings.HasPrefix(r.URL.Path, "/mirror/") {
			w.WriteHeader(int(status))
			return
		}
		if status := p.Refusing.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		data, ok := p.Served[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		p.Fetches.Add(1)
		_, _ = w.Write(data)
	}))
	t.Cleanup(p.Server.Close)
	return p
}

// Add plans a port in a tree.
func (p *Planner) Add(tree model.ObjectID, port PlannedPort) {
	if p.ports[tree] == nil {
		p.ports[tree] = map[string]PlannedPort{}
	}
	var checksums []string
	for _, name := range slices.Sorted(maps.Keys(port.Archives)) {
		// An archive two ports name is made once: made again a second
		// later, its tarball's times differ, and the first port's
		// checksum no longer matches what's served (CI at 22336157).
		data, made := p.Served[name]
		if script, raw := port.Archives[name][""]; raw && !made {
			// A file served as it is, no archive: a script a port copies
			// into place.
			data, made = []byte(script), true
			p.Served[name] = data
		}
		if !made {
			path := WriteTarball(p.t, p.t.TempDir(), strings.TrimSuffix(name, ".tar.gz"), port.Archives[name])
			var err error
			data, err = os.ReadFile(path)
			require.NoError(p.t, err)
			p.Served[name] = data
		}
		sum := sha256.Sum256(data)
		checksums = append(checksums, fmt.Sprintf("%s sha256 %s size %d", name, hex.EncodeToString(sum[:]), len(data)))
	}
	options := map[string]string{"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0"}
	maps.Copy(options, port.Info.Options)
	options["checksums"] = strings.Join(checksums, " ")
	port.Info.Options = options
	p.ports[tree][port.Info.Name] = port
}

// Same plans a tree's ports as another tree's, the same archives served.
func (p *Planner) Same(tree, as model.ObjectID) {
	p.ports[tree] = p.ports[as]
}

func (p *Planner) ArchivePlan(_ context.Context, source model.Source, directory, name string) (macports.PortInfo, []macports.Distfile, error) {
	port, ok := p.ports[source.Tree][name]
	if !ok {
		return macports.PortInfo{}, nil, fmt.Errorf("%w: %s defines no port %s", treequery.ErrNoPort, directory, name)
	}
	if port.NoSource || port.Info.GitFetched() {
		return port.Info, nil, treequery.ErrNoArchives
	}
	var plan []macports.Distfile
	for _, name := range slices.Sorted(maps.Keys(port.Archives)) {
		plan = append(plan, macports.Distfile{Name: name, URLs: []string{p.Server.URL + "/" + name}})
	}
	return port.Info, plan, nil
}

// WriteTarball writes files under one top directory, as a release archive
// has them.
func WriteTarball(t *testing.T, directory, top string, files map[string]string) string {
	name := filepath.Join(directory, top+".tar.gz")
	out, err := os.Create(name)
	require.NoError(t, err)
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for path, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: top + "/" + path, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, out.Close())
	return name
}
