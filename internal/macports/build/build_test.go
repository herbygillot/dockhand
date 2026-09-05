package build

import (
	"io"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func TestLayoutTakesTheCategoryAbove(t *testing.T) {
	c, n, err := Layout("/Users/x/macports-ports/sysutils/jq")
	require.NoError(t, err)
	assert.Equal(t, "sysutils", c)
	assert.Equal(t, "jq", n)
}

func TestLayoutToleratesATrailingSlash(t *testing.T) {
	c, n, err := Layout("/ports/devel/git/")
	require.NoError(t, err)
	assert.Equal(t, "devel", c)
	assert.Equal(t, "git", n)
}

// A portdir with no category above it indexes nothing, and a build
// against an empty overlay silently tests the installation's own copy
// of the port.
func TestLayoutRefusesAPortdirWithoutACategory(t *testing.T) {
	_, _, err := Layout("/jq")
	require.ErrorIs(t, err, ErrNotAPortdir)
}

const indexed = "Creating port index in /private/tmp/ov\n\n" +
	"Total number of ports parsed:\t1 \nPorts successfully parsed:\t1 \n" +
	"Ports failed:\t\t\t0 \nUp-to-date ports skipped:\t0 \n"

func TestParseTallyReadsPortindex(t *testing.T) {
	got, err := ParseTally(indexed)
	require.NoError(t, err)
	assert.Equal(t, Tally{Parsed: 1, Succeeded: 1, Failed: 0}, got)
	assert.True(t, got.Complete())
}

// An unparseable Portfile is counted, not thrown: portindex exits zero
// and reports the failure in its tally, so the tally is the only thing
// that distinguishes a staged port from a skipped one.
func TestParseTallyCatchesAFailedPortfile(t *testing.T) {
	got, err := ParseTally("Total number of ports parsed:\t1 \n" +
		"Ports successfully parsed:\t0 \nPorts failed:\t\t\t1 \n")
	require.NoError(t, err)
	assert.False(t, got.Complete())
}

func TestParseTallyCatchesAnEmptyOverlay(t *testing.T) {
	got, err := ParseTally("Total number of ports parsed:\t0 \n" +
		"Ports successfully parsed:\t0 \nPorts failed:\t\t\t0 \n")
	require.NoError(t, err)
	assert.False(t, got.Complete(), "indexing nothing is not success")
}

// Output that cannot be read must not be mistaken for an index that
// found nothing, which would be mistaken for a port problem.
func TestParseTallyRefusesForeignOutput(t *testing.T) {
	_, err := ParseTally("command not found: portindex")
	require.ErrorIs(t, err, ErrNoTally)
}

func TestSourcesLineIsNosync(t *testing.T) {
	assert.Equal(t, "file:///tmp/ov [nosync]", SourcesLine("/tmp/ov"))
}

func TestInstallArgsKeepsVariantsSeparate(t *testing.T) {
	v, err := info.Variants("+doc", "-x11")
	require.NoError(t, err)
	assert.Equal(t, []string{"-d", "-N", "install", "pmd", "+doc", "-x11"},
		InstallArgs("pmd", v, false))
}

func TestInstallArgsDefaultFrame(t *testing.T) {
	assert.Equal(t, []string{"-d", "-N", "install", "jq"}, InstallArgs("jq", "", false))
}

// -s is only for a re-derivation at an unchanged version, where the
// archive that matches predates the change.
func TestInstallArgsForcesSource(t *testing.T) {
	assert.Equal(t, []string{"-d", "-N", "-s", "install", "jq"}, InstallArgs("jq", "", true))
}

func TestTestArgsRunFirstAndKeepTheBuild(t *testing.T) {
	assert.Equal(t, []string{"-d", "-N", "-k", "test", "jq"}, TestArgs("jq", ""))
}

// The seated sibling is taken out by force: under -N nothing is asked,
// and without -f the registry's dependents check would stop the
// deactivate of a port the members before it were built against. Force
// is on the deactivate and never on the install.
func TestDeactivateArgsForceTheSiblingOut(t *testing.T) {
	assert.Equal(t, []string{"-d", "-N", "-f", "deactivate", "gegl"}, DeactivateArgs("gegl"))
}

// The installer name pairs the product version with the marketing name,
// spaces removed. Every case here is a file MacPorts actually publishes.
func TestInstallerName(t *testing.T) {
	for _, c := range []struct{ release, want string }{
		{"Sequoia", "MacPorts-2.12.6-15-Sequoia.pkg"},
		{"Tahoe", "MacPorts-2.12.6-26-Tahoe.pkg"},
		{"Big Sur", "MacPorts-2.12.6-11-BigSur.pkg"},
		{"High Sierra", "MacPorts-2.12.6-10.13-HighSierra.pkg"},
		{"El Capitan", "MacPorts-2.12.6-10.11-ElCapitan.pkg"},
	} {
		r, ok := platform.ByName(c.release)
		require.True(t, ok, "release %q", c.release)
		assert.Equal(t, c.want, InstallerName("2.12.6", r))
	}
}

func TestInstallerURL(t *testing.T) {
	r, _ := platform.ByName("Sequoia")
	assert.Equal(t, "https://distfiles.macports.org/MacPorts/MacPorts-2.12.6-15-Sequoia.pkg",
		InstallerURL("2.12.6", r))
}

// The release table and the installer naming are only right if MacPorts
// agrees, and neither this package nor platform can tell on its own. So
// ask: every release named must have a published installer, and every
// published installer must have a release that names it.
//
// It reaches upstream, so it is opt-in: a run says it wants the outside
// world with DOCKHAND_TEST_REQUIRE=network. What it protects against is
// the table quietly falling behind a MacPorts release — the failure a
// constant would have had, and one a derived value should not.
func TestReleaseTableAgreesWithWhatMacPortsPublishes(t *testing.T) {
	testenv.Network(t)
	const version = "2.12.6"
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(DistfilesURL + "/")
	require.NoError(t, err, "the network was required but is not reachable")
	defer resp.Body.Close() //nolint:errcheck // read-path close
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	re := regexp.MustCompile(`MacPorts-` + regexp.QuoteMeta(version) + `-[0-9.]+-[A-Za-z]+\.pkg`)
	published := map[string]bool{}
	for _, m := range re.FindAllString(string(body), -1) {
		published[m] = true
	}
	require.NotEmpty(t, published, "found no %s installers; the listing format may have changed", version)

	for _, r := range platform.Releases {
		name := InstallerName(version, r)
		assert.True(t, published[name], "%s names %s, which MacPorts does not publish", r.Name, name)
		delete(published, name)
	}
	for name := range published {
		assert.Fail(t, "unnamed release",
			"MacPorts publishes %s but platform.Releases names no release for it", name)
	}
}

// A stock macOS has an empty /usr/local owned by root — measured on a
// clean vanilla image, 0 bytes containing one empty directory. Naming
// the bare prefix as foreign would declare every clean machine dirty,
// so Homebrew's presence under it is named by what only Homebrew makes.
func TestForeignPrefixesNameManagersNotPlaces(t *testing.T) {
	assert.NotContains(t, ForeignPrefixes, "/usr/local",
		"a stock macOS has one; its existence is not contamination")
	assert.Contains(t, ForeignPrefixes, "/usr/local/Homebrew")
	assert.Contains(t, ForeignPrefixes, "/opt/homebrew")
	// None of them may be a directory a stock macOS already has, or the
	// check condemns every clean image.
	stock := []string{"/usr", "/usr/local", "/opt", "/bin", "/sbin", "/etc", "/var", "/tmp"}
	for _, p := range ForeignPrefixes {
		assert.NotContains(t, stock, p, "%s exists on a clean macOS", p)
	}
}

// Empty output is the pass. Every line the script can print is a way an
// environment looks ready without being it.
func TestCleanScriptChecksEveryWayToLookReady(t *testing.T) {
	s := CleanScript("/opt/local/bin/port")
	assert.Contains(t, s, "installed", "a leak from a previous verification shows as installed ports")
	assert.Contains(t, s, "/opt/homebrew", "a build must not find a package manager the port never declared")
	assert.Contains(t, s, "xcode-select", "an image with no compiler fails every port, one at a time")
	assert.Contains(t, s, "clang --version")
	assert.Contains(t, s, "/opt/local/bin/port version", "MacPorts answering is the last thing checked")
	assert.Contains(t, s, "exit 0", "findings are printed, not signalled by exit status")
	for _, p := range ForeignPrefixes {
		assert.Contains(t, s, p)
	}
}

// The prefix is threaded through: an ephemeral-prefix backend checks an
// installation that is not at /opt/local.
func TestCleanScriptUsesTheGivenPrefix(t *testing.T) {
	s := CleanScript("/opt/dockhand/e/abc/bin/port")
	assert.Contains(t, s, "/opt/dockhand/e/abc/bin/port installed")
	assert.NotContains(t, s, "/opt/local/bin/port")
}

func TestLintArgsLeadTheRun(t *testing.T) {
	assert.Equal(t, []string{"lint", "jq"}, LintArgs("jq"))
}
