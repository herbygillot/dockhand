package tart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// fakeGuest is a tart that really runs what it is handed.
//
// The stub in guest_test.go records argv and answers nothing, which is
// what a transcript needs and the wrong tool for this: the baseline is a
// sequence of commands whose ORDER and whose reading of each other's
// output is the whole recipe, and a stub that answers "" to everything
// would agree with a recipe that did the steps backwards.
//
// So this one executes. `tart exec` runs the argv for real, with two
// substitutions that are exactly the two ways a guest is not this
// machine: an absolute /tmp path is the guest's and lands under a root
// of the test's own, and sudo means "as root", which here is whoever is
// running the test. Everything else — /bin/sh, otool, xargs, sed — is
// the real tool, because the scripts under test are shell and the only
// honest way to know what a shell script does is to run it.
type fakeGuest struct {
	t      *testing.T
	root   string
	prefix prefix.Prefix
	tools  *tool.Finder
	vm     string
}

// guestState is the directory the guest's own state directory maps to.
func (g *fakeGuest) guestState() string { return filepath.Join(g.root, "tmp", "dockhand-verify") }

func (g *fakeGuest) read(name string) string {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(g.guestState(), name))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(g.t, err)
	return string(b)
}

// calls is every port(1) invocation the guest saw, in order. It is what
// the recipe is asserted against: the commands, and the sequence.
func (g *fakeGuest) calls() []string {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(g.root, "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(g.t, err)
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// plant writes a file into the guest's state directory, for the reading
// paths: what a submit recorded is what a later settle reads, and the two
// are deliberately different processes.
func (g *fakeGuest) plant(name, body string) {
	g.t.Helper()
	require.NoError(g.t, os.MkdirAll(g.guestState(), 0o755))
	require.NoError(g.t, os.WriteFile(filepath.Join(g.guestState(), name), []byte(body), 0o644))
}

// answer scripts the guest's otool for one file: `mode` is D or L, and
// @FILE@ in the body stands for the path otool was handed.
func (g *fakeGuest) answer(mode, base, body string) {
	g.t.Helper()
	dir := filepath.Join(g.root, "otool", mode)
	require.NoError(g.t, os.MkdirAll(dir, 0o755))
	require.NoError(g.t, os.WriteFile(filepath.Join(dir, base), []byte(body), 0o644))
}

func (g *fakeGuest) provider() Provider {
	return Provider{Prefix: g.prefix, Tools: g.tools}
}

// newFakeGuest builds the guest. portCases is the body of a `case "$*"`
// over port(1)'s own argv, so each test scripts exactly the environment
// it is about.
func newFakeGuest(t *testing.T, portCases string) *fakeGuest {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o755))

	// The guest's MacPorts installation: a prefix with a port(1) that
	// answers what the test scripted, a portindex that reports a clean
	// tally, and a sources.conf for the overlay line to be prepended to.
	pfx := filepath.Join(root, "opt", "local")
	require.NoError(t, os.MkdirAll(filepath.Join(pfx, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pfx, "etc", "macports"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pfx, "etc", "macports", "sources.conf"),
		[]byte("rsync://rsync.macports.org/macports/release/tarballs/ports.tar [default]\n"), 0o644))
	// A sudo the guest resolves on PATH: the staging script says `sudo -n
	// cp` without a path, the way the runner does, and in this guest root
	// is whoever is running the test.
	gbin := filepath.Join(root, "gbin")
	require.NoError(t, os.MkdirAll(gbin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gbin, "sudo"),
		[]byte("#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n"), 0o755))

	// The guest's otool. By default it is this machine's, because the
	// captures in macports/build were taken from real Mach-O files and
	// this is the same tool reading them. A test that needs an
	// environment no compiler here can produce — a library that
	// publishes an install name, a dependent bound to it — writes its
	// answers under root/otool/<D|L>/<basename> instead.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "otool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gbin, "otool"), []byte(fmt.Sprintf(`#!/bin/sh
root=%q
mode=${3#-}
shift 3
for f in "$@"; do
  answer="$root/otool/$mode/$(basename "$f")"
  if [ -f "$answer" ]; then sed "s|@FILE@|$f|g" "$answer"
  elif [ -d "$root/otool/$mode" ]; then echo "$f: is not an object file"
  else exec /usr/bin/otool -arch all "-$mode" "$@"
  fi
done
`, root)), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(pfx, "bin", "portindex"), []byte(
		"#!/bin/sh\n"+
			"echo 'Total number of ports parsed:\t1'\n"+
			"echo 'Ports successfully parsed:\t1'\n"+
			"echo 'Ports failed:\t0'\n"), 0o755))

	tart := filepath.Join(root, "tart")
	require.NoError(t, os.WriteFile(tart, []byte(fmt.Sprintf(`#!/bin/sh
root=%q
PATH="$root/gbin:$PATH"
export PATH
case "$1" in
  clone) echo "$3" >> "$root/vms"; exit 0;;
  stop|delete) exit 0;;
  run) exit 0;;
  list)
    echo "local dockhand-base-test 50 30 stopped"
    if [ -f "$root/vms" ]; then while IFS= read -r v; do echo "local $v 50 30 running"; done < "$root/vms"; fi
    exit 0;;
  exec) shift;;
  *) exit 0;;
esac
[ "$1" = -i ] && shift
shift
if [ "$1" = /usr/bin/sudo ]; then shift; shift; fi
n=$#
i=0
while [ "$i" -lt "$n" ]; do
  a=$1; shift
  set -- "$@" "$(printf '%%s' "$a" | sed -e "s|/tmp/|$root/tmp/|g" -e "s|/usr/bin/otool|$root/gbin/otool|g")"
  i=$((i+1))
done
exec "$@"
`, root)), 0o755))

	finder := tool.NewFinder(func(name string) (string, error) {
		switch name {
		case string(tool.Tart):
			return tart, nil
		case string(tool.Tar):
			return "/usr/bin/tar", nil
		}
		return "", fmt.Errorf("this guest resolves tart and tar, not %s", name)
	})

	g := &fakeGuest{t: t, root: root, prefix: prefix.Prefix(pfx), tools: finder, vm: "dockhand-worker-test"}
	g.setPort(portCases)
	require.NoError(t, os.WriteFile(filepath.Join(root, "vms"), []byte(g.vm+"\n"), 0o644))
	return g
}

// setPort scripts port(1). The stub records every argv before it answers
// anything, so a recipe can be asserted as the sequence of calls it made
// whatever those calls returned.
func (g *fakeGuest) setPort(cases string) {
	g.t.Helper()
	require.NoError(g.t, os.WriteFile(g.prefix.Port(), []byte(fmt.Sprintf(
		"#!/bin/sh\nprintf 'port %%s\\n' \"$*\" >> %q\ncase \"$*\" in\n%s\nesac\nexit 0\n",
		filepath.Join(g.root, "calls"), cases)), 0o755))
}

// program installs an executable where a port would have laid one down,
// under the guest prefix's bin, and returns the path `port contents`
// would list.
func (g *fakeGuest) program(name, body string) string {
	g.t.Helper()
	path := filepath.Join(string(g.prefix), "bin", name)
	require.NoError(g.t, os.WriteFile(path, []byte(body), 0o755))
	return path
}

// portdir writes a staging directory the way the engine materializes
// one, so stage has something real to tar.
func portdir(t *testing.T, root, side, category, name, portfile string) string {
	t.Helper()
	dir := filepath.Join(root, side, category, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte(portfile), 0o644))
	return dir
}

// The recipe, run for real and asserted as a sequence. Every command
// here is load-bearing: -b refuses to build rather than inventing a
// before that was never published, `-q installed` is asked because
// port(1)'s exit status cannot answer it, the capture happens while the
// baseline is still installed, and -f uninstall is what leaves the
// environment holding the dependencies and not the port.
func TestTheBaselineRecipeInOrder(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @2.4.1_0 (active)";;
`)
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	assert.Equal(t, []string{
		"port -N -b install libwidget",
		"port -q installed libwidget",
		"port -q contents libwidget",
		"port -q installed libwidget",
		"port -N -f uninstall libwidget",
	}, portCalls(g.calls()),
		"the baseline is installed, confirmed installed, described while it is there, then removed")

	assert.Equal(t, verify.BaselineArchive+"\n", g.read("baseline"))
	assert.Contains(t, g.read("manifest.pre"), "===> dockhand manifest: end")
	assert.Equal(t, "libwidget\n", g.read("manifest.ports"))
}

// portCalls drops the calls the staging makes, so a recipe assertion
// reads as the recipe. portindex is a separate binary and lint and
// install belong to the runner, which nothing here starts.
func portCalls(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "port ") {
			out = append(out, c)
		}
	}
	return out
}

// The order is the recipe. Both overlays are the same directory, so a
// baseline taken after the staging would install the change and measure
// it against itself — and the comparison would always say nothing moved.
func TestTheBaselineIsMeasuredBeforeTheChangeIsStaged(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @2.4.1_0 (active)";;
`)
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	// What the guest was holding when the baseline was installed, and
	// what it holds now, are two different versions of the same portdir.
	staged, err := os.ReadFile(filepath.Join(g.root, "tmp", "dockhand-overlay", "devel", "libwidget", "Portfile"))
	require.NoError(t, err)
	assert.Equal(t, "version 3.0\n", string(staged),
		"the overlay the build will use is the branch's, and it replaced the merge base's")
}

// A binary-only install that finds no archive is the honest no-baseline
// case, and it is DETECTED: the environment's own refusal is quoted, so
// the finding says which unavailability this was rather than inferring
// one from an empty manifest.
func TestABaselineWithNoArchiveDeclinesInTheEnvironmentsWords(t *testing.T) {
	g := newFakeGuest(t, `
  "-N -b install libwidget")
    echo "Error: Failed to unarchive libwidget: Archive for libwidget 2.4.1_0 not found, required when binary-only is set!"
    exit 1;;
`)
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req),
		"a check that could not be made is not a submit that failed")

	assert.Equal(t,
		"none\nError: Failed to unarchive libwidget: Archive for libwidget 2.4.1_0 not found, required when binary-only is set!\n",
		g.read("baseline"))
	assert.Empty(t, g.read("manifest.pre"))
	assert.NotContains(t, portCalls(g.calls()), "port -N -f uninstall libwidget",
		"nothing was installed, so nothing is removed")
}

// port(1) exiting zero is not the claim that the port is installed:
// `port -q installed` on a port that is not exits zero and prints
// nothing. Believing the exit code records a baseline that was never
// taken, and an empty baseline compares as every library removed.
func TestAnInstallThatSucceededAndInstalledNothingIsRefused(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") exit 0;;
`)
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	assert.Equal(t, "none\nthe binary-only install reported success and the port is not installed\n",
		g.read("baseline"))
}

// A baseline that will not come off is the one failure here that must
// stop the submit. The old version left active makes the branch's
// install a no-op or an upgrade, and the run would pass without ever
// having built the change.
func TestABaselineThatWillNotUninstallRefusesTheSubmit(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @2.4.1_0 (active)";;
  "-N -f uninstall libwidget") echo "Error: Please uninstall the ports that depend on libwidget first."; exit 1;;
`)
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
		Manifest: true,
	}

	err := g.provider().prepare(t.Context(), g.vm, req)

	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.Contains(t, err.Error(), "libwidget@2.4.1_0 would not uninstall")
	assert.Contains(t, err.Error(), "verified it instead of the change")
}

// No merge-base portdir is a real state — a port added on the branch did
// not exist to be measured — and it declines by name rather than
// producing an empty comparison.
func TestNoMergeBasePortdirDeclinesByName(t *testing.T) {
	g := newFakeGuest(t, "")
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	assert.Equal(t, "none\nno merge-base portdir was staged, so there is nothing to install as the before\n",
		g.read("baseline"))
	assert.Empty(t, portCalls(g.calls()), "nothing was installed to be measured")
}

// A caller holding a banked measurement says so at submit, because what
// it buys is the download this would otherwise spend. The provider
// records the source and takes nothing.
func TestABankedBaselineSpendsNoDownload(t *testing.T) {
	g := newFakeGuest(t, "")
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
		Banked:   true,
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	assert.Equal(t, verify.BaselineBanked+"\n", g.read("baseline"))
	assert.Empty(t, portCalls(g.calls()))
}

// A request that asked for no manifest leaves the guest exactly as it
// always was: no roster, no baseline record, no capture. That is the
// compatibility claim the frozen runner rests on, held one directory
// lower than the transcript holds it.
func TestNoManifestLeavesNoTraceInTheGuest(t *testing.T) {
	g := newFakeGuest(t, "")
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"libwidget"},
		Portdirs: []string{portdir(t, staging, "branch", "devel", "libwidget", "version 3.0\n")},
		Baseline: []string{portdir(t, staging, "base", "devel", "libwidget", "version 2.4.1\n")},
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	assert.Empty(t, g.read("manifest.ports"))
	assert.Empty(t, g.read("baseline"))
	assert.Empty(t, portCalls(g.calls()))
}

// The variant frame is the branch build's, because an archive is named
// by version, revision AND variants: a baseline taken under a different
// frame compares two different builds and reports a phantom ABI change.
func TestTheBaselineCarriesTheRequestsVariantFrame(t *testing.T) {
	v, err := info.Variants("+quartz", "-x11")
	require.NoError(t, err)
	g := newFakeGuest(t, `
  "-q installed cairo") echo "  cairo @1.18.4_2+quartz (active)";;
`)
	staging := t.TempDir()
	req := verify.Request{
		Ports:    []string{"cairo"},
		Portdirs: []string{portdir(t, staging, "branch", "graphics", "cairo", "version 1.19\n")},
		Baseline: []string{portdir(t, staging, "base", "graphics", "cairo", "version 1.18.4\n")},
		Variants: v,
		Manifest: true,
	}

	require.NoError(t, g.provider().prepare(t.Context(), g.vm, req))

	assert.Equal(t, "port -N -b install cairo +quartz -x11", portCalls(g.calls())[0])
}
