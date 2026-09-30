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
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
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

func (checksummer) ResolveRelease(context.Context, preparation.Request) (model.Release, error) {
	return model.Release{}, nil
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
	return preparation.Result{Target: model.Target{Name: "rift", Portfile: name}, PreparedTree: model.ObjectID(tree), Files: []git.FileEdit{edit},
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
		`  Unconfirmed, marked in the file: license \(from GitHub's detection\), long_description, destroot\n`+
		`Next: dockhand edit rift, then dockhand check\n$`, out)

	branch := regexp.MustCompile(`dockhand/(rift-[a-z0-9]{4})`).FindStringSubmatch(out)[1]
	dir := filepath.Join(w.home, "Source", "macports-branches", branch)
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

	// A category the tree keeps for itself is refused before anything is
	// written (the helper-ownership review's table).
	_, _, err = dockhand(t, "create", "https://github.com/rift-dev/rift", "--new", "--name", "rift2", "--category", "_resources")
	require.ErrorContains(t, err, `"_resources" is not a category`)

	// A name a port already has is refused.
	testProjectReader = jqProject{}
	_, _, err = dockhand(t, "create", "https://github.com/jqlang/jq", "--category", "sysutils")
	require.ErrorContains(t, err, "there is already a port jq, at textproc/jq")
}

type jqProject struct{}

func (jqProject) Project(context.Context, string) (engine.Project, error) {
	return engine.Project{Owner: "jqlang", Name: "jq", Tag: "jq-1.8.1", Files: map[string][]byte{"configure.ac": nil}}, nil
}

// txtProject stands in for GitHub as the txt run found it: GitHub detects
// no license it can name, and its description is a pitch, while
// Cargo.toml declares both in the project's own words.
type txtProject struct{}

func (txtProject) Project(context.Context, string) (engine.Project, error) {
	return engine.Project{Owner: "ErikHellman", Name: "txt", Description: "A terminal text editor for engineers in the age of AI coding", License: "NOASSERTION", Tag: "v0.8.1",
		Homepage: "http://txt.hellman.io/",
		Files:    map[string][]byte{"Cargo.toml": []byte("[package]\nname = \"txt\"\nlicense = \"MIT OR Apache-2.0\"\ndescription = \"A fast, intuitive terminal text editor\"\n")}}, nil
}

// A new port's license and line come from the project's manifest before
// its forge, in MacPorts' words, and a guessed category is said with what
// it chose, since it picks the directory (the txt run's findings 3, 4,
// and 5).
func TestCreateTakesTheManifestsLicenseAndLine(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testProjectReader = txtProject{}
	testPreparer = func(e *engine.Engine) engine.Preparer { return checksummer{repo: e.Repo} }
	t.Cleanup(func() { testProjectReader, testPreparer = nil, nil })

	testHTTPS = httpsAnswers{"https://txt.hellman.io/": true}
	out, _, err := dockhand(t, "create", "https://github.com/ErikHellman/txt", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "  homepage: over HTTPS, as MacPorts prefers; GitHub gives http://txt.hellman.io/\n", "the finding 6 of the txt run")
	require.Contains(t, out, "txt 0.8.1 · Rust (Cargo.toml) · Cargo.toml says MIT OR Apache-2.0 · \"A fast, intuitive terminal text editor\"\n")
	require.Contains(t, out, "Unconfirmed, marked in the file: category devel (guessed from the build system; --category chooses), license (from Cargo.toml), long_description, maintainers")
	branch := regexp.MustCompile(`dockhand/(txt-[a-z0-9]{4})`).FindStringSubmatch(out)[1]
	data, err := os.ReadFile(filepath.Join(w.home, "Source", "macports-branches", branch, "devel/txt/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "# dockhand: unconfirmed, from Cargo.toml's license field\nlicense             {MIT Apache-2}\n")
	require.Contains(t, string(data), "description         A fast, intuitive terminal text editor\n")
	require.Contains(t, string(data), "homepage            https://txt.hellman.io/\n")

	// Where https doesn't answer, the homepage is written as GitHub gives
	// it, and said.
	testHTTPS = httpsAnswers{}
	out, _, err = dockhand(t, "create", "https://github.com/ErikHellman/txt", "--new", "--name", "txt-plain")
	require.NoError(t, err)
	require.NotContains(t, out, "homepage: over HTTPS")
	require.Contains(t, out, "MacPorts prefers HTTPS; over plain HTTP:\n  homepage http://txt.hellman.io/: https doesn't answer there\n")
}

// A port's plain-HTTP URLs are said with whether their https form answers,
// and left as they are.
func TestPlainHTTPURLsAreSaid(t *testing.T) {
	var out strings.Builder
	writePlainHTTP(&out, nil)
	require.Empty(t, out.String())
	writePlainHTTP(&out, []engine.PlainURL{
		{PlainURL: macports.PlainURL{Option: "homepage", URL: "http://jqlang.example/"}, HTTPS: "https://jqlang.example/", Answers: true},
		{PlainURL: macports.PlainURL{Option: "master_sites", URL: "http://dl.example/jq/"}, HTTPS: "https://dl.example/jq/"},
	})
	require.Equal(t, "MacPorts prefers HTTPS; over plain HTTP:\n  homepage http://jqlang.example/: https://jqlang.example/ answers\n  master_sites http://dl.example/jq/: https doesn't answer there\n", out.String())
}
