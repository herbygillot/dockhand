package command

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
)

// riftProject stands in for GitHub: a Rust project with one crate.
type riftProject struct{}

func (riftProject) Project(_ context.Context, address string) (engine.Project, error) {
	return engine.Project{Owner: "rift-dev", Name: "rift", Description: "Fast structural diff for config files", License: "MIT", Tag: "v0.4.2",
		Files: map[string][]byte{"Cargo.toml": []byte("[package]\nname = \"rift\"\n"), "Cargo.lock": []byte(`version = 4
[[package]]
name = "anyhow"
version = "1.0.89"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6"
`)}}, nil
}

// checksummer stands in for MacPorts filling in a new port's checksums.
type checksummer struct{ repo *git.Repository }

func (checksummer) ResolveRelease(context.Context, preparation.Request) (record.Release, error) {
	return record.Release{}, nil
}

func (c checksummer) Prepare(ctx context.Context, request preparation.Request) (preparation.Result, error) {
	name := "textproc/" + request.Selection.Selector + "/Portfile"
	before, data, err := c.repo.File(ctx, string(request.Source.Tree), name)
	if err != nil || !before.Exists {
		return preparation.Result{}, err
	}
	after := strings.Replace(string(data), "rmd160  0 \\\n                    sha256  0 \\\n                    size    0", "rmd160  aaaa \\\n                    sha256  bbbb \\\n                    size    4096", 1)
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := c.repo.EditTree(ctx, string(request.Source.Tree), []git.FileEdit{edit})
	port := macports.Snapshot{Ports: map[string]macports.PortInfo{"rift": {Name: "rift", Version: "0.4.2"}}}
	return preparation.Result{Target: record.Target{Name: "rift", Portfile: name}, PreparedTree: record.ObjectID(tree), Files: []git.FileEdit{edit},
		Fidelity:  []portedit.Fidelity{{Before: port, After: port}},
		Downloads: []archives.Download{{Checksum: portfile.Checksum{Name: "rift-0.4.2.tar.gz", SHA256: "bbbb", Size: 4096}}}}, err
}

func TestCreateWritesANewPortFromItsProject(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testProjectReader = riftProject{}
	testPreparer = func(e *engine.Engine) engine.Preparer { return checksummer{repo: e.Repo} }
	t.Cleanup(func() { testProjectReader, testPreparer = nil, nil })
	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n"), 0o644))

	_, _, err := dockhand(t, "create", "https://github.com/rift-dev/rift")
	require.ErrorContains(t, err, "create writes in a branch: --new starts one")

	out, _, err := dockhand(t, "create", "https://github.com/rift-dev/rift", "--new", "--category", "textproc")
	require.NoError(t, err)
	require.Regexp(t, `^rift 0\.4\.2 · Rust \(Cargo\.toml\) · GitHub says MIT · "Fast structural diff for config files"\n`+
		`Started dockhand/rift-[a-z0-9]{4} from master [0-9a-f]+ \(fetched just now\)\n`+
		`Created textproc/rift/Portfile from the github and cargo PortGroups\n`+
		`  cargo.crates: 1 crate, from Cargo.lock\n`+
		`  checksums: 1 distfile \+ 1 crate\n`+
		`  Unconfirmed, marked in the file: license \(from GitHub's detection\), long_description\n`+
		`Next: dockhand edit rift, then dockhand check\n$`, out)

	branch := regexp.MustCompile(`dockhand/(rift-[a-z0-9]{4})`).FindStringSubmatch(out)[1]
	dir := filepath.Join(w.home, "src", "macports-branches", branch)
	data, err := os.ReadFile(filepath.Join(dir, "textproc/rift/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "github.setup        rift-dev rift 0.4.2 v\n")
	require.Contains(t, string(data), "maintainers         {@ada example.org:ada} openmaintainer\n")
	require.Contains(t, string(data), "sha256  bbbb")
	require.Contains(t, string(data), "    anyhow  1.0.89  86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6\n")
	require.Equal(t, "textproc/rift/Portfile", strings.TrimSpace(gitRun(t, dir, "diff", "--cached", "--name-only")), "staged, so check includes it")

	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	require.Equal(t, "rift: new port, version 0.4.2", strings.TrimSpace(gitRun(t, dir, "log", "-1", "--format=%s")))

	// A name a port already has is refused.
	testProjectReader = jqProject{}
	_, _, err = dockhand(t, "create", "https://github.com/jqlang/jq", "--category", "sysutils")
	require.ErrorContains(t, err, "there is already a port jq, at textproc/jq")
}

type jqProject struct{}

func (jqProject) Project(context.Context, string) (engine.Project, error) {
	return engine.Project{Owner: "jqlang", Name: "jq", Tag: "jq-1.8.1", Files: map[string][]byte{"configure.ac": nil}}, nil
}
