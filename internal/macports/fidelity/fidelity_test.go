package fidelity

import (
	"maps"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

func snapshot(ports map[string]macports.PortInfo) macports.Snapshot {
	return macports.Snapshot{Target: model.Target{Name: "main", Portfile: "devel/main/Portfile"}, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Ports: ports}
}

func TestEquivalentNormalizesRelocatedWorkspaces(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/before/devel/main/files"}}})
	before.Root = "/before"
	after := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/after/devel/main/files"}}})
	after.Root = "/after"
	require.NoError(t, Equivalent(before, after), "paths under different roots compare equal, each normalized by its own")
	elsewhere := after
	elsewhere.Root = "/elsewhere"
	require.ErrorIs(t, Equivalent(before, elsewhere), ErrMismatch, "a snapshot claiming another root shows as a changed option")
	require.Equal(t, "/before/devel/main/files", before.Ports["main"].Options["filespath"], "comparison must not mutate observations")
	changed := snapshot(map[string]macports.PortInfo{"main": {Name: "main", Version: "1", Revision: 3, Options: map[string]string{"filespath": "/after/devel/main/files"}}})
	changed.Root = "/after"
	require.ErrorIs(t, Equivalent(before, changed), ErrMismatch)
	unrooted := before
	unrooted.Root = ""
	require.ErrorIs(t, Equivalent(unrooted, after), ErrMismatch, "a snapshot read back without its root keeps its paths, and they differ from a normalized one")
	require.NoError(t, Equivalent(unrooted, unrooted), "and compares equal to itself")
}

func TestRevisionAndChecksumsReportOnlyIntendedChanges(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 0, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	bumped := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 1, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	report := Revision(before, bumped, "main", nil)
	require.Equal(t, []string{"main.revision +1"}, report.ExpectedChanges)
	require.Empty(t, report.UnexpectedChanges)
	sibling := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 1, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 4, Options: map[string]string{}},
	})
	require.NotEmpty(t, Revision(before, sibling, "main", nil).UnexpectedChanges, "a sibling revision change is unexpected")
	// One that shares the literal, as observed, moves with it: here child's
	// revision line is the edited one, though it stood at another number.
	require.Empty(t, Revision(before, sibling, "main", []string{"child"}).UnexpectedChanges)
	sentinel := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 7919, Options: map[string]string{"checksums": "sha256 aaaa"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	require.Equal(t, []string{"main"}, Sharing(before, sentinel, func(old, next macports.PortInfo) bool { return old.Revision != next.Revision }))

	refreshed := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 0, Options: map[string]string{"checksums": "sha256 bbbb"}},
		"child": {Name: "child", Version: "2", Revision: 3, Options: map[string]string{}},
	})
	require.Empty(t, Checksums(before, refreshed, "main", "sha256 bbbb").UnexpectedChanges)
	require.NotEmpty(t, Checksums(before, refreshed, "main", "sha256 cccc").UnexpectedChanges, "checksums must match the intended values")

	// A python stub and its subports share one declaration: refreshing the
	// selected subport's refreshes the stub's (the py-flatbuffers run's
	// finding 1). A sibling left behind, or one of another version, isn't
	// the selected port's to change.
	family := func(stub, other string) macports.Snapshot {
		return snapshot(map[string]macports.PortInfo{
			"py313-demo": {Name: "py313-demo", Version: "1", Options: map[string]string{"checksums": "sha256 bbbb"}},
			"py-demo":    {Name: "py-demo", Version: "1", Options: map[string]string{"checksums": stub}},
			"py312-demo": {Name: "py312-demo", Version: "0.9", Options: map[string]string{"checksums": other}},
		})
	}
	shared := snapshot(map[string]macports.PortInfo{
		"py313-demo": {Name: "py313-demo", Version: "1", Options: map[string]string{"checksums": "sha256 aaaa"}},
		"py-demo":    {Name: "py-demo", Version: "1", Options: map[string]string{"checksums": "sha256 aaaa"}},
		"py312-demo": {Name: "py312-demo", Version: "0.9", Options: map[string]string{"checksums": "sha256 aaaa"}},
	})
	moved := Checksums(shared, family("sha256 bbbb", "sha256 aaaa"), "py313-demo", "sha256 bbbb")
	require.Empty(t, moved.UnexpectedChanges)
	require.Equal(t, []string{"py313-demo.checksums", "py-demo.checksums, shared"}, moved.ExpectedChanges)
	require.Empty(t, Checksums(shared, family("sha256 aaaa", "sha256 aaaa"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "a sibling keeping its own declaration is left as it was")
	require.NotEmpty(t, Checksums(shared, family("sha256 bbbb", "sha256 bbbb"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "another version's aren't shared")
	require.NotEmpty(t, Checksums(shared, family("sha256 cccc", "sha256 aaaa"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "nor are other checksums")
	apart := snapshot(map[string]macports.PortInfo{
		"py313-demo": {Name: "py313-demo", Version: "1", Options: map[string]string{"checksums": "sha256 aaaa"}},
		"py-demo":    {Name: "py-demo", Version: "1", Options: map[string]string{"checksums": "sha256 zzzz"}},
		"py312-demo": {Name: "py312-demo", Version: "0.9", Options: map[string]string{"checksums": "sha256 aaaa"}},
	})
	require.NotEmpty(t, Checksums(apart, family("sha256 bbbb", "sha256 aaaa"), "py313-demo", "sha256 bbbb").UnexpectedChanges, "a sibling with checksums of its own shares nothing")
	require.Error(t, CheckSnapshot(macports.Snapshot{}, macports.Context{}))
}

// A homepage that spells the version moves with a bump: the perl5 ports'
// metacpan release pages do, and nothing is fetched from it.
func TestVersionAllowsAHomepageThatFollowsTheVersion(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main": {Name: "main", Version: "0.21.0", Revision: 0, Options: map[string]string{"checksums": "sha256 aaaa", "homepage": "https://metacpan.org/release/List-Uniq-v0.21.0"}},
	})
	after := snapshot(map[string]macports.PortInfo{
		"main": {Name: "main", Version: "0.230.0", Revision: 0, Options: map[string]string{"checksums": "sha256 bbbb", "homepage": "https://metacpan.org/release/List-Uniq-0.23"}},
	})
	report := ScopedVersion(false, before, after, "main", model.Release{Version: "0.230.0"}, "sha256 bbbb")
	require.Empty(t, report.UnexpectedChanges)
	// Whatever else the version's evaluation moves is the version's, but a
	// dependency.
	after.Ports["main"].Options["description"] = "changed"
	report = ScopedVersion(false, before, after, "main", model.Release{Version: "0.230.0"}, "sha256 bbbb")
	require.Empty(t, report.UnexpectedChanges, "the version's evaluation is the prediction")
	moved := after.Ports["main"]
	moved.Dependencies = []macports.Dependency{{Phase: "lib", Spec: "port:other"}}
	after.Ports["main"] = moved
	require.Equal(t, []string{"main.dependencies changed"}, ScopedVersion(false, before, after, "main", model.Release{Version: "0.230.0"}, "sha256 bbbb").UnexpectedChanges)
}

// A field that moves only by the version written in it is the version's,
// as go_toolchain's long_description, "provides Go 1.26.8": go-1.26's
// update was refused as changing it (field testing at 1cc87c43).
func TestVersionAllowsAFieldThatWritesTheVersion(t *testing.T) {
	describe := func(version, extra string) macports.Snapshot {
		return snapshot(map[string]macports.PortInfo{"go-1.26": {Name: "go-1.26", Version: version, Options: map[string]string{"checksums": "sha256 " + version,
			"long_description": "This port provides Go " + version + " specifically" + extra}}})
	}
	report := ScopedVersion(false, describe("1.26.8", ""), describe("1.26.9", ""), "go-1.26", model.Release{Version: "1.26.9"}, "sha256 1.26.9")
	require.Empty(t, report.UnexpectedChanges)
	// A version other than the release's is unexpected.
	report = ScopedVersion(false, describe("1.26.8", ""), describe("1.26.10", ""), "go-1.26", model.Release{Version: "1.26.9"}, "sha256 1.26.10")
	require.Contains(t, report.UnexpectedChanges, "go-1.26.version: expected 1.26.9, got 1.26.10")
}

// A fetch is compared by its kind, which the options' compatible flag
// doesn't tell apart among the kinds a direct download repeats.
func TestAFetchIsComparedByItsKind(t *testing.T) {
	port := func(kind string) macports.PortInfo {
		return macports.PortInfo{Name: "demo", Version: "1", Options: map[string]string{"fetch.archive_compatible": "1"}, Fetch: &macports.FetchSemantics{Kind: kind}}
	}
	require.Equal(t, []string{"demo.fetch changed"}, Compare("demo", port("standard"), port("git")))
	require.Empty(t, Compare("demo", port("standard"), port("standard")))
}

// A port of the same Portfile that fetches its own source is said as the
// person sees it, and once: rust's refusal read "fidelity: … : [fidelity:
// … : rust-src has independent distfiles; shared-source preparation is
// required]" (the rust and cargo run).
func TestASiblingFetchingItsOwnSourceIsSaidOnce(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"rust":     {Name: "rust", Version: "1.98.1", Options: map[string]string{"distfiles": "rustc-1.98.1-src.tar.gz", "checksums": "sha256 aaaa"}},
		"rust-src": {Name: "rust-src", Version: "1.98.1", Options: map[string]string{"distfiles": "rust-src-1.98.1.tar.gz", "checksums": "sha256 bbbb"}},
	})
	after := snapshot(map[string]macports.PortInfo{
		"rust":     {Name: "rust", Version: "1.99.0", Options: map[string]string{"distfiles": "rustc-1.99.0-src.tar.gz", "checksums": "sha256 cccc"}},
		"rust-src": {Name: "rust-src", Version: "1.99.0", Options: map[string]string{"distfiles": "rust-src-1.99.0.tar.gz", "checksums": "sha256 dddd"}},
	})
	// With --shared-release, its own archives are the update's to fetch,
	// and its checksums to write, as xapian-core's bindings' are.
	require.Empty(t, ScopedVersion(true, before, after, "rust", model.Release{Version: "1.99.0"}, "sha256 cccc").UnexpectedChanges)
	git := after
	git.Ports = maps.Clone(after.Ports)
	src := git.Ports["rust-src"]
	src.Options = map[string]string{"git.branch": "other", "distfiles": "rust-src-1.99.0.tar.gz", "checksums": "sha256 dddd"}
	git.Ports["rust-src"] = src
	require.EqualError(t, ScopedVersion(true, before, git, "rust", model.Release{Version: "1.99.0"}, "sha256 cccc").Err(),
		"fidelity: evaluation does not match the intended change: rust-src, another port of the same Portfile, fetches its own source (its git.branch differ from rust's), which updating rust doesn't move", "one of its own Git source isn't, yet")
	// Without --shared-release, a sibling moving with the release names the
	// flag that moves both, where it named an assess command v3 hasn't.
	require.EqualError(t, ScopedVersion(false, before, after, "rust", model.Release{Version: "1.99.0"}, "sha256 cccc").Err(),
		"fidelity: evaluation does not match the intended change: rust-src, another port of the same Portfile, moves with rust's release; --shared-release moves both, and --plan shows what that changes first")
}

// What partitioning a release finds of a sibling is a finding of a kind,
// each said as the refusal was: a sibling moving, which shared
// authorization answers, and one of its own version or source, which
// nothing answers yet.
func TestAScopeFindsEachSiblingByKind(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"rust":     {Name: "rust", Version: "1.98.1", Options: map[string]string{"distfiles": "rustc-1.98.1-src.tar.gz", "checksums": "sha256 aaaa"}},
		"rust-src": {Name: "rust-src", Version: "1.98.1", Options: map[string]string{"distfiles": "rust-src-1.98.1.tar.gz", "checksums": "sha256 bbbb"}},
		"rust-doc": {Name: "rust-doc", Version: "1.98.1", Options: map[string]string{"distfiles": "rustc-1.98.1-src.tar.gz", "checksums": "sha256 aaaa"}},
	})
	after := snapshot(map[string]macports.PortInfo{
		"rust":     {Name: "rust", Version: "1.99.0", Options: map[string]string{"distfiles": "rustc-1.99.0-src.tar.gz", "checksums": "sha256 cccc"}},
		"rust-src": {Name: "rust-src", Version: "1.99.0", Options: map[string]string{"distfiles": "rust-src-1.99.0.tar.gz", "checksums": "sha256 dddd"}},
		"rust-doc": {Name: "rust-doc", Version: "1.99.1", Options: map[string]string{"distfiles": "rustc-1.99.0-src.tar.gz", "checksums": "sha256 cccc"}},
	})
	scope, findings, err := Scope(before, after, "rust")
	require.NoError(t, err)
	require.Len(t, scope.Affected, 3)
	require.Equal(t, []ScopeFinding{
		{Kind: SiblingMoves, Port: "rust-doc", Selected: "rust"},
		{Kind: SiblingOwnVersion, Port: "rust-doc", Selected: "rust"},
		{Kind: SiblingMoves, Port: "rust-src", Selected: "rust"},
		{Kind: SiblingOwnSource, Port: "rust-src", Field: "distfiles", Selected: "rust"},
	}, findings)
	require.True(t, findings[0].AnsweredBy(true))
	require.False(t, findings[0].AnsweredBy(false))
	require.False(t, findings[1].AnsweredBy(true))
	require.True(t, findings[3].AnsweredBy(true), "its own archives, at the release's version, are the update's to fetch")
	require.False(t, ScopeFinding{Kind: SiblingOwnSource, Port: "rust-src", Field: "git.branch"}.AnsweredBy(true))
}

// A path below the source directory is the same path in two evaluations,
// whatever directory MacPorts builds each in and whatever the version
// names the source directory: qemu's configure.cmd, ${worksrcpath}/
// configure, read as a change (field testing, 2026-10-02). A path below
// it that changes still does.
func TestAPathBelowTheSourceFollowsIt(t *testing.T) {
	port := func(cmd string) macports.PortInfo {
		return macports.PortInfo{Name: "qemu", Options: map[string]string{"configure.cmd": cmd}}
	}
	require.Empty(t, Compare("qemu", port("/opt/local/var/macports/build/qemu-234b5126/work/qemu-9.0.2/configure"), port("/opt/local/var/macports/build/qemu-f3e5b0c6/work/qemu-9.1.0/configure")))
	require.Equal(t, []string{"qemu.configure.cmd changed"}, Compare("qemu", port("/opt/local/var/macports/build/qemu-234b5126/work/qemu-9.0.2/configure"), port("/opt/local/var/macports/build/qemu-f3e5b0c6/work/qemu-9.1.0/build/configure")))
}

// A directory's subports are compared each for itself, every side
// normalized by its own root: one new at the revision is added, one gone
// is removed, and one whose fields moved says from what to what.
func TestSubportChangesSayWhatMovedInEach(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main":       {Name: "main", Version: "1", Options: map[string]string{"filespath": "/before/devel/main/files"}},
		"main-old":   {Name: "main-old", Version: "0.9"},
		"main-devel": {Name: "main-devel", Version: "2", Dependencies: []macports.Dependency{{Port: "zlib", Phase: "lib", Spec: "port:zlib"}}},
	})
	before.Root = "/before"
	after := snapshot(map[string]macports.PortInfo{
		"main":       {Name: "main", Version: "1", Options: map[string]string{"filespath": "/after/devel/main/files"}},
		"main-devel": {Name: "main-devel", Version: "2", Revision: 1, Dependencies: []macports.Dependency{{Port: "zstd", Phase: "lib", Spec: "port:zstd"}}},
		"main-new":   {Name: "main-new", Version: "3"},
	})
	after.Root = "/after"
	require.Equal(t, []model.SubportChange{
		{Port: "main", Kind: model.SubportUnchanged},
		{Port: "main-devel", Kind: model.SubportChanged, Fields: []model.FieldChange{
			{Field: "revision", From: "0", To: "1"},
			{Field: "dependencies", From: "lib:port:zlib", To: "lib:port:zstd"},
		}},
		{Port: "main-new", Kind: model.SubportAdded},
		{Port: "main-old", Kind: model.SubportRemoved},
	}, SubportChanges("devel/main", &before, &after, nil))
	require.Equal(t, []model.SubportChange{{Port: "main", Kind: model.SubportAdded}, {Port: "main-devel", Kind: model.SubportAdded}, {Port: "main-new", Kind: model.SubportAdded}},
		SubportChanges("devel/main", nil, &after, nil), "a directory new at the revision")

	// A subport renamed is compared with its old name's port, its name a
	// field that moved, where it read as added beside main-old removed.
	renamed := []macports.PortTransition{{Before: macports.PortSide{Name: "main-old"}, After: macports.PortSide{Name: "main-new"}, Kind: macports.TransitionRenamed}}
	require.Equal(t, []model.SubportChange{
		{Port: "main", Kind: model.SubportUnchanged},
		{Port: "main-devel", Kind: model.SubportChanged, Fields: []model.FieldChange{
			{Field: "revision", From: "0", To: "1"},
			{Field: "dependencies", From: "lib:port:zlib", To: "lib:port:zstd"},
		}},
		{Port: "main-new", Kind: model.SubportChanged, Fields: []model.FieldChange{
			{Field: "name", From: "main-old", To: "main-new"},
			{Field: "version", From: "0.9", To: "3"},
		}},
	}, SubportChanges("devel/main", &before, &after, renamed))
}

// Each subport keeps its maintainers as evaluated, the base's where the
// base has it, even where the revision changes them, and the revision's
// for one it adds, which status reads for a pull request's merge fitness
// without evaluating, the timeout waiting on the maintainers MacPorts
// has; a change of them is a field that moved.
func TestSubportChangesKeepTheirMaintainers(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main":     {Name: "main", Version: "1", Options: map[string]string{"maintainers": "@ryandesign openmaintainer"}},
		"main-old": {Name: "main-old", Version: "1", Options: map[string]string{"maintainers": "nomaintainer"}},
	})
	after := snapshot(map[string]macports.PortInfo{
		"main":     {Name: "main", Version: "1", Options: map[string]string{"maintainers": "{@herbygillot gmail.com:herby.gillot} openmaintainer"}},
		"main-new": {Name: "main-new", Version: "1", Options: map[string]string{"maintainers": "@jmroot"}},
	})
	require.Equal(t, []model.SubportChange{
		{Port: "main", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "maintainers", From: "@ryandesign openmaintainer", To: "{@herbygillot gmail.com:herby.gillot} openmaintainer"}},
			Maintainers: "@ryandesign openmaintainer"},
		{Port: "main-new", Kind: model.SubportAdded, Maintainers: "@jmroot"},
		{Port: "main-old", Kind: model.SubportRemoved, Maintainers: "nomaintainer"},
	}, SubportChanges("devel/main", &before, &after, nil))
}

// A PortGroup's file that moved between the sides is said once, before
// the ports it moved; a port that only loads it, with nothing of its own
// moved, is no change, as csvtk's go.vendors and every Go port's build.env
// moved through the golang PortGroup with no Portfile change (the survey's
// Q18).
func TestAPortGroupsChangeIsSaidBeforeThePortsItMoves(t *testing.T) {
	golang := func(blob, env string) macports.PortInfo {
		return macports.PortInfo{Name: "x", Version: "1", Options: map[string]string{
			"dockhand.portgroups": "{golang 1.0 <source>/_resources/port1.0/group/golang-1.0.tcl " + blob + "} {github 1.0 <source>/_resources/port1.0/group/github-1.0.tcl 9999}",
			"build.env":           env,
		}}
	}
	named := func(port macports.PortInfo, name string) macports.PortInfo {
		port.Name = name
		return port
	}
	before := snapshot(map[string]macports.PortInfo{"csvtk": named(golang("aaaa", "GOFLAGS="), "csvtk"), "csvtk-docs": named(golang("aaaa", ""), "csvtk-docs")})
	after := snapshot(map[string]macports.PortInfo{"csvtk": named(golang("bbbb", "GOFLAGS=-modcacherw"), "csvtk"), "csvtk-docs": named(golang("bbbb", ""), "csvtk-docs")})
	changes := SubportChanges("devel/csvtk", &before, &after, nil)
	require.Equal(t, []model.SubportChange{
		{Port: "csvtk", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "build.env", From: "GOFLAGS=", To: "GOFLAGS=-modcacherw"}}},
		{Port: "csvtk-docs", Kind: model.SubportUnchanged},
	}, changes, "the PortGroup's blob is no field of a port's")
	resources := ResourceChanges(changes, &before, &after)
	require.Equal(t, []model.ResourceChange{{File: "_resources/port1.0/group/golang-1.0.tcl", From: "aaaa", To: "bbbb", Ports: []string{"csvtk"}}}, resources)
	require.Equal(t, "_resources/port1.0/group/golang-1.0.tcl changed, moving csvtk", resources[0].Line())
	require.Empty(t, ResourceChanges(changes, &before, &before), "the same files say nothing")
	require.Empty(t, ResourceChanges(nil, nil, &after), "nor a directory new at the revision")
}

// A port's code is compared part by part: a hook whose body moved is said
// by its name, one an option known to move without an edit is said as
// unstable, and neither is a change ComparablePort keeps, so an edit's
// fidelity never reads them.
func TestAPortsCodeIsComparedPartByPart(t *testing.T) {
	code := func(hook, epoch string) macports.PortInfo {
		return macports.PortInfo{Name: "main", Version: "1", Options: map[string]string{"dockhand.code": "procedures aa userproc-post-org.macports.destroot-destroot-0 " + hook + " unstable:source_date_epoch " + epoch}}
	}
	before, after := snapshot(map[string]macports.PortInfo{"main": code("11", "1")}), snapshot(map[string]macports.PortInfo{"main": code("22", "2")})
	require.Equal(t, []model.SubportChange{{Port: "main", Kind: model.SubportChanged,
		Fields:   []model.FieldChange{{Field: "code userproc-post-org.macports.destroot-destroot-0", From: "11", To: "22"}},
		Unstable: []model.FieldChange{{Field: "code unstable:source_date_epoch", From: "1", To: "2"}},
	}}, SubportChanges("devel/main", &before, &after, nil))
	moved := snapshot(map[string]macports.PortInfo{"main": code("11", "2")})
	require.Equal(t, []model.SubportChange{{Port: "main", Kind: model.SubportUnchanged,
		Unstable: []model.FieldChange{{Field: "code unstable:source_date_epoch", From: "1", To: "2"}},
	}}, SubportChanges("devel/main", &before, &moved, nil), "a part that moves without an edit is no change")
	require.NoError(t, Equivalent(before, after), "an edit's fidelity doesn't read the port's code")
}

// Two revisions' records of a directory confirm an inert change where
// only inert options and the checksum of every option's value moved
// between them, and refuse where a record names another field, one
// subport is added in one alone, or a record couldn't be made.
func TestInertBetweenConfirmsOnlyInertFieldsMoved(t *testing.T) {
	t.Parallel()
	record := func(ports ...model.SubportChange) model.ChangeRecord {
		return model.ChangeRecord{Directory: "textproc/omnidiff", Ports: ports, Policy: ChangePolicy}
	}
	changed := func(fields ...model.FieldChange) model.SubportChange {
		return model.SubportChange{Port: "omnidiff", Kind: model.SubportChanged, Fields: fields}
	}
	version := model.FieldChange{Field: "version", From: "1.1.0", To: "1.2.0"}
	then := record(changed(version, model.FieldChange{Field: "maintainers", From: "a", To: "a"}))
	now := record(changed(version, model.FieldChange{Field: "maintainers", From: "a", To: "a openmaintainer"}, model.FieldChange{Field: "code options", From: "1", To: "2"}))
	require.True(t, InertBetween(then, now), "the same version, the maintainers and options' checksum moved")
	require.True(t, InertBetween(record(model.SubportChange{Port: "omnidiff", Kind: model.SubportAdded}), record(model.SubportChange{Port: "omnidiff", Kind: model.SubportAdded})), "a port both add")
	require.False(t, InertBetween(then, record(changed(model.FieldChange{Field: "version", From: "1.1.0", To: "1.3.0"}))), "the version moved")
	require.False(t, InertBetween(then, record(changed(version, model.FieldChange{Field: "license", From: "MIT", To: "BSD"}))), "license isn't inert")
	require.False(t, InertBetween(then, record(changed(version, model.FieldChange{Field: "code procedures", From: "1", To: "2"}))), "a procedure moved")
	require.False(t, InertBetween(then, record(changed())), "the version moved back to the base's")
	require.False(t, InertBetween(then, record(changed(version), model.SubportChange{Port: "omnidiff-lite", Kind: model.SubportAdded})), "a subport added")
	require.False(t, InertBetween(record(model.SubportChange{Port: "omnidiff", Kind: model.SubportAdded}), record(changed(version))), "added in one alone")
	failed := now
	failed.Problem = "the revision couldn't be evaluated"
	require.False(t, InertBetween(then, failed), "an unreadable record")
	older := now
	older.Policy = ChangePolicy - 1
	require.False(t, InertBetween(then, older), "another policy's record")
}
