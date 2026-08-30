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

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
)

// ErrNotAPortdir reports a path that cannot be staged, because the
// category directory the indexer needs is not above it.
var ErrNotAPortdir = errors.New("build: not a <category>/<port> directory")

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
	args := []string{"-N"}
	if fromSource {
		args = append(args, "-s")
	}
	args = append(args, "install", port)
	return append(args, variants.List()...)
}
