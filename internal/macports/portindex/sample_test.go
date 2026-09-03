package portindex

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// These tests run against the checked-in slice of a real PortIndex —
// verbatim entries out of a file MacPorts' own portindex wrote — rather
// than against a fixture this package writes for itself. The synthetic
// fixture agreed with a reader that could not walk a real tree at all,
// and agreed with it for as long as it existed; only real bytes can
// tell that apart.
//
// The numbers asserted here are measured over the slice, so they are
// exact rather than approximate. The one figure deliberately loose is
// the subport proportion, which drifts with the tree.

// sampleIndex opens the checked-in slice.
func sampleIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := Open(testenv.PortIndexSample(t))
	require.NoError(t, err)
	return ix
}

// entries collects the whole slice through Each.
func entries(t *testing.T, ix *Index) []Entry {
	t.Helper()
	var out []Entry
	require.NoError(t, ix.Each(func(e Entry) bool {
		out = append(out, e)
		return true
	}))
	return out
}

func TestEachWalksTheWholeRealIndex(t *testing.T) {
	ix := sampleIndex(t)
	all := entries(t, ix)
	assert.Len(t, all, 116)
	assert.Equal(t, 116, ix.Len(), "the accelerator names the same entries the walk finds")

	names := make(map[string]bool, len(all))
	for _, e := range all {
		assert.NotEmpty(t, e.Portdir, "%s has no portdir", e.Name)
		assert.False(t, names[e.Name], "%s yielded twice", e.Name)
		names[e.Name] = true
	}
}

func TestEachStopsWhenTheCallerDoes(t *testing.T) {
	seen := 0
	require.NoError(t, sampleIndex(t).Each(func(Entry) bool {
		seen++
		return seen < 3
	}))
	assert.Equal(t, 3, seen)
}

// The framing quirks, each one an entry that a reader getting the unit
// wrong reads differently. A byte-counting reader truncates
// R-ADGofTest's payload mid-portdir; a code-point-counting one eats the
// first character of the name after arangodb.
func TestRealFramingQuirks(t *testing.T) {
	ix := sampleIndex(t)

	// Two en-dashes: the declared length is characters, not bytes.
	adg, err := ix.Lookup("R-ADGofTest")
	require.NoError(t, err)
	assert.Equal(t, "R/R-ADGofTest", adg.Portdir)

	// An astral character: the unit is UTF-16, not code points. The
	// entry after it is the one that pays for getting this wrong.
	all := entries(t, ix)
	var next string
	for i, e := range all {
		if e.Name == "arangodb" {
			require.Less(t, i+1, len(all), "arangodb must not be the last entry, or nothing follows to be misread")
			next = all[i+1].Name
		}
	}
	assert.Equal(t, "librasterlite2", next)

	// A payload is not a line: xz's long_description holds a newline of
	// its own, so only the declared length says where the entry ends.
	xz, err := ix.Lookup("xz")
	require.NoError(t, err)
	assert.Contains(t, xz.Fields["long_description"], "\n")
}

// Open falls back to scanning the PortIndex when the accelerator is
// missing, and that fallback is what byte framing made unreachable: it
// died four entries in. A tree delivered without a quick file, or with
// a stale one, is an ordinary tree.
func TestOpenWithoutTheAccelerator(t *testing.T) {
	root := t.TempDir()
	body, err := os.ReadFile(filepath.Join(testenv.PortIndexSample(t), macports.IndexFile))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), body, 0o644))

	ix, err := Open(root)
	require.NoError(t, err)
	assert.Equal(t, 116, ix.Len())
	e, err := ix.Lookup("gdal")
	require.NoError(t, err)
	assert.Equal(t, "gis/gdal", e.Portdir)
}

func TestDependentsOverARealIndex(t *testing.T) {
	deps, err := sampleIndex(t).Dependents()
	require.NoError(t, err)

	rows := deps.ByPort["gdal"]
	require.Len(t, rows, 82, "every port in the slice that declares gdal")

	portdirs := map[string]bool{}
	var buildOnly, runEdges []string
	for _, r := range rows {
		portdirs[r.Portdir] = true
		if r.BuildOnly() {
			buildOnly = append(buildOnly, r.Name)
		}
		for _, k := range r.Keys {
			if k == DependsRun {
				runEdges = append(runEdges, r.Name)
			}
		}
	}
	// The staging unit is the portdir, and 82 dependents are 39 edits.
	assert.Len(t, portdirs, 39)
	// A build-only dependent is listed and never proposed: rebuilding
	// it produces the same binary.
	assert.Equal(t, []string{
		"libosmium", "libosmium-doc",
		"py310-pysaga", "py311-pysaga", "py312-pysaga", "py313-pysaga", "py314-pysaga",
	}, buildOnly)
	assert.Equal(t, []string{"R-mlr"}, runEdges)

	// Six of gdal's own dependents live in gdal's own portdir. A cohort
	// that staged them would plan an edit to the Portfile it just
	// changed, so the caller has to see them as they are.
	var siblings []string
	for _, r := range rows {
		if r.Portdir == "gis/gdal" {
			siblings = append(siblings, r.Name)
		}
	}
	assert.Equal(t, []string{
		"gdal-hdf4", "gdal-hdf5", "gdal-kea", "gdal-libkml", "gdal-netcdf", "gdal-pdf",
	}, siblings)

	// Rows come back sorted, so the cohort a run proposes is the cohort
	// the next run proposes.
	assert.True(t, sort.SliceIsSorted(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name }))
}

// Subports have no directory of their own, so the reverse index reports
// them at their parent's portdir — the index's own field, never a guess
// from the name. Judy's seven php dependents are one portdir to edit.
func TestSubportsCollapseToTheirParentPortdir(t *testing.T) {
	ix := sampleIndex(t)
	deps, err := ix.Dependents()
	require.NoError(t, err)

	var php []string
	portdirs := map[string]bool{}
	for _, r := range deps.ByPort["judy"] {
		if r.Portdir == "php/php-Judy" {
			php = append(php, r.Name)
		}
		portdirs[r.Portdir] = true
	}
	assert.Equal(t, []string{
		"php80-Judy", "php81-Judy", "php82-Judy", "php83-Judy",
		"php84-Judy", "php85-Judy", "php86-Judy",
	}, php)
	assert.Len(t, portdirs, 2, "seven php subports and netdata are two portdirs")

	// The proportion, measured over the slice rather than pinned: a
	// name that is not its portdir's basename is the common case, not
	// the exception, which is why the mapping cannot be derived.
	all := entries(t, ix)
	unlike := 0
	basenames := map[string]bool{}
	for _, e := range all {
		basenames[filepath.Base(e.Portdir)] = true
	}
	for _, e := range all {
		if e.Name != filepath.Base(e.Portdir) {
			unlike++
		}
	}
	assert.Greater(t, unlike, len(all)/3, "%d of %d names differ from their portdir's basename", unlike, len(all))
	for _, e := range all {
		if e.Name != filepath.Base(e.Portdir) {
			assert.False(t, basenames[e.Name], "%s must match no portdir basename anywhere", e.Name)
		}
	}
}

// All four token forms, taken from real values rather than invented
// ones. netdata carries every form in one depends_lib.
func TestDependencyTokenFormsFromRealValues(t *testing.T) {
	deps, err := sampleIndex(t).Dependents()
	require.NoError(t, err)

	named := func(target string) []string {
		var out []string
		for _, r := range deps.ByPort[target] {
			out = append(out, r.Name)
		}
		return out
	}

	// port:brotli, bin:curl:curl, lib:libuuid:libuuid,
	// path:lib/libssl.dylib:openssl — one value, four forms.
	assert.Contains(t, named("brotli"), "netdata")
	assert.Contains(t, named("curl"), "netdata")
	assert.Contains(t, named("libuuid"), "netdata")
	assert.Contains(t, named("openssl"), "netdata")

	// lib: and bin: on their own, the two rarest forms.
	assert.Equal(t, []string{"BigSQL"}, named("postgresql83"))
	assert.Equal(t, []string{"R-WriteXLS"}, named("perl5"))
	assert.Equal(t, []string{DependsRun}, deps.ByPort["perl5"][0].Keys)

	// Brace-quoted elements: split as a Tcl list, the name is gmake and
	// postgresql84; split on whitespace, it is `postgresql84}`.
	assert.Equal(t, []string{"mercurial"}, named("gmake"))
	assert.Equal(t, []string{"BiggerSQL"}, named("postgresql84"))
	assert.Empty(t, named("postgresql84}"))

	// A path: token whose test field is full of slashes and dots still
	// resolves on the last colon.
	assert.Contains(t, named("qt6-qtbase"), "djview-qt5")
}

// Dependents do not always spell a port the way it is indexed. Folding
// the key recovers those edges, and it is lossless: the index has no
// two names differing only by case.
func TestDependentsFoldTheTargetName(t *testing.T) {
	deps, err := sampleIndex(t).Dependents()
	require.NoError(t, err)
	require.Len(t, deps.ByPort["speexdsp"], 1)
	assert.Equal(t, "cubeb", deps.ByPort["speexdsp"][0].Name, "cubeb declares port:speexDSP")
	assert.Empty(t, deps.ByPort["speexDSP"], "the reverse index is keyed lowercased")
}

// The exclusion signals the index actually carries. There is no
// obsolete category and no obsolete field: replaced_by and known_fail
// are what a real tree says, and a filter written against anything else
// would exclude nobody while claiming it had.
func TestDependentsCarryTheExclusionSignals(t *testing.T) {
	ix := sampleIndex(t)
	deps, err := ix.Dependents()
	require.NoError(t, err)

	var omegat, djview Dependent
	for _, r := range deps.ByPort["openjdk11"] {
		if r.Name == "OmegaT-latest" {
			omegat = r
		}
	}
	for _, r := range deps.ByPort["qt6-qtbase"] {
		if r.Name == "djview-qt5" {
			djview = r
		}
	}
	assert.Equal(t, "OmegaT", omegat.ReplacedBy)
	assert.Equal(t, "djview", djview.ReplacedBy)
	assert.True(t, djview.KnownFail)

	for _, e := range entries(t, ix) {
		assert.NotContains(t, e.Fields["categories"], "obsolete")
		assert.Empty(t, e.Fields["obsolete"])
	}
}

// A row carries its own dependency targets so that ordering a cohort —
// build the library before the things that link it — is arithmetic over
// values, with no second walk of the index from inside a judgment that
// is not allowed one.
func TestDependentsCarryTheirOwnRequirements(t *testing.T) {
	deps, err := sampleIndex(t).Dependents()
	require.NoError(t, err)
	var judy Dependent
	for _, r := range deps.ByPort["judy"] {
		if r.Name == "php85-Judy" {
			judy = r
		}
	}
	assert.Equal(t, []string{"autoconf", "judy", "php85"}, judy.Requires)
	assert.True(t, sort.StringsAreSorted(judy.Requires))
}

func TestByMaintainerOverARealIndex(t *testing.T) {
	byKey, err := sampleIndex(t).ByMaintainer()
	require.NoError(t, err)

	// One person under two spellings, both kept: the index gives no
	// ground for deciding they are the same person.
	assert.Contains(t, byKey["gh:nilason"], "gdal")
	assert.Contains(t, byKey["mail:n_larsson@yahoo.com"], "gdal")

	// A nested element is two maintainers plus a keyword, not three
	// maintainers named `{nicos`, `@NicosPavlov}` and `openmaintainer`.
	assert.Contains(t, byKey["gh:nicospavlov"], "djview-qt5")
	assert.Contains(t, byKey["mail:nicos@macports.org"], "djview-qt5")
	assert.NotContains(t, byKey, "mail:openmaintainer@macports.org")
	assert.NotContains(t, byKey, "mail:nomaintainer@macports.org")

	// nomaintainer is the absence of a maintainer, so proj is under no
	// key at all.
	for key, ports := range byKey {
		assert.NotContains(t, ports, "proj", "proj is nomaintainer but appears under %s", key)
	}
	proj, err := sampleIndex(t).Lookup("proj")
	require.NoError(t, err)
	assert.True(t, proj.Nomaintainer())
	assert.Empty(t, proj.Maintainers())
}
