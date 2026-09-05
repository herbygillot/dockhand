// Package build knows how to obtain a MacPorts installation and drive
// it into building a port: which installer a given macOS release wants,
// where an edited portdir has to sit for the indexer to see it, what
// portindex says about what it saw, how a local tree is put ahead of
// the installation's own, and what port(1) is asked to install.
//
// None of it knows where the installation is. That is the split this
// package exists to make: a VM, an ephemeral prefix and a CI runner
// differ in how a command gets there and how its output comes back,
// not in which command it is. Everything here is pure — argv in,
// strings out, no execution and no transport — so a provider supplies
// the transport and nothing else.
package build

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
)

// ErrNotAPortdir reports a path that cannot be staged, because the
// category directory the indexer needs is not above it.
var ErrNotAPortdir = errors.New("build: not a <category>/<port> directory")

// ForeignPrefixes are the other package managers a MacPorts build can
// find by accident. A verdict environment that carries one is not
// answering the question it was asked: a port that builds against
// Homebrew's libraries proves nothing about a machine that has none,
// and D4's declaration-completeness is exactly the proposition such a
// prefix destroys.
//
// MacPorts' own binpath already excludes these from build phases, so
// their binaries are not found — but binpath governs executables, not
// headers, and a configure script that probes /opt/homebrew/include
// finds what is there regardless. Removing them is the only way to be
// sure, and an environment provisioned for verdicts can afford to be.
//
// Each entry is a path that only a package manager creates. That
// distinction matters: /usr/local is not on this list, because a stock
// macOS has one — empty, owned by root — and treating its existence as
// contamination fails every clean image there is. Homebrew's presence
// under it is named exactly, by the directories only Homebrew makes.
//
// The list is what MacPorts' own guide names as conflicting, plus the
// Apple silicon Homebrew prefix it predates.
var ForeignPrefixes = []string{
	"/opt/homebrew",       // Homebrew on Apple silicon: the prefix is its own
	"/usr/local/Homebrew", // Homebrew on Intel, which shares /usr/local
	"/usr/local/Cellar",
	"/sw",      // Fink
	"/opt/pkg", // pkgsrc
}

// PathInjectors are the places a package manager puts itself on every
// user's PATH. Removing a prefix and leaving these behind leaves a
// machine whose PATH names directories that no longer exist — untidy
// rather than dangerous, but it also leaves the next reader unsure
// whether the excision worked.
var PathInjectors = []string{
	"/etc/paths.d/homebrew",
	"/etc/paths.d/fink",
}

// DistfilesURL is where MacPorts publishes its own releases.
const DistfilesURL = "https://distfiles.macports.org/MacPorts"

// InstallerName is the package MacPorts publishes for one release on
// one macOS version: MacPorts-2.12.6-15-Sequoia.pkg. It pairs the
// product version with the marketing name, spaces removed — two of a
// release's three names in one string, which is why the table that
// knows all three lives in its own package.
func InstallerName(version string, r platform.Release) string {
	return fmt.Sprintf("MacPorts-%s-%s-%s.pkg", version, r.Product, r.CompactName())
}

// InstallerURL is where to fetch that package.
func InstallerURL(version string, r platform.Release) string {
	return DistfilesURL + "/" + InstallerName(version, r)
}

// Layout returns the category and port name a portdir must be staged
// under. portindex walks categories, so a portdir staged on its own
// indexes nothing at all — and a build against an overlay that indexed
// nothing silently tests the installation's own copy of the port
// instead of the edited one.
func Layout(portdir string) (category, name string, err error) {
	clean := filepath.Clean(portdir)
	name = filepath.Base(clean)
	category = filepath.Base(filepath.Dir(clean))
	switch {
	case name == "" || name == "." || name == string(filepath.Separator),
		category == "" || category == "." || category == string(filepath.Separator):
		return "", "", fmt.Errorf("%w: %s", ErrNotAPortdir, portdir)
	}
	return category, name, nil
}

// Tally is portindex's own count of what it saw.
type Tally struct {
	Parsed    int
	Succeeded int
	Failed    int
}

// Complete reports a tally that describes a usable overlay: something
// was indexed, and nothing failed to parse.
func (t Tally) Complete() bool { return t.Parsed > 0 && t.Failed == 0 && t.Succeeded > 0 }

var (
	reParsed    = regexp.MustCompile(`(?m)^Total number of ports parsed:\s+(\d+)`)
	reSucceeded = regexp.MustCompile(`(?m)^Ports successfully parsed:\s+(\d+)`)
	reFailed    = regexp.MustCompile(`(?m)^Ports failed:\s+(\d+)`)
)

// ErrNoTally reports output that is not portindex's.
var ErrNoTally = errors.New("build: portindex reported no tally")

// ParseTally reads portindex's summary. Absence of a tally is an error
// rather than a zero: output that cannot be read must not be mistaken
// for an index that found nothing, and neither may be mistaken for
// success.
func ParseTally(out string) (Tally, error) {
	num := func(re *regexp.Regexp) (int, bool) {
		m := re.FindStringSubmatch(out)
		if m == nil {
			return 0, false
		}
		n, err := strconv.Atoi(m[1])
		return n, err == nil
	}
	parsed, ok1 := num(reParsed)
	succeeded, ok2 := num(reSucceeded)
	failed, ok3 := num(reFailed)
	if !ok1 || !ok2 || !ok3 {
		return Tally{}, ErrNoTally
	}
	return Tally{Parsed: parsed, Succeeded: succeeded, Failed: failed}, nil
}

// ResourcesDir is the ports tree directory MacPorts reads its own
// configuration out of — archive sites, mirror sites, port groups. A
// staged tree without it is not a ports tree: portarchivefetch resolves
// archive_sites.tcl under the port's own tree with the fallback
// disabled, so a port served without it can reach no archive at all.
const ResourcesDir = "_resources"

// SourcesLine is the sources.conf entry for a local tree. nosync says
// the installation must not try to update it: an overlay is staged, not
// fetched.
func SourcesLine(root string) string { return "file://" + root + " [nosync]" }

// InstallArgs is what port(1) is asked to do. Order matters: options
// precede the subcommand, and the variant frame follows the port as
// separate words.
//
// fromSource forces a build even where an archive exists. A version
// bump does not need it — the new version names an archive that has
// never been published, so the port builds from source on its own while
// its untouched dependencies still arrive as binaries. A re-derivation
// at an unchanged version does need it, because the archive that
// matches predates the change. The flag is all-or-nothing in port(1):
// asking for source builds the dependencies from source too, which is
// the difference between seconds and minutes.
func InstallArgs(port string, variants info.VariantSet, fromSource bool) []string {
	// -d is deliberate: the environment is disposable and the log is
	// the artifact, so verification runs at full verbosity — mpbb's
	// -dkn made the same call for the same reason.
	args := []string{"-d", "-N"}
	if fromSource {
		args = append(args, "-s")
	}
	args = append(args, "install", port)
	return append(args, variants.List()...)
}

// LintArgs asks port(1) to lint the Portfile. It leads every
// verification: the environment is already there, lint is cheap, and
// its passing is what lets the PR template's lint box be checked
// honestly. Errors fail the run; warnings do not — port lint's own
// exit code draws that line.
func LintArgs(port string) []string {
	return []string{"lint", port}
}

// TestArgs asks port(1) to run the port's test suite, under the same
// variant frame the install will use. It runs BEFORE the install, the
// way mpbb's test-port does: a fresh invocation builds the port through
// the test phase under one consistent privilege drop, where testing
// after an install rebuilds in a work directory whose files two
// invocations own between them — measured as EPERM in the guest. -k
// keeps the work directory, so the install that follows continues from
// the built state (build → destroot → activate is the normal
// single-run progression) instead of starting the port over.
func TestArgs(port string, variants info.VariantSet) []string {
	return append([]string{"-d", "-N", "-k", "test", port}, variants.List()...)
}

// DeactivateArgs asks port(1) to take an installed port out of the
// active set, dependents and all, so that a port declaring a conflict
// with it can be activated in its place.
//
// It exists for one caller's one case. D24 rules that two members
// MacPorts will not activate together are not built together: the one
// that loses the seat is bumped and left out of the guest, withheld.
// A person may override that (ruled 2026-09-05 by the orchestrator,
// pending the maintainer) and have the withheld member built anyway,
// and this is the step that makes that possible — the seated sibling
// is deactivated immediately before the forced member is built, in
// the environment that built the sibling, so MacPorts' own conflict
// check finds nothing active to object to.
//
// -f is the whole of it. Under -N nothing is ever asked, and without
// force a deactivate of a port that other installed ports depend on
// stops at the registry's dependents check — "Please uninstall the
// ports that depend on X first" — which is exactly the situation a
// seated sibling is in once the members that needed it have been
// built. Forced, the registry warns that it is proceeding despite the
// dependencies and proceeds. Those dependents keep their files and
// their records; what they lose is a guarantee that the library they
// linked is the one now active, which is why the caller builds a
// forced member last, after every member that might need the sibling.
//
// Force goes on the deactivate and never on the install. `port -f
// install` past a conflict only warns and then collides at activation
// on the files both ports lay down; taking the sibling out first is
// the lever that leaves nothing to collide with. Options precede the
// action word as port(1)'s synopsis and InstallArgs's convention have
// it, and -d for the same reason as everywhere else here: the log is
// the artifact.
func DeactivateArgs(port string) []string {
	return []string{"-d", "-N", "-f", "deactivate", port}
}

// CleanScript is a shell script that reports everything wrong with a
// MacPorts environment, one finding per line, and prints nothing when
// there is nothing wrong. Empty output is the pass.
//
// It exists as a script rather than a sequence of calls because the
// callers reach their environments by different means — a guest agent,
// a local exec, eventually a CI step — and the question is the same for
// all of them. What differs is transport, which is the provider's.
//
// The four findings are the ways an environment can look ready and not
// be. Ports installed means a previous verification leaked into a base
// that should be pristine. A foreign prefix means a build can find a
// package manager the port never declared. No compiler means every port
// will fail, opaquely and one at a time — a published image shipped
// exactly that way. And MacPorts not answering means the environment
// was never finished.
func CleanScript(portCmd string) string {
	var b strings.Builder
	b.WriteString("installed=$(" + portCmd + " installed 2>/dev/null | grep -v 'No ports are installed' | grep -c . || true)\n")
	b.WriteString("[ \"$installed\" != \"0\" ] && echo \"ports already installed: $installed\"\n")
	for _, p := range append(append([]string{}, ForeignPrefixes...), PathInjectors...) {
		b.WriteString("[ -e " + p + " ] && echo \"foreign package manager: " + p + "\"\n")
	}
	b.WriteString("xcode-select -p >/dev/null 2>&1 || echo 'no developer tools'\n")
	b.WriteString("clang --version >/dev/null 2>&1 || echo 'no working compiler'\n")
	b.WriteString(portCmd + " version >/dev/null 2>&1 || echo 'MacPorts does not answer'\n")
	b.WriteString("exit 0\n")
	return b.String()
}
