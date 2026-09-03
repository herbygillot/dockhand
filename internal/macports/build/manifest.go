package build

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/verify"
)

// BaselineArgs is the binary-only install that takes the "before"
// measurement: fetch and activate the published archive for this port,
// and refuse rather than build it.
//
// -b is what makes the measurement affordable and what makes it honest.
// Affordable, because MacPorts selects only depends_lib and depends_run
// for a binary install, so nothing compiles and no build dependency is
// pulled. Honest, because the whole point of a baseline is the version
// the change is leaving: a -b that cannot find an archive says so and
// stops, where a source build would quietly produce a "before" that
// nobody ever shipped.
//
// The variant frame is the branch build's, deliberately. An archive is
// named by version, revision AND variants, so a baseline taken under a
// different frame compares two different builds and reports a phantom
// ABI change.
//
// No -d, unlike InstallArgs. The build log is the artifact of a
// verification; a baseline is a measurement taken before one, and full
// verbosity here buys nothing and buries the failure that matters.
func BaselineArgs(port string, variants info.VariantSet) []string {
	return append([]string{"-N", "-b", "install", port}, variants.List()...)
}

// UninstallArgs removes the baseline again, so the branch's own build
// starts from an environment that has the port's dependencies and not
// the port.
//
// -f is not optional. MacPorts refuses to uninstall a port other ports
// depend on ("Please uninstall the ports that depend on <X> first"), and
// it would normally offer the override as a question — which a detached
// runner with no terminal never sees. With -f it says "Uninstall
// forced" and proceeds, and the dependencies stay installed, which is
// the point: they are exactly the binaries the branch build would pull.
func UninstallArgs(port string) []string {
	return []string{"-N", "-f", "uninstall", port}
}

// InstalledArgs asks port(1) what it holds of one port, quietly: one
// line per installed version, or nothing at all.
//
// It exists because port(1)'s exit code cannot answer the question.
// `port -q installed <a port that is not installed>` exits 0 and prints
// nothing — measured — so a caller reading the status of the install
// that came before it would record a baseline that was never taken, and
// every comparison against it would read as "every library removed":
// the strongest false break available.
func InstalledArgs(port string) []string {
	return []string{"-q", "installed", port}
}

// ErrNotInstalled reports that port(1) named no installed version.
var ErrNotInstalled = errors.New("build: the port is not installed")

// ParseInstalled reads the version out of `port -q installed`. The
// version carried is the whole archive-naming string —
// version_revision+variants — because that, and not the bare version,
// is what identifies the build a manifest describes.
//
// The active version wins where several are installed, because the
// active one is what a dependent would have linked against. Where none
// is marked active the first is taken, and that is a real state: a port
// can be installed and inactive.
func ParseInstalled(out string) (string, error) {
	var first string
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		var version string
		for _, f := range fields {
			if strings.HasPrefix(f, "@") {
				version = strings.TrimPrefix(f, "@")
				break
			}
		}
		if version == "" {
			continue
		}
		if first == "" {
			first = version
		}
		if strings.Contains(line, "(active)") {
			return version, nil
		}
	}
	if first == "" {
		return "", ErrNotInstalled
	}
	return first, nil
}

// The two ways a binary-only install refuses, in MacPorts' own words.
//
// They are matched as substrings of the transcript rather than inferred
// from an exit code, because port(1) exits non-zero for every kind of
// failure and a baseline that could not be taken must be told apart from
// one that failed for a reason worth reporting. The first comes from
// portunarchive.tcl, surfaced through portutil.tcl; the second from
// portarchivefetch.tcl. An archivefetch that merely fails to download
// does not raise the second — it falls through to the first.
const (
	noArchive     = "required when binary-only is set"
	noArchiveSite = "no usable archive sites configured"
)

// BaselineFailure explains a failed binary-only install in the words
// the environment used.
//
// The two known refusals are quoted from the line that carried them, so
// the finding says which archive was missing. Anything else falls back
// to the transcript's last non-empty line, which is where port(1) puts
// the error that stopped it — a weaker answer, and deliberately not a
// silent one.
func BaselineFailure(out string) string {
	var last string
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		last = line
		if strings.Contains(line, noArchive) || strings.Contains(line, noArchiveSite) {
			return line
		}
	}
	if last == "" {
		return "the environment said nothing"
	}
	return last
}

// manifestPrefix opens a section of a captured manifest. It spells
// dockhand out for the same reason the subject marker does: the capture
// crosses a guest agent that merges stdout and stderr, and a frame keyed
// on something port(1) or a build system could plausibly print would let
// a warning be read as a file path or an install name.
const manifestPrefix = "===> dockhand manifest: "

// The sections of a capture, in the order the script writes them. The
// closing one carries no body and is the whole durability guarantee: a
// capture that was cut off — the guest died, the disk filled, the exec
// was killed — is missing it, and is refused rather than parsed. A
// truncated file list read as a manifest is a set of libraries that
// vanished, which is indistinguishable from a real and total ABI break.
const (
	sectionPort     = "port"
	sectionVersion  = "version"
	sectionPlatform = "platform"
	sectionFiles    = "files"
	sectionID       = "id"
	sectionLinks    = "links"
	sectionEnd      = "end"
)

// ManifestScript writes one framed manifest into the guest, atomically.
//
// The port name is read out of a file rather than spelled into the
// script, the way every other name this package hands a guest travels:
// nothing here is syntax a Portfile could have written. The roster file
// is the runner's own, one port per line in request order, and the index
// selects a line — so the same script serves the headline and every
// member of a cohort.
//
// Three things about the body are load-bearing and none of them is
// style. `port contents` is asked once and kept, because each
// invocation is a Tcl interpreter start. Its two-space indent is
// stripped explicitly, because a path read with its indent is a file
// that does not exist and otool answers that on stderr, which is
// discarded — so the library would simply be absent rather than
// reported missing. And otool is handed the whole list at once through
// xargs -0 rather than run per file: measured, a per-file loop over one
// large port's sixteen thousand files takes about 280 seconds where the
// batch takes three, and a cohort of them would spend an hour inside the
// guest where nothing can see it.
//
// -arch all is what makes the shape deterministic. Without it otool
// prints one section for a universal file whose slices include the
// host's exact subtype and a section per slice otherwise — measured, on
// one machine, on two files — so a capture taken on one host and a
// capture taken on another would disagree about how many libraries a
// port has. With it every slice is always named.
//
// The write is a rename, for the reason the cohort runner's state files
// are: `> f` is truncate-then-write, and a reader landing in that window
// sees a file that parses into a partial manifest rather than a file
// that is obviously absent.
func ManifestScript(portCmd, dest, roster string, index int) string {
	return `set -u
n=0
subject=
while IFS= read -r line; do
  if [ "$n" -eq ` + strconv.Itoa(index) + ` ]; then subject=$line; break; fi
  n=$((n+1))
done < ` + roster + `
[ -n "$subject" ] || { echo "no subject at line ` + strconv.Itoa(index+1) + ` of ` + roster + `" >&2; exit 1; }
files=` + dest + `.files
` + portCmd + ` -q contents "$subject" 2>/dev/null | sed 's/^  //' > "$files"
{
  printf '` + manifestPrefix + sectionPort + `\n%s\n' "$subject"
  printf '` + manifestPrefix + sectionVersion + `\n'
  ` + portCmd + ` -q installed "$subject" 2>/dev/null
  printf '` + manifestPrefix + sectionPlatform + `\n'
  printf '%s %s\n' "$(/usr/bin/sw_vers -productVersion 2>/dev/null)" "$(/usr/bin/uname -m 2>/dev/null)"
  printf '` + manifestPrefix + sectionFiles + `\n'
  cat "$files"
  printf '` + manifestPrefix + sectionID + `\n'
  tr '\n' '\0' < "$files" | /usr/bin/xargs -0 /usr/bin/otool -arch all -D 2>/dev/null
  printf '` + manifestPrefix + sectionLinks + `\n'
  tr '\n' '\0' < "$files" | /usr/bin/xargs -0 /usr/bin/otool -arch all -L 2>/dev/null
  printf '` + manifestPrefix + sectionEnd + `\n'
} > ` + dest + `.part && mv -f ` + dest + `.part ` + dest + `
rm -f "$files"
`
}

// Capture is one framed manifest read back: the installation as the
// environment described it, and the bindings the same otool sweep saw.
//
// The bindings are here rather than on verify.Manifest because they are
// an observation about this capture and not a property of the port: a
// manifest says what a port laid down, and who links to what is a
// question asked across an installation. The caller assembles the
// answer from as many captures as it took.
type Capture struct {
	Manifest verify.Manifest
	// LinksTo maps an install name to the captured files that record it
	// as a dependency, in capture order and without repeats.
	LinksTo map[string][]string
}

var (
	// ErrNoManifest reports output that carries no frame at all — the
	// script never ran, or something else answered.
	ErrNoManifest = errors.New("build: no manifest frame in the output")
	// ErrManifestTruncated reports a frame with no closing section. It is
	// its own error because the remedy differs: a capture that was cut
	// off is a fact about the environment, where output with no frame is
	// a fact about what was run.
	ErrManifestTruncated = errors.New("build: the manifest frame is truncated")
)

var (
	reArchHeader = regexp.MustCompile(`^(.*) \(architecture ([^)]+)\):$`)
	// The trailing comma alternative is not decoration: otool prints a
	// fourth field for a weak, reexported or upward link, and eight such
	// lines exist in this machine's own prefix.
	reLinkLine = regexp.MustCompile(`^\s+(.*) \(compatibility version ([^,]+), current version ([^,)]+)[,)]`)
)

const notAnObject = ": is not an object file"

// slice is one file as one architecture: the key everything about a
// Mach-O is recorded under, because a universal file is several
// libraries under one path and they can disagree.
type machoSlice struct {
	path string
	arch string
}

// ParseManifest reads a framed capture.
//
// It never consults an exit status, and the reason is measured: otool
// handed a batch containing one file that has since vanished writes its
// complaint to stderr, exits 1, and still prints every good file on
// stdout. Treating that as a failed capture would throw away a complete
// manifest because one file disappeared between the listing and the
// sweep.
//
// A file that is not Mach-O is skipped by the environment itself, on
// stdout, in the form "<path>: is not an object file" — so the parser
// must recognize that line as a terminator rather than read it as a
// path. Which lines are headers is decided against the file list the
// capture itself carries, not by a shape: an install name and a header
// differ only by a trailing colon, and every install name a real port
// publishes is also one of its own files.
func ParseManifest(out string) (Capture, error) {
	sections, err := frame(out)
	if err != nil {
		return Capture{}, err
	}

	m := verify.Manifest{
		Port:     firstLine(sections[sectionPort]),
		Platform: firstLine(sections[sectionPlatform]),
	}
	if v, err := ParseInstalled(sections[sectionVersion]); err == nil {
		m.Version = v
	}
	files := map[string]bool{}
	for line := range strings.Lines(sections[sectionFiles]) {
		// The same trim the header reader uses, so a path recorded here
		// and a header naming it are the same string.
		if p := strings.TrimRight(line, "\r\n"); p != "" {
			m.Files = append(m.Files, p)
			files[p] = true
		}
	}

	ids, idOrder := parseIDs(sections[sectionID], files)
	links := parseLinks(sections[sectionLinks], files)

	got := Capture{LinksTo: map[string][]string{}}
	for _, s := range idOrder {
		name := ids[s]
		d := verify.Dylib{Path: s.path, Arch: s.arch, InstallName: name}
		for _, l := range links[s] {
			if l.name == name {
				d.CompatVersion, d.CurrentVersion = l.compat, l.current
				break
			}
		}
		m.Dylibs = append(m.Dylibs, d)
	}
	// Every swept file's dependencies, whether or not the file is itself
	// a library: an executable links against the libraries this change
	// may have moved, and it breaks exactly as loudly. The order is the
	// file list's, so the answer does not depend on a map's.
	for _, path := range m.Files {
		for _, s := range slicesOf(path, links) {
			collect(got.LinksTo, s, links)
		}
	}
	got.Manifest = m
	return got, nil
}

// slicesOf is every architecture one path was captured as, in the order
// otool named them.
func slicesOf(path string, links map[machoSlice][]link) []machoSlice {
	var out []machoSlice
	if _, ok := links[machoSlice{path: path}]; ok {
		out = append(out, machoSlice{path: path})
	}
	for s := range links {
		if s.path == path && s.arch != "" {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].arch < out[j].arch })
	return out
}

// collect files one slice's dependencies under the names it recorded,
// once each: a path that appears as several slices links against the
// same library once as far as a reader is concerned.
func collect(into map[string][]string, s machoSlice, links map[machoSlice][]link) {
	for _, l := range links[s] {
		if !contains(into[l.name], s.path) {
			into[l.name] = append(into[l.name], s.path)
		}
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

// frame splits a capture into its sections, and refuses one that does
// not close.
func frame(out string) (map[string]string, error) {
	sections := map[string]*strings.Builder{}
	name := ""
	closed := false
	for line := range strings.Lines(out) {
		if rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), manifestPrefix); ok {
			name = strings.TrimSpace(rest)
			if name == sectionEnd {
				closed = true
				break
			}
			if sections[name] == nil {
				sections[name] = &strings.Builder{}
			}
			continue
		}
		// Anything before the first section header is whatever else the
		// transport put on the stream, and is dropped rather than read.
		if name == "" {
			continue
		}
		sections[name].WriteString(line)
	}
	if name == "" {
		return nil, ErrNoManifest
	}
	if !closed {
		return nil, ErrManifestTruncated
	}
	out2 := make(map[string]string, len(sections))
	for k, b := range sections {
		out2[k] = b.String()
	}
	return out2, nil
}

func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// header reads a `<path>:` or `<path> (architecture X):` line, against
// the files the capture said it swept.
func header(line string, files map[string]bool) (machoSlice, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return machoSlice{}, false
	}
	if p, ok := strings.CutSuffix(line, ":"); ok && files[p] {
		return machoSlice{path: p}, true
	}
	if m := reArchHeader.FindStringSubmatch(line); m != nil && files[m[1]] {
		return machoSlice{path: m[1], arch: m[2]}, true
	}
	return machoSlice{}, false
}

// parseIDs reads the install name each slice announces. A slice with an
// empty body is not a library — measured, `otool -D` on an executable
// prints its header and nothing else — and is left out rather than
// recorded with an empty name.
func parseIDs(section string, files map[string]bool) (map[machoSlice]string, []machoSlice) {
	ids := map[machoSlice]string{}
	var order []machoSlice
	var cur machoSlice
	open := false
	for line := range strings.Lines(section) {
		line = strings.TrimRight(line, "\r\n")
		if s, ok := header(line, files); ok {
			cur, open = s, true
			continue
		}
		if p, ok := strings.CutSuffix(line, notAnObject); ok && files[p] {
			open = false
			continue
		}
		if !open || strings.TrimSpace(line) == "" {
			continue
		}
		if _, seen := ids[cur]; !seen {
			ids[cur] = strings.TrimSpace(line)
			order = append(order, cur)
		}
	}
	return ids, order
}

// link is one dependency otool -L recorded, with what the dependent
// must satisfy.
type link struct{ name, compat, current string }

func parseLinks(section string, files map[string]bool) map[machoSlice][]link {
	out := map[machoSlice][]link{}
	var cur machoSlice
	open := false
	for line := range strings.Lines(section) {
		line = strings.TrimRight(line, "\r\n")
		if s, ok := header(line, files); ok {
			cur, open = s, true
			continue
		}
		if p, ok := strings.CutSuffix(line, notAnObject); ok && files[p] {
			open = false
			continue
		}
		if !open {
			continue
		}
		if m := reLinkLine.FindStringSubmatch(line); m != nil {
			out[cur] = append(out[cur], link{name: m[1], compat: m[2], current: m[3]})
		}
	}
	return out
}
