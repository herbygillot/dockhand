package create

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
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

func (checksummer) ResolveRelease(context.Context, editprep.Request) (model.Release, error) {
	return model.Release{}, nil
}

func (c checksummer) Prepare(ctx context.Context, request editprep.Request) (editprep.Result, error) {
	name := "textproc/" + request.Selection.Selector + "/Portfile"
	before, data, err := c.repo.File(ctx, string(request.Source.Tree), name)
	if err != nil || !before.Exists {
		return editprep.Result{}, err
	}
	after := strings.Replace(string(data), "rmd160  0 \\\n                    sha256  0 \\\n                    size    0", "rmd160  aaaa \\\n                    sha256  bbbb \\\n                    size    4096", 1)
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := c.repo.EditTree(ctx, string(request.Source.Tree), []git.FileEdit{edit})
	port := macports.Snapshot{Ports: map[string]macports.PortInfo{"rift": {Name: "rift", Version: "0.4.2"}}}
	return editprep.Result{Target: model.Target{Name: "rift", Portfile: name}, PreparedTree: model.ObjectID(tree), Files: []git.FileEdit{edit},
		Fidelity:  []portedit.Fidelity{{Before: port, After: port}},
		Downloads: []distfetch.Download{{Checksum: portfile.Checksum{Name: "rift-0.4.2.tar.gz", SHA256: "bbbb", Size: 4096}}}}, err
}

func TestCreateWritesANewPortFromItsProject(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).ProjectReader = riftProject{}
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return checksummer{repo: e.Repo} }
	t.Cleanup(func() { cmdtest.Stands(t).Preparer = nil })
	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n"), 0o644))

	_, _, err := cmdtest.Dockhand(t, "create", "https://github.com/rift-dev/rift")
	require.ErrorContains(t, err, "create needs a branch. Use --new to start one")

	out, _, err := cmdtest.Dockhand(t, "create", "https://github.com/rift-dev/rift", "--new", "--category", "textproc")
	require.NoError(t, err)
	require.Regexp(t, `^rift 0\.4\.2 · Rust \(Cargo\.toml\) · GitHub says MIT · "Fast structural diff for config files"\n`+
		`Started dockhand/rift-new from master [0-9a-f]+ \(fetched just now\)\n`+
		`Created textproc/rift/Portfile from the github and cargo PortGroups\n`+
		`  cargo.crates: 1 crate, from Cargo.lock\n`+
		`  checksums: 1 distfile \+ 1 crate\n`+
		`  Review these fields and remove their unconfirmed markers: license \(from GitHub's detection\),\n`+
		`    long_description, destroot\n`+
		`Next: cd "\$\(dockhand path rift-new\)", then dockhand edit rift, then dockhand check\n$`, out)

	branch := regexp.MustCompile(`dockhand/(rift-new)`).FindStringSubmatch(out)[1]
	dir := filepath.Join(w.Home, "Source", "macports-branches", branch)
	data, err := os.ReadFile(filepath.Join(dir, "textproc/rift/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "github.setup        rift-dev rift 0.4.2 v\n")
	require.Contains(t, string(data), "maintainers         {@ada example.org:ada} openmaintainer\n")
	require.Contains(t, string(data), "sha256  bbbb")
	require.Contains(t, string(data), "    anyhow  1.0.89  86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6\n")
	require.Equal(t, "textproc/rift/Portfile", strings.TrimSpace(testsupport.Git(t, dir, "diff", "--cached", "--name-only")), "staged, so check includes it")

	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	require.Equal(t, "rift: new port", strings.TrimSpace(testsupport.Git(t, dir, "log", "-1", "--format=%s")))

	// A category the tree keeps for itself is refused before anything is
	// written (the helper-ownership review's table).
	_, _, err = cmdtest.Dockhand(t, "create", "https://github.com/rift-dev/rift", "--new", "--name", "rift2", "--category", "_resources")
	require.ErrorContains(t, err, `"_resources" is not a category`)

	// A name a port already has is refused, with --name named; with
	// --new, before a branch is started for it, where one was started and
	// left behind (the sand-runner port).
	cmdtest.Stands(t).ProjectReader = jqProject{}
	_, _, err = cmdtest.Dockhand(t, "create", "https://github.com/jqlang/jq", "--category", "sysutils")
	require.ErrorContains(t, err, "there is already a port jq, at textproc/jq; --name names this one otherwise, or dockhand update jq updates that one")
	_, _, err = cmdtest.Dockhand(t, "create", "https://github.com/jqlang/jq", "--new", "--category", "sysutils")
	require.ErrorContains(t, err, "there is already a port jq, at textproc/jq; --name names this one otherwise")
	require.Empty(t, strings.TrimSpace(testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/jq-*")), "no branch was started for it")
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
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).ProjectReader = txtProject{}
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return checksummer{repo: e.Repo} }
	t.Cleanup(func() { cmdtest.Stands(t).Preparer = nil })

	cmdtest.Stands(t).HTTPS = testsupport.HTTPSAnswers{"https://txt.hellman.io/": true}
	out, _, err := cmdtest.Dockhand(t, "create", "https://github.com/ErikHellman/txt", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "  homepage: using HTTPS, as MacPorts recommends (GitHub supplied http://txt.hellman.io/)\n", "the finding 6 of the txt run")
	require.Contains(t, out, "txt 0.8.1 · Rust (Cargo.toml) · Cargo.toml says MIT OR Apache-2.0 · \"A fast, intuitive terminal text\n  editor\"\n")
	require.Contains(t, out, "Review these fields and remove their unconfirmed markers: category devel (guessed from the build\n    system; create --category moves it), license (from Cargo.toml), long_description, maintainers")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("  maintainers: nomaintainer (no maintainer configured). set maintainer to you as a Portfile's maintainers line writes you, such as \"@you\" for your GitHub login, or \"{example.org:you @you}\" with your email too, in "+filepath.Join(w.Home, ".dockhand", "config.toml")+"\n"),
		"no port at the base names @ada")
	branch := regexp.MustCompile(`dockhand/(txt-new)`).FindStringSubmatch(out)[1]
	data, err := os.ReadFile(filepath.Join(w.Home, "Source", "macports-branches", branch, "devel/txt/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "# dockhand: unconfirmed, from Cargo.toml's license field\nlicense             {MIT Apache-2}\n")
	require.Contains(t, string(data), "description         A fast, intuitive terminal text editor\n")
	require.Contains(t, string(data), "homepage            https://txt.hellman.io/\n")

	// Edited by hand, as create asks, and then created again with another
	// category, it moves there, its edits kept; with none, it's said what
	// moves it (the txt run's finding 1).
	dir := filepath.Join(w.Home, "Source", "macports-branches", branch)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devel/txt/Portfile"), append(data, []byte("\n# settled by hand\n")...), 0o644))
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	out, _, err = cmdtest.Dockhand(t, "create", "https://github.com/ErikHellman/txt", "--category", "editors")
	require.NoError(t, err)
	require.Contains(t, out, "Moved txt from devel/txt to editors/txt, preserving all files and your edits.\n")
	moved, err := os.ReadFile(filepath.Join(dir, "editors/txt/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(moved), "# settled by hand\n")
	require.NoDirExists(t, filepath.Join(dir, "devel/txt"))
	require.Equal(t, "editors/txt/Portfile", strings.TrimSpace(testsupport.Git(t, dir, "diff", "--cached", "--name-only")), "staged where it now is")
	_, _, err = cmdtest.Dockhand(t, "create", "https://github.com/ErikHellman/txt")
	require.ErrorContains(t, err, "txt is this branch's new port, at editors/txt, not yet committed: dockhand edit txt edits it, and create --category <another> moves it")

	// Its hand edits leave tidy's plan standing, with create's subject and
	// no attribution, since dockhand didn't make them all (the txt run's
	// finding 3). Committed, it's no longer create's to move.
	out, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	require.Contains(t, out, "new port from dockhand create, including your later edits")
	require.Equal(t, "txt: new port", strings.TrimSpace(testsupport.Git(t, dir, "log", "-1", "--format=%s")))
	require.NotContains(t, testsupport.Git(t, dir, "log", "-1", "--format=%B"), "Generated-By")
	_, _, err = cmdtest.Dockhand(t, "create", "https://github.com/ErikHellman/txt", "--category", "games")
	require.ErrorContains(t, err, "txt is this branch's new port, at editors/txt, and committed: dockhand edit txt edits it")
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)

	// Where https doesn't answer, the homepage is written as GitHub gives
	// it, and said.
	cmdtest.Stands(t).HTTPS = testsupport.HTTPSAnswers{}
	out, _, err = cmdtest.Dockhand(t, "create", "https://github.com/ErikHellman/txt", "--new", "--name", "txt-plain")
	require.NoError(t, err)
	require.NotContains(t, out, "homepage: over HTTPS")
	require.Contains(t, out, "MacPorts prefers HTTPS; over plain HTTP:\n  homepage http://txt.hellman.io/: https doesn't answer there\n")
}

// maintainedPort is onePort, with an index whose ports write @ada two ways.
type maintainedPort struct{ cmdtest.OnePort }

func (maintainedPort) Spellings(_ context.Context, _ model.Source, spelling string) ([]portindex.MaintainerSpelling, error) {
	if spelling != "@ada" {
		return nil, nil
	}
	return []portindex.MaintainerSpelling{{Maintainer: macports.Maintainer{"@ada", "example.org:ada"}, Portfiles: 12}, {Maintainer: macports.Maintainer{"@ada"}, Portfiles: 2}}, nil
}

// Where the config names no maintainer, create writes nomaintainer, and
// says the line to set: the person's own, as the ports naming their
// GitHub login write it, with the others named to choose from. It writes
// no config (the flyctl run, macports/macports-ports#35069).
func TestCreateSuggestsTheMaintainerLineThePortsWrite(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).ProjectReader = txtProject{}
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return checksummer{repo: e.Repo} }
	cmdtest.UsePorts(cmdtest.Stands(t), maintainedPort{})
	config := filepath.Join(w.Home, ".dockhand", "config.toml")
	require.NoFileExists(t, config)

	out, _, err := cmdtest.Dockhand(t, "create", "https://github.com/ErikHellman/txt", "--new", "--category", "editors")
	require.NoError(t, err)
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("  maintainers: nomaintainer (no maintainer configured). set maintainer = \"{@ada example.org:ada}\" in "+filepath.Join(w.Home, ".dockhand", "config.toml")+", as 12 of the ports that name @ada write it, or as others do: \"@ada\" (2)\n"))
	require.NoFileExists(t, config, "it only suggests")
}

// A port's plain-HTTP URLs are said with whether their https form answers,
// and left as they are.
func TestPlainHTTPURLsAreSaid(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	view.WritePlainHTTP(&out, nil)
	require.Empty(t, out.String())
	view.WritePlainHTTP(&out, []engine.PlainURL{
		{PlainURL: macports.PlainURL{Option: "homepage", URL: "http://jqlang.example/"}, HTTPS: "https://jqlang.example/", Answers: true},
		{PlainURL: macports.PlainURL{Option: "master_sites", URL: "http://dl.example/jq/"}, HTTPS: "https://dl.example/jq/"},
	})
	require.Equal(t, "MacPorts prefers HTTPS; over plain HTTP:\n  homepage http://jqlang.example/: https://jqlang.example/ answers\n  master_sites http://dl.example/jq/: https doesn't answer there\n", out.String())
}

// toolProject stands in for GitHub: a Python project.
type toolProject struct{}

func (toolProject) Project(_ context.Context, address string) (engine.Project, error) {
	return engine.Project{Owner: "o", Name: "tool", Description: "A tool", License: "MIT", Tag: "v2.0",
		Files: map[string][]byte{"pyproject.toml": []byte("[project]\nname = \"tool\"\n")}}, nil
}

// pythonPorts read every port as the python PortGroup defaults it.
type pythonPorts struct{ cmdtest.OnePort }

func (pythonPorts) Ports(_ context.Context, _ model.Source, directory string, _ model.Environment, _ map[string]bool) ([]macports.PortInfo, error) {
	return []macports.PortInfo{{Name: filepath.Base(directory), Options: map[string]string{"python.default_version": "314"}}}, nil
}

// A Python project's python.versions starts from the python PortGroup's
// default, as MacPorts evaluates the new Portfile: create wrote 313 after
// the PortGroup moved to 314 (the library survey, 2026-10-02).
func TestCreateTakesThePythonPortGroupsDefault(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).ProjectReader = toolProject{}
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return checksummer{repo: e.Repo} }
	cmdtest.UsePorts(cmdtest.Stands(t), pythonPorts{})
	out, _, err := cmdtest.Dockhand(t, "create", "https://github.com/o/tool", "--new", "--category", "textproc")
	require.NoError(t, err)
	branch := regexp.MustCompile(`dockhand/(py-tool-new)`).FindStringSubmatch(out)[1]
	data, err := os.ReadFile(filepath.Join(w.Home, "Source", "macports-branches", branch, "textproc/py-tool/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "python.versions     314\n")
}

// refusingChecksums stands in for MacPorts refusing a new port's
// checksums, as for a Go module before go 1.17 whose go.sum has modules
// its go.mod doesn't require.
type refusingChecksums struct{ checksummer }

func (refusingChecksums) Prepare(context.Context, editprep.Request) (editprep.Result, error) {
	return editprep.Result{}, errors.New("dependency: go.mod says 1.15, before 1.17; go.sum has the source of github.com/kr/text too, which go.vendors needs by hand: add github.com/kr/text lock v0.1.0, as go.sum pins it, with the checksums port -v checksum reports for its archive")
}

// A draft whose checksums couldn't be made isn't done: create says what
// to add, and exits 3, where it exited 0 (v0.4.0's known issues).
func TestCreateExitsForALookWhereADraftHasNoChecksums(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).ProjectReader = riftProject{}
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return refusingChecksums{checksummer{repo: e.Repo}} }
	t.Cleanup(func() { cmdtest.Stands(t).Preparer = nil })
	out, _, err := cmdtest.Dockhand(t, "create", "https://github.com/rift-dev/rift", "--new", "--category", "textproc")
	require.Equal(t, term.ExitAttention, term.ExitCode(err), out)
	require.Contains(t, cmdtest.Flat(out), "checksums: not generated: dependency: go.mod says 1.15, before 1.17; go.sum has the source of github.com/kr/text too, which go.vendors needs by hand: add github.com/kr/text lock v0.1.0")
	require.Contains(t, out, "Next: resolve the checksum problem above, then run dockhand checksums rift\n")
}

// create --plan prints the Portfile create would write, and writes
// nothing, starting no branch (Then's item 14).
func TestCreatePlanPrintsThePortfileAndWritesNothing(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).ProjectReader = riftProject{}
	out, _, err := cmdtest.Dockhand(t, "create", "https://github.com/rift-dev/rift", "--plan", "--category", "textproc")
	require.NoError(t, err)
	require.Contains(t, out, "Would write textproc/rift/Portfile:\n")
	require.Contains(t, out, "github.setup        rift-dev rift 0.4.2 v\n")
	require.Contains(t, out, "    anyhow  1.0.89  86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6\n")
	require.Contains(t, out, "Nothing was written (--plan)")
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")
}
