package dependents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/artifact"
	"github.com/herbygillot/dockhand/internal/darwin/abi"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
)

// The measurements are taken through darwin/abi rather than fabricated
// as Result literals, because what this package is entitled to assume
// about a verdict is exactly what that comparator produces. A hand-made
// Changed carrying no clauses would let a decline sentence pass a test
// that quotes them.
//
// The manifests are two rows rather than the transcribed captures next
// door: what a cohort decision needs from a measurement is its verdict,
// its criterion and its unmeasured files, and the capture fidelity that
// matters to the comparison itself is pinned in that package against
// the capture files.

// widget is one installation of the headline, publishing one library.
func widget(version, soname, compat string) *artifact.Manifest {
	return &artifact.Manifest{Port: "libwidget", Version: version, Platform: "Sequoia",
		Dylibs: []artifact.Dylib{{Path: soname, InstallName: soname,
			CompatVersion: compat, CurrentVersion: compat}}}
}

// delta is the ordinary input: an environment that answered, a baseline
// unpacked from a binary archive, a branch built from source.
func delta(before, after *artifact.Manifest) abi.Result {
	return abi.Delta(abi.Input{
		Port: "libwidget", Portdir: "devel/libwidget", Described: true, FromSource: true,
		Baseline: before, Source: abi.Archive, Candidate: after,
	})
}

// abiChanged is the acceptance test's own measurement: libwidget's
// install name moved, measured between the archive of 2.4.1 and the
// 3.0.0 this branch built.
func abiChanged() abi.Result {
	return delta(
		widget("2.4.1_0", "/opt/local/lib/libwidget.2.dylib", "2.0.0"),
		widget("3.0.0_0", "/opt/local/lib/libwidget.3.dylib", "3.0.0"))
}

// abiUnchanged is the same port rebuilt with nothing moved — the
// up-front cohort a measurement refutes.
func abiUnchanged() abi.Result {
	m := widget("2.4.1_0", "/opt/local/lib/libwidget.2.dylib", "2.0.0")
	return delta(m, m)
}

// lib is a plain depends_lib dependent.
func lib(port, portdir string, requires ...string) Dependent {
	return Dependent{Port: port, Portdir: portdir, Keys: []string{"depends_lib"}, Requires: requires}
}

// ports reads a candidate list back as names, which is what nearly
// every assertion here is about.
func ports(all []record.Candidate) []string {
	out := make([]string, 0, len(all))
	for _, c := range all {
		out = append(out, c.Port)
	}
	return out
}

// reasonFor finds one port's recorded reason. Every port examined
// carries one, proposed or not: a decision no reader can see is a
// decision nobody can disagree with.
func reasonFor(t *testing.T, all []record.Candidate, port string) record.Candidate {
	t.Helper()
	for _, c := range all {
		if c.Port == port {
			return c
		}
	}
	require.Failf(t, "not examined", "%s appears in no candidate row", port)
	return record.Candidate{}
}

func TestMembersComeBackInDependencyOrder(t *testing.T) {
	// The guest's runner skips a member whose prerequisite failed and
	// builds the rest, and it can only do that where prerequisites come
	// first: a member built before the thing it needs fails for a
	// reason that has nothing to do with the change.
	c := Propose(abiChanged(), nil, []Dependent{
		lib("qgis", "gis/qgis", "gdal", "proj", "libwidget"),
		lib("gdal", "gis/gdal", "proj", "libwidget"),
		lib("proj", "gis/proj", "libwidget"),
	}, nil, 0)

	require.True(t, c.Proposes())
	assert.Equal(t, []string{"proj", "gdal", "qgis"}, c.Ports())
	assert.Empty(t, c.Declined)
	// The headline is not a member of its own cohort, and an edge to it
	// orders nothing.
	assert.NotContains(t, c.Ports(), "libwidget")
}

func TestATieIsBrokenByNameSoTwoRunsProposeOneOrder(t *testing.T) {
	deps := []Dependent{lib("zzz", "z/zzz"), lib("mmm", "m/mmm"), lib("aaa", "a/aaa")}
	first := Propose(abiChanged(), nil, deps, nil, 0)
	second := Propose(abiChanged(), nil, deps, nil, 0)

	assert.Equal(t, []string{"aaa", "mmm", "zzz"}, first.Ports())
	assert.Equal(t, first.Ports(), second.Ports(), "the goldens must not move under a map's iteration")
}

func TestACycleIsDeclinedByNameRatherThanOrdered(t *testing.T) {
	// Two ports that need each other, and one behind them. Picking one
	// to go first would be a guess with a build behind it.
	c := Propose(abiChanged(), nil, []Dependent{
		lib("alpha", "a/alpha", "beta"),
		lib("beta", "b/beta", "alpha"),
		lib("gamma", "g/gamma", "alpha"),
		lib("solo", "s/solo"),
	}, nil, 0)

	assert.Equal(t, []string{"solo"}, c.Ports(), "what can be ordered still goes forward")
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, ports(c.Excluded))
	// The whole knot is named, because a member behind a cycle is as
	// unorderable as one inside it and the remedy is to look at them
	// together.
	assert.Contains(t, reasonFor(t, c.Excluded, "gamma").Reason,
		"dependency cycle among alpha, beta, gamma")
	assert.Contains(t, reasonFor(t, c.Excluded, "gamma").Reason, "by hand")
}

func TestBuildOnlyDependentsAreListedAndNeverProposed(t *testing.T) {
	// A build-only dependent links nothing this change publishes, so a
	// revbump of it rebuilds a binary that did not change — but it is a
	// fact the proposal has to state rather than one it may drop.
	c := Propose(abiChanged(), nil, []Dependent{
		lib("gdal", "gis/gdal"),
		{Port: "widget-docs", Portdir: "doc/widget-docs", Keys: []string{"depends_build"}},
		{Port: "widget-tools", Portdir: "devel/widget-tools", Keys: []string{"depends_build", "depends_lib"}},
	}, nil, 0)

	assert.Equal(t, []string{"gdal", "widget-tools"}, c.Ports(),
		"a port declaring the edge under more than one key is not build-only")
	listed := reasonFor(t, c.Excluded, "widget-docs")
	assert.False(t, listed.Proposed)
	assert.Equal(t, "depends_build only: it links nothing this change publishes", listed.Reason)
	assert.Equal(t, "doc/widget-docs", listed.Portdir, "listed, with where it lives, so a human can go and look")
	// The reason quotes the fields a reviewer would grep for.
	assert.Equal(t, "depends_build, depends_lib", reasonFor(t, c.Members, "widget-tools").Reason)
}

func TestTheExclusionsAreByNameAndByFactsTheIndexCarries(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		lib("gdal", "gis/gdal"),
		// A subport of the portdir this change is already editing. Six of
		// gdal's own 82 dependents live in gis/gdal, and a cohort that
		// staged them would plan an edit to the Portfile it just changed.
		lib("libwidget-devel", "devel/libwidget"),
		// replaced_by is the only obsolescence marker a real PortIndex
		// carries: there is no obsolete category and no obsolete field,
		// measured over the whole tree, so a filter written against one
		// would exclude nobody while claiming to have applied a filter.
		{Port: "djview-qt5", Portdir: "aqua/djview", Keys: []string{"depends_lib"}, ReplacedBy: "djview"},
		{Port: "brokenport", Portdir: "devel/brokenport", Keys: []string{"depends_lib"}, KnownFail: true},
		{Port: "qgis", Portdir: "gis/qgis", Keys: []string{"depends_lib"}, InFlight: "dockhand/qgis-3.40"},
	}, nil, 0)

	assert.Equal(t, []string{"gdal"}, c.Ports())
	assert.Equal(t, "it lives in devel/libwidget, the portdir this change already edits",
		reasonFor(t, c.Excluded, "libwidget-devel").Reason)
	assert.Equal(t, "replaced by djview", reasonFor(t, c.Excluded, "djview-qt5").Reason)
	assert.Contains(t, reasonFor(t, c.Excluded, "brokenport").Reason, "known_fail")
	assert.Equal(t, "already in flight on dockhand/qgis-3.40",
		reasonFor(t, c.Excluded, "qgis").Reason)
	for _, cand := range c.Excluded {
		assert.False(t, cand.Proposed, "%s is examined and left out", cand.Port)
	}
}

func TestNomaintainerAnnotatesAndNeverExcludes(t *testing.T) {
	// Better than a third of a real tree declares nomaintainer. An
	// unmaintained dependent still breaks; what the reviewer needs to
	// know is that there is nobody to ask about it.
	c := Propose(abiChanged(), nil, []Dependent{
		{Port: "orphan", Portdir: "devel/orphan", Keys: []string{"depends_lib"}, Nomaintainer: true},
	}, nil, 0)

	assert.Equal(t, []string{"orphan"}, c.Ports())
	assert.Equal(t, "depends_lib; nomaintainer", reasonFor(t, c.Members, "orphan").Reason)
}

func TestSubportsAreSeparateBuildsInOnePortdir(t *testing.T) {
	// judy's seven php subports all report php/php-Judy, and gdal's own
	// portdir holds six of its dependents. The cap counts BUILDS, so
	// each subport is a member; collapsing them to one edit is the
	// planner's job and not this one's.
	c := Propose(abiChanged(), nil, []Dependent{
		lib("php83-Judy", "php/php-Judy"),
		lib("php84-Judy", "php/php-Judy"),
	}, nil, 0)

	assert.Equal(t, []string{"php83-Judy", "php84-Judy"}, c.Ports())
	for _, m := range c.Members {
		assert.Equal(t, "php/php-Judy", m.Portdir, "a subport is staged at its parent's directory")
	}
}

func TestTheCapMakesTheRestASecondCohort(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		lib("aaa", "a/aaa"), lib("bbb", "b/bbb"), lib("ccc", "c/ccc"), lib("ddd", "d/ddd"),
	}, nil, 2)

	assert.Equal(t, []string{"aaa", "bbb"}, c.Ports())
	assert.Equal(t, []string{"ccc", "ddd"}, ports(c.Deferred))
	for _, d := range c.Deferred {
		assert.False(t, d.Proposed, "this proposal does not put it forward")
		assert.Contains(t, d.Reason, "beyond the cohort cap of 2")
		assert.Contains(t, d.Reason, "a second cohort")
	}
	// Named rather than dropped: a member silently cut is a dependent
	// left broken with nothing said about it.
	assert.Equal(t, []string{"aaa", "bbb", "ccc", "ddd"}, ports(c.Candidates()))
}

func TestNothingIsProposedUnlessTheMeasurementSaysSomethingMoved(t *testing.T) {
	deps := []Dependent{lib("gdal", "gis/gdal"), lib("proj", "gis/proj")}

	t.Run("unchanged", func(t *testing.T) {
		// The up-front cohort refuted by measurement. Nothing moved, so
		// nothing is proposed — and the PR body has to state it.
		c := Propose(abiUnchanged(), nil, deps, nil, 0)
		assert.False(t, c.Proposes())
		assert.Empty(t, c.Candidates())
		assert.Contains(t, c.Declined, "no dependent needs a revision bump on this evidence")
	})

	t.Run("unavailable", func(t *testing.T) {
		// A check that could not be made is never read as one that found
		// nothing: an absent baseline compares as every library removed.
		a := abi.Delta(abi.Input{Port: "libwidget", Portdir: "devel/libwidget"})
		c := Propose(a, nil, deps, nil, 0)
		assert.False(t, c.Proposes())
		assert.Contains(t, c.Declined, "cannot describe an installation")
		assert.Equal(t, a.Why, c.Criterion)
	})

	t.Run("a comment that asks anyway is surfaced, not obeyed", func(t *testing.T) {
		// The refusal-not-guess case the walkthrough names: an
		// instruction comment is present and the measurement disagrees
		// with it. The comment is its own finding, and a human decides.
		c := Propose(abiUnchanged(),
			[]Instruction{{Source: "devel/libwidget/Portfile",
				Quote: "# Please increase the revision of gdal whenever libwidget's version is updated.",
				Ports: []string{"gdal"}}}, deps, nil, 0)

		assert.False(t, c.Proposes())
		assert.Contains(t, c.Declined, "the comment in devel/libwidget/Portfile asks for one anyway")
		assert.Contains(t, c.Declined, "for a human to weigh")
	})
}

func TestNoDependentsProposesNothingAndSaysSo(t *testing.T) {
	c := Propose(abiChanged(), nil, nil, nil, 0)
	assert.False(t, c.Proposes())
	assert.Equal(t, "no port in the index declares a dependency on libwidget", c.Declined)
	_, ok := c.Finding()
	assert.False(t, ok, "there is no proposal to append")
}

func TestEveryDependentExcludedIsSaidRatherThanLeftEmpty(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		{Port: "djview-qt5", Portdir: "aqua/djview", Keys: []string{"depends_lib"}, ReplacedBy: "djview"},
	}, nil, 0)

	assert.False(t, c.Proposes())
	assert.Equal(t, "every declared dependent of libwidget was excluded by name", c.Declined)
	assert.Equal(t, []string{"djview-qt5"}, ports(c.Candidates()), "what was examined is still recorded")
}

func TestAnInstructionLiftsABuildOnlyDependentAndNamesItsSource(t *testing.T) {
	// The maintainer's comment says something the dependency fields do
	// not, and the measurement corroborates it. The four hard exclusions
	// are not lifted this way: a replaced port is a fact about the tree
	// that naming it in a comment does not change.
	quotes := []Instruction{{
		Source: "devel/libwidget/Portfile",
		Quote:  "# Please increase the revision of gdal and widget-docs when updating",
		Ports:  []string{"gdal", "widget-docs", "djview-qt5"},
	}}
	c := Propose(abiChanged(), quotes, []Dependent{
		lib("gdal", "gis/gdal"),
		{Port: "widget-docs", Portdir: "doc/widget-docs", Keys: []string{"depends_build"}},
		{Port: "djview-qt5", Portdir: "aqua/djview", Keys: []string{"depends_lib"}, ReplacedBy: "djview"},
	}, nil, 0)

	assert.Equal(t, []string{"gdal", "widget-docs"}, c.Ports())
	assert.Equal(t, "depends_build only, and named by the comment in devel/libwidget/Portfile",
		reasonFor(t, c.Members, "widget-docs").Reason)
	assert.Equal(t, "depends_lib; named by the comment in devel/libwidget/Portfile",
		reasonFor(t, c.Members, "gdal").Reason)
	assert.Equal(t, "replaced by djview", reasonFor(t, c.Excluded, "djview-qt5").Reason)
}

func TestAnInstructionNamingAPortNothingDeclaresDeclinesByName(t *testing.T) {
	// openssl3's own comment names `openssl (to rebuild the shim
	// links)`, which need not be a declared dependent. There is no
	// portdir for it here, and a member with no portdir is a plan that
	// edits nothing — so it is named for a human rather than invented
	// into the cohort.
	c := Propose(abiChanged(), []Instruction{{
		Source: "devel/libwidget/Portfile",
		Quote:  "# Please revbump these ports when updating the libwidget version",
		Ports:  []string{"widget-shim"},
	}}, []Dependent{lib("gdal", "gis/gdal")}, nil, 0)

	assert.Equal(t, []string{"gdal"}, c.Ports())
	said := reasonFor(t, c.Excluded, "widget-shim")
	assert.Empty(t, said.Portdir, "nothing here knows where it lives")
	assert.Contains(t, said.Reason, "nothing in the index declares it a dependent here")
	assert.Contains(t, said.Reason, "by hand")
}

func TestACommentNamedPortIsRecordedInTheCommentsOwnSpelling(t *testing.T) {
	// Matching folds case, because the tree is inconsistent about it —
	// real Portfiles declare port:speexDSP where the port is speexdsp.
	// What is RECORDED must not fold: the row for a port nothing declares
	// is what a human searches the tree for by hand, and the folded key
	// is a spelling the tree does not use.
	c := Propose(abiChanged(), []Instruction{{
		Source: "devel/libwidget/Portfile",
		Quote:  "# Please revbump MoltenVK and GDAL when updating libwidget",
		Ports:  []string{"MoltenVK", "GDAL"},
	}}, []Dependent{lib("gdal", "gis/gdal")}, nil, 0)

	// GDAL folds onto the declared gdal, and the member keeps the index's
	// spelling, because that is the port a plan stages and builds.
	assert.Equal(t, []string{"gdal"}, c.Ports())
	assert.Contains(t, reasonFor(t, c.Members, "gdal").Reason,
		"named by the comment in devel/libwidget/Portfile")
	assert.Contains(t, reasonFor(t, c.Excluded, "MoltenVK").Reason,
		"nothing in the index declares it a dependent here")
}

func TestTheCapNeverDefersSomethingAMemberWaitsOn(t *testing.T) {
	// The cap cuts the dependency order at a point, so what it keeps is
	// closed under "needs": qgis waits on both and is the one held back.
	// Cutting the other way would propose a cohort whose first build
	// waits on a second cohort nobody has scheduled.
	c := Propose(abiChanged(), nil, []Dependent{
		lib("qgis", "gis/qgis", "gdal", "proj"),
		lib("gdal", "gis/gdal", "proj"),
		lib("proj", "gis/proj"),
	}, nil, 2)

	assert.Equal(t, []string{"proj", "gdal"}, c.Ports())
	assert.Equal(t, []string{"qgis"}, ports(c.Deferred))
}

func TestAnInstructionThatNamesNoPortAddsNobody(t *testing.T) {
	// The unnamed form — icu's "increase the revision number of the
	// dependents whenever the library version number changes" — is a
	// criterion and not a roster. Reading it as one would auto-include
	// every dependent there is off a single sentence.
	c := Propose(abiChanged(), []Instruction{{
		Source: "devel/libwidget/Portfile",
		Quote:  "# Please increase the revision number of the dependents whenever the library version changes.",
	}}, []Dependent{
		lib("gdal", "gis/gdal"),
		{Port: "widget-docs", Portdir: "doc/widget-docs", Keys: []string{"depends_build"}},
	}, nil, 0)

	assert.Equal(t, []string{"gdal"}, c.Ports(), "the build-only dependent stays listed")
}

func TestTheFindingCarriesTheProposalAndEveryPortExamined(t *testing.T) {
	a := abiChanged()
	c := Propose(a, nil, []Dependent{
		lib("proj", "gis/proj"),
		lib("gdal", "gis/gdal", "proj"),
		{Port: "widget-docs", Portdir: "doc/widget-docs", Keys: []string{"depends_build"}},
	}, nil, 0)

	f, ok := c.Finding()
	require.True(t, ok)
	assert.Equal(t, record.KindABIDependents, f.Kind)
	assert.Equal(t, []string{"proj", "gdal"}, f.Ports, "in the order the guest must build them")
	// The proposal restates the measurement verbatim, so the commit body
	// and the pull request carry one sentence rather than two
	// paraphrases of it.
	assert.Equal(t, a.Why, f.Criterion)
	// This is the one finding that carries Proposed: it is the question
	// a human answers by running the cohort verb or by dismissing it,
	// and the machine gate holds publication until they have.
	assert.Equal(t, record.Proposed, f.Disposition)
	assert.True(t, f.At.IsZero(), "a judgment has no clock; the caller stamps it")

	assert.Equal(t, []string{"proj", "gdal", "widget-docs"}, ports(f.Candidates))
	assert.True(t, f.Candidates[0].Proposed)
	assert.False(t, f.Candidates[2].Proposed)
	for _, cand := range f.Candidates {
		assert.NotEmpty(t, cand.Reason, "%s was examined and the reason is recorded", cand.Port)
	}
}

// A dependent this change already carries is excluded by name, and it
// is a different exclusion from another branch carrying it.
//
// The settlement that measures a cohort is the COHORT'S OWN
// verification: the members are subjects of the record being settled,
// so the pass re-measures and reads the same dependents back. Without
// this, a second proposal for ports this very commit has already
// revbumped is one changed exclusion away — the identity two findings
// merge under hides it only while the proposed set matches exactly.
func TestAPortThisChangeAlreadyCarriesIsNotProposedAgain(t *testing.T) {
	carried := lib("gdal", "gis/gdal")
	carried.Carried = true
	c := Propose(abiChanged(), nil, []Dependent{
		carried,
		lib("grass", "gis/grass"),
	}, nil, 0)

	assert.Equal(t, []string{"grass"}, ports(c.Members))
	assert.Equal(t, "this change already carries it", reasonFor(t, c.Excluded, "gdal").Reason)
	assert.NotContains(t, reasonFor(t, c.Excluded, "gdal").Reason, "in flight",
		"a port this commit revbumped is not a port somebody else is doing")
}

// A dependency field the index could not read is stated beside the
// exclusions, because it is what makes the list possibly short.
func TestAnUnreadableDependencyFieldIsNamedRatherThanDropped(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{lib("gdal", "gis/gdal")},
		[]Unread{{Port: "mercurial", Portdir: "devel/mercurial", Field: "depends_build"}}, 0)

	assert.Equal(t, []string{"gdal"}, ports(c.Members), "it is not a dependent and is never proposed")
	row := reasonFor(t, c.Excluded, "mercurial")
	assert.Equal(t, "devel/mercurial", row.Portdir)
	assert.False(t, row.Proposed)
	assert.Contains(t, row.Reason, "depends_build field could not be read as a list")
	assert.Contains(t, row.Reason, "whether it depends on libwidget is unknown")
}

// A reading that skipped files may not decline as though it had read
// them all.
func TestAMixedReadingSaysHowMuchItCouldCompare(t *testing.T) {
	// One library compared and unchanged, one file declined: a universal
	// binary whose slices announce different install names.
	m := widget("2.4.1_0", "/opt/local/lib/libwidget.2.dylib", "2.0.0")
	m.Dylibs = append(m.Dylibs,
		artifact.Dylib{Path: "/opt/local/lib/libfat.dylib", Arch: "x86_64",
			InstallName: "/opt/local/lib/libfat.2.dylib", CompatVersion: "2.0.0"},
		artifact.Dylib{Path: "/opt/local/lib/libfat.dylib", Arch: "arm64",
			InstallName: "/opt/local/lib/libfat.3.dylib", CompatVersion: "3.0.0"})
	a := delta(m, m)
	require.Equal(t, abi.Unchanged, a.Verdict, "something was compared, so this is still a reading")
	require.Len(t, a.Unmeasured(), 2, "one per side")

	c := Propose(a, nil, []Dependent{lib("gdal", "gis/gdal")}, nil, 0)

	assert.False(t, c.Proposes())
	assert.Contains(t, c.Declined, "nothing that could be compared moved")
	assert.Contains(t, c.Declined, "2 files were not measured")
	assert.Contains(t, c.Declined, "/opt/local/lib/libfat.dylib")
	assert.NotContains(t, c.Declined, "nothing libwidget publishes moved",
		"the plain sentence claims more than the measurement covered")
}

// conflicting is a dependent that declares it cannot be installed
// beside the named ports.
func conflicting(port, portdir string, conflicts ...string) Dependent {
	d := lib(port, portdir, "libwidget")
	d.Conflicts = conflicts
	return d
}

// solos reads back the members the cohort bumps but will not build.
func solos(all []record.Candidate) []string {
	out := []string{}
	for _, c := range all {
		if c.Solo {
			out = append(out, c.Port)
		}
	}
	return out
}

// over reads back, for each withheld member, the seated sibling it
// lost to — the name the engine deactivates if a person forces the
// member in. A member that keeps its seat names nobody.
func over(all []record.Candidate) map[string]string {
	out := map[string]string{}
	for _, c := range all {
		if c.Solo || c.Over != "" {
			out[c.Port] = c.Over
		}
	}
	return out
}

// Two members that MacPorts will not activate together cannot share a
// guest. Both are still bumped — each links the library that moved —
// and the development twin is the one that gives up its seat.
func TestACohortDoesNotStageTwoMembersThatConflict(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		conflicting("gegl", "graphics/gegl", "gegl-devel"),
		conflicting("gegl-devel", "graphics/gegl-devel", "gegl"),
		lib("gthumb", "gnome/gthumb", "libwidget"),
	}, nil, 0)

	assert.Equal(t, []string{"gegl", "gegl-devel", "gthumb"}, ports(c.Members),
		"every member is bumped: a conflict constrains one guest, not what the tree is owed")
	assert.Equal(t, []string{"gegl-devel"}, solos(c.Members),
		"the -devel twin gives up the seat")
	assert.Equal(t, map[string]string{"gegl-devel": "gegl"}, over(c.Members),
		"and it names the sibling it lost to, on its own key and not only in the sentence")
	for _, m := range c.Members {
		assert.True(t, m.Proposed, "%s must stay proposed — its revision is owed either way", m.Port)
	}
}

// The suffix decides regardless of which one the index happened to list
// first: an outcome that depended on ordering here would be a coin toss
// wearing a rule's clothes.
func TestTheDevelTwinLosesTheSeatFromEitherOrder(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		conflicting("gegl-devel", "graphics/gegl-devel", "gegl"),
		conflicting("gegl", "graphics/gegl", "gegl-devel"),
	}, nil, 0)
	assert.Equal(t, []string{"gegl-devel"}, solos(c.Members))
	assert.Equal(t, map[string]string{"gegl-devel": "gegl"}, over(c.Members))
}

// A conflict is symmetric in MacPorts and both halves are usually
// written, but a cohort must not need both to be.
func TestOneSidedConflictDeclarationIsStillHonoured(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		lib("libheif", "multimedia/libheif", "libwidget"),
		conflicting("libheif-devel", "multimedia/libheif-devel", "libheif"),
	}, nil, 0)
	assert.Equal(t, []string{"libheif-devel"}, solos(c.Members))
	assert.Equal(t, map[string]string{"libheif-devel": "libheif"}, over(c.Members),
		"the sibling is named whichever side wrote the declaration")
}

// Where the suffix does not tell them apart, build order decides and
// the member already seated keeps it.
func TestWithoutADevelSuffixTheSeatGoesByBuildOrder(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		conflicting("mbedtls", "devel/mbedtls", "mbedtls3"),
		conflicting("mbedtls3", "devel/mbedtls3", "mbedtls"),
	}, nil, 0)
	assert.Equal(t, []string{"mbedtls3"}, solos(c.Members),
		"the one already in the build keeps the seat")
	assert.Equal(t, map[string]string{"mbedtls3": "mbedtls"}, over(c.Members))
}

// Ports that conflict with something outside the cohort constrain
// nothing: the guest is only ever asked to hold the members.
func TestAConflictWithAStrangerDoesNotCostASeat(t *testing.T) {
	c := Propose(abiChanged(), nil, []Dependent{
		conflicting("gegl", "graphics/gegl", "some-port-nobody-proposed"),
		lib("gthumb", "gnome/gthumb", "libwidget"),
	}, nil, 0)
	assert.Empty(t, solos(c.Members), "no member of this cohort declares that conflict")
	assert.Empty(t, over(c.Members), "and nobody lost a seat to anybody")
}

// The field name a reason quotes is the index's own, and this is what
// holds the two spellings together.
//
// It is declared here because it belongs to a sentence a person reads,
// and the index owns the vocabulary; an index that renamed its field
// would leave every candidate row quoting a field the tree no longer
// has, which is a decision no reader could check.
func TestTheQuotedFieldNameIsTheIndexsOwn(t *testing.T) {
	assert.Equal(t, portindex.DependsBuild, DependsBuild)
}

// The row translation is this package's, and it folds in the two facts
// the index cannot carry.
//
// It moved here from the settlement that used to write it inline, and
// the reason is the ruling: the package that knows what a Dependent
// needs is the one that should build one, or every caller holding the
// two extra facts writes the loop again and one of them forgets a
// field.
func TestFromFoldsTheIndexRowsTogetherWithWhatDockhandKnows(t *testing.T) {
	rows := []portindex.Dependent{
		{Name: "gdal", Portdir: "gis/gdal", Keys: []string{"depends_lib"},
			Requires: []string{"proj"}, Conflicts: []string{"gdal-devel"}},
		{Name: "speexDSP", Portdir: "audio/speexdsp", Keys: []string{"depends_lib"},
			ReplacedBy: "speex", KnownFail: true, Nomaintainer: true},
	}
	deps, short := From(rows,
		[]portindex.Unread{{Port: "mercurial", Portdir: "devel/mercurial", Field: "depends_build"}},
		map[string]string{"gdal": "dockhand/gdal-3.11"},
		map[string]bool{"speexdsp": true})

	require.Len(t, deps, 2)
	// Every index fact crosses, including the two a cohort needs to
	// order and to seat its members.
	assert.Equal(t, Dependent{
		Port: "gdal", Portdir: "gis/gdal", Keys: []string{"depends_lib"},
		Requires: []string{"proj"}, Conflicts: []string{"gdal-devel"},
		InFlight: "dockhand/gdal-3.11",
	}, deps[0])

	// The maps are keyed folded and the row is not: speexDSP is how the
	// tree spells it, "speexdsp" is how a lookup finds it, and what is
	// recorded is the spelling a person searches for.
	assert.Equal(t, "speexDSP", deps[1].Port)
	assert.True(t, deps[1].Carried, "a folded key still finds the row it is about")
	assert.True(t, deps[1].KnownFail)
	assert.True(t, deps[1].Nomaintainer)
	assert.Equal(t, "speex", deps[1].ReplacedBy)
	assert.Empty(t, deps[1].InFlight, "no branch is carrying it")

	assert.Equal(t, []Unread{{Port: "mercurial", Portdir: "devel/mercurial", Field: "depends_build"}}, short,
		"the unreadable fields cross unchanged: they are not dependents and are never proposed")

	deps, short = From(nil, nil, nil, nil)
	assert.Empty(t, deps)
	assert.Empty(t, short)
}
