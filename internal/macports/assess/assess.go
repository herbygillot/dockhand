// Package assess decides what an upstream change means for a MacPorts
// port (the assessment design, C): from two versions' readings, what
// sourcecompare says changed between them, and the port as MacPorts
// evaluates it at each end, which findings ask a person's look before a
// submission nobody reviews, and what the assessment read and set apart.
//
// It is pure, as planning is: it reads, fetches, and evaluates nothing
// itself. What it needs observed, such as the version MacPorts has of a
// port that provides a Python requirement, it names (Wanted), and its
// caller observes and gives back.
package assess

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
)

// Policy is the version of the rules an assessment is made under, and of
// what the engine gives them to compare: raising it keeps an assessment
// made before from standing for one made now, without reading anything
// again (the assessment design, C). 2: a port that declares its crates or
// Go modules is compared through its own archives, not refused, and a
// crate gone that linked a native library the port still has something
// for is said. 3: batch 13's, a Node project's workspaces read, a Python
// project's requires-python judged, and a Python pin behind the
// PortGroup's default noted. 4: that note read as MacPorts' PortGroup
// computes its default, and a setup file its project's backend doesn't
// read set apart. 5: a Node package's dependencies counted, as a Rust
// crate's are, not held (D9). 6: patches checked against the candidate's
// source, its own and those it drops. 7: license text moved between files
// said once, holding nothing, and a subport's archives planned for it.
// 8: a vendored port's patches read in the tree, so one with patches of
// its own is compared, where rust's were "not compared" (the rust and
// cargo run). 9: archives of any size downloaded, and read to 4 GiB
// uncompressed, a zip's under the same bound, where rust's source was past
// both (the person's word, 2026-10-01). 10: a Go the Portfile pins, by
// go.bin or a dependency on go-1.NN, older than go.mod requires holds,
// where only go.toolchain_min was judged and trivy's go-1.26 under 0.75.0's
// Go 1.27.0 read as gated on (the trivy run, #35083).
// 11: a CMakeLists.txt that only adds options, and what one off by
// default gates, holds nothing (D12, revisited 2026-10-01). 12: what any
// option off by default gates, which neither the file nor the Portfile
// turns on, holds nothing, nor do comments (the same). 13: a new port's
// license said with its Portfile's line, holding only where its manifest
// declares another, and its build files, all new, not said (batch 28).
// 14: an update's assessment checks its patches, as a revision's does, so
// one kept from an update without them is made again (batch 32).
const Policy = 14

// Input is what one port's assessment reads.
type Input struct {
	// Port is the port as the candidate evaluates it, and Base as the base
	// does.
	Port, Base macports.PortInfo
	// Pairs are each archive the candidate fetches beside the base's it
	// replaces, as read.
	Pairs    []Pair
	Versions sourcecompare.Versions
	// Portfile is the candidate's Portfile as written, for the build
	// options it names, in any variant, which it may set: a CMake option
	// off by default gates what the default build doesn't reach only
	// where the Portfile doesn't name it (D12). Nil where it wasn't read,
	// which takes every option as one it may set.
	Portfile []byte
	// Toolchain is what the candidate's go.mod requires of a module-mode
	// Go port, where it was read, and what an edit did about it.
	Toolchain *Toolchain
	// Observed are the observations Wanted named, as the caller made them.
	Observed map[Provider]Observation
	// Patches are the candidate's patches and those the base applied that
	// it doesn't, each checked against the candidate's archives; none
	// where neither declares any, or the archives weren't fetched.
	Patches []Patch
	// New is a port the base doesn't have: its archives are read against
	// nothing, so every file is new to it.
	New bool
}

// Pair is one archive the candidate fetches, read, beside the base's it
// replaces, read, in the context that fetches them: Port is the port as
// that context evaluates the candidate, whose zero value is the input's
// own.
type Pair struct {
	Archive       string
	Before, After project.Reading
	Port          macports.PortInfo
}

// Provider is a port whose version an assessment needs, in the base's
// tree or the candidate's.
type Provider struct {
	Port string
	Base bool
}

// Observation is a port's version as the caller observed it, and its
// directory; or that the tree has no such port (Absent); or why it
// couldn't tell.
type Observation struct {
	Version   string
	Directory string
	Absent    bool
	Problem   string
}

// Rules are what raised a finding: with its path and subject, its
// identity.
const (
	LicenseChanged      = "license-changed"
	LicenseYears        = "license-years"
	LicenseMoved        = "license-moved"
	BuildFileChanged    = "build-file-changed"
	BuildFileVersion    = "build-file-version"
	BuildFileOptions    = "build-file-options"
	DependencyAdded     = "dependency-added"
	DependencyDropped   = "dependency-dropped"
	DependencyMoved     = "dependency-moved"
	DependenciesCounted = "dependencies-counted"
	NativeLibrary       = "native-library"
	NativeLibraryLeft   = "native-library-left"
	Unread              = "unread"
	LayoutAmbiguous     = "layout-ambiguous"
	RequirementUnmet    = "python-requirement-unmet"
	RequirementUnknown  = "python-requirement-unknown"
	ProviderUnresolved  = "python-provider-unresolved"
	ProviderRemoved     = "python-provider-removed"
	GoToolchainRule     = "go-toolchain"
	RequiresPythonRule  = "python-requires"
	PythonPinBehind     = "python-pin-behind"
	PatchRejected       = "patch-rejected"
	PatchDropped        = "patch-dropped"
)

// proven are the manifests whose dependencies a check proves (D9). A Go
// module or a Rust crate is compiled into what the port builds, and a
// check builds with only what the port declares, so one needing a library
// the port doesn't declare fails it. A Node package is fetched and bundled
// by the build, as npm and yarn install it: no port provides one, so a
// check that builds proves it resolves, and one with native code compiles
// there or fails it (D9, for Node, decided 2026-09-30 after beekeeper-studio
// held on every plain npm addition). What they change is counted, and holds
// nothing, nor does what the comparison couldn't read of them. A Python
// dependency is another port, found when the software runs, which a build
// doesn't prove.
var proven = map[string]bool{"go.mod": true, "Cargo.toml": true, project.CargoLock: true, "package.json": true}

// namedDependencies is how many of a kind a count names.
const namedDependencies = 3

// Assess assesses a port's update: each pair's changes, what the new
// version's Python requirements ask of the ports that provide them, and
// what its go.mod asks of go.toolchain_min. A change the pairs share is
// said once. Findings that hold come first within each manifest, as a
// person reads them.
func Assess(input Input) model.UpstreamComparison {
	a := assessment{input: input, comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{}}}
	for _, pair := range input.Pairs {
		a.pair(pair)
	}
	for _, found := range a.pins() {
		a.add(found)
	}
	if found, ok := a.toolchain(); ok {
		a.add(found)
	}
	if found, ok := a.requiresPython(); ok {
		a.add(found)
	}
	if found, ok := a.pythonPin(); ok {
		a.add(found)
	}
	for _, found := range a.patches() {
		a.add(found)
	}
	return a.comparison
}

// assessment is an assessment as it's made.
type assessment struct {
	input      Input
	comparison model.UpstreamComparison
	// requirements are the Python requirements of manifests the port's
	// build uses, each with its declarations in both versions, for pins.
	requirements []requirement
	// wanted are the ports whose presence judging a native library needs
	// that weren't observed, for Wanted.
	wanted []Provider
}

// requirement is a Python dependency a used manifest declares, in the new
// version and the old, and whether it changed between them.
type requirement struct {
	manifest, name string
	now, before    []project.Requirement
	changed        bool
}

// add adds a finding, once.
func (a *assessment) add(found model.UpstreamChange) {
	if !slices.ContainsFunc(a.comparison.Changes, func(c model.UpstreamChange) bool { return c.Key() == found.Key() && c.Message == found.Message }) {
		a.comparison.Changes = append(a.comparison.Changes, found)
	}
}

// cover records coverage, once.
func (a *assessment) cover(coverage model.Coverage) {
	if !slices.Contains(a.comparison.Coverage, coverage) {
		a.comparison.Coverage = append(a.comparison.Coverage, coverage)
	}
}

// pair assesses one pair's changes.
func (a *assessment) pair(pair Pair) {
	port := pair.Port
	if port.Name == "" {
		port = a.input.Port
	}
	for _, reading := range []project.Reading{pair.Before, pair.After} {
		if reading.Missing != "" {
			a.cover(model.Coverage{Path: reading.Missing, Relevance: "unknown", Treatment: "inspected",
				Reason: fmt.Sprintf("the port builds in %s, which %s doesn't have, so it was read at its top", reading.Missing, pair.Archive)})
		}
	}
	// A file of a build system the port doesn't use holds nothing:
	// flatbuffers, built with CMake, held on package.json and
	// Package.swift, which its build never reads (the flatbuffers run's
	// finding 2). Which the port uses is MacPorts' to say, in the context
	// that fetches the archive; where it can't say, every file holds as
	// before. It's a policy, not a proof: the file's relevance is unknown,
	// and it's set apart under PortGroup scoping, which a later rule can
	// revisit (the assessment design's critique, point 7).
	uses, known := port.BuildSystems()
	unused := func(system project.System) bool {
		return known && system != "" && !slices.Contains(uses, system)
	}
	var names []string
	for _, system := range uses {
		names = append(names, string(system))
	}
	why := func(system project.System) string {
		return fmt.Sprintf("%s builds with %s, not %s", port.Name, strings.Join(names, " and "), system)
	}
	changes := sourcecompare.Compare(pair.Before, pair.After, a.input.Versions, func(option string) bool {
		return a.input.Portfile == nil || portfile.Mentions(a.input.Portfile, option)
	})
	// Each manifest's dependency findings are put in order as a run: what
	// holds first. A proven manifest's are counted in one line, where the
	// file is.
	var run []model.UpstreamChange
	var proved []sourcecompare.Change
	flush := func() {
		if len(proved) > 0 {
			a.add(count(proved[0].Path, proved))
			proved = nil
		}
		slices.SortStableFunc(run, func(x, y model.UpstreamChange) int {
			switch {
			case x.Hold == y.Hold:
				return 0
			case x.Hold:
				return -1
			}
			return 1
		})
		for _, found := range run {
			a.add(found)
		}
		run = nil
	}
	for _, change := range changes {
		if len(run) > 0 && (change.Kind != "dependency" || change.Path != run[0].Path) ||
			len(proved) > 0 && (change.Kind != "dependency" || change.Path != proved[0].Path) {
			flush()
		}
		base := path.Base(change.Path)
		// What changed in a file of a build system the port doesn't use is
		// said in coverage alone: rust's "package.json: 1 dependency
		// changed; rust builds with cargo, not node", its in-tree tidy
		// tooling's, was accurate and noise (rust 1.99.0, batch 23).
		if unused(change.System) {
			a.cover(model.Coverage{Path: change.Path, System: string(change.System), Relevance: "unknown", Treatment: "set-apart", Policy: "portgroup-scoping", Reason: why(change.System)})
			continue
		}
		switch {
		case change.How == "unlinked":
			if found, ok := unlinked(change, port); ok {
				a.add(found)
			}
			continue
		case change.Kind == "dependency" && change.How != "native" && proven[base]:
			proved = append(proved, change)
			continue
		case change.Kind == "dependency" && change.How != "native":
			if change.Requirements != nil {
				a.need(requirement{manifest: change.Path, name: change.Name, now: change.Requirements, before: change.Before, changed: true})
			}
			run = append(run, dependency(change))
			continue
		case change.Kind == "unread":
			// What couldn't be read holds, as a change would, but for a
			// proven manifest's (D9); an archive whose project wasn't
			// found names no manifest, and holds.
			a.add(finding(change, !proven[base]))
		case change.How == "native":
			if found, ok := a.native(change); ok {
				a.add(found)
			}
		case change.Kind == "license" && (change.How == "years" || change.How == "moved"):
			a.add(finding(change, false))
		case change.Kind == "license" && a.input.New:
			a.add(newLicense(change, pair, port))
		case change.Kind == "license":
			a.add(a.license(change, pair, port))
		case change.Kind == "build" && a.input.New:
			// Every build file of a new port is new to it, which a passing
			// build speaks for; what was read is said in coverage.
		case change.Kind == "build":
			a.add(a.build(change, pair))
		default:
			a.add(finding(change, false))
		}
	}
	flush()
	a.unchanged(pair, unused)
	a.read(pair, unused, known, why)
}

// read covers each file of the new version's that was read, so a
// comparison that found nothing isn't taken for one that didn't look:
// rust's said only its package.json, and the person couldn't tell whether
// its Cargo manifests had been read (rust 1.99.0, batch 23). A file of a
// build system the port doesn't use is set apart, as its changes are.
func (a *assessment) read(pair Pair, unused func(project.System) bool, known bool, why func(project.System) string) {
	for _, name := range slices.Sorted(maps.Keys(pair.After.Files)) {
		system := project.SystemOf(name)
		if unused(system) {
			a.cover(model.Coverage{Path: name, System: string(system), Relevance: "unknown", Treatment: "set-apart", Policy: "portgroup-scoping", Reason: why(system)})
			continue
		}
		relevance := "used"
		if !known && system != "" {
			relevance = "unknown"
		}
		reason := "compared with the base's"
		if _, ok := pair.Before.Files[name]; !ok {
			reason = "new in this version"
		}
		a.cover(model.Coverage{Path: name, System: string(system), Relevance: relevance, Treatment: "inspected", Policy: "read", Reason: reason})
	}
}

// finding is a change as a finding, holding or not, with its rule.
func finding(change sourcecompare.Change, hold bool) model.UpstreamChange {
	found := model.UpstreamChange{Kind: change.Kind, Path: change.Path, Message: change.Message, Hold: hold, Class: model.Introduced}
	switch {
	case change.Kind == "license" && change.How == "years":
		found.Rule = LicenseYears
	case change.Kind == "license" && change.How == "moved":
		found.Rule = LicenseMoved
	case change.Kind == "license":
		found.Rule = LicenseChanged
	case change.Kind == "build" && change.How == "version":
		found.Rule = BuildFileVersion
	case change.Kind == "build" && change.How == "options":
		found.Rule = BuildFileOptions
	case change.Kind == "build":
		found.Rule = BuildFileChanged
	case change.How == "ambiguous":
		found.Rule, found.Subject = LayoutAmbiguous, change.Side
	case change.Kind == "unread":
		found.Rule, found.Subject = Unread, change.Side
	case change.How == "native":
		found.Rule, found.Subject = NativeLibrary, change.Name
	case change.How == "unlinked":
		found.Rule, found.Subject = NativeLibraryLeft, change.Name
	}
	// What the old version couldn't be read for leaves the base unknown.
	if change.Kind == "unread" && change.Side == "old" {
		found.Class = model.UnknownBaseline
	}
	return found
}

// unlinked is a crate that linked a native library, gone from Cargo.lock,
// as a finding where the port still has what's there for the library: a
// PortGroup or a dependency named for it, which still reach the build.
// zola's PortGroup openssl, left once openssl-sys went, put OpenSSL 3's
// headers before aws-lc's own, and broke its build. It holds nothing, and
// is said only where the port has something left.
func unlinked(change sourcecompare.Change, port macports.PortInfo) (model.UpstreamChange, bool) {
	ties := port.TiesTo(project.CargoPackage{Name: change.Name}.NativeLibrary())
	var still []string
	for _, group := range ties.PortGroups {
		still = append(still, "PortGroup "+group)
	}
	for _, dependency := range ties.Ports {
		still = append(still, "its dependency on "+dependency)
	}
	if len(still) == 0 {
		return model.UpstreamChange{}, false
	}
	it, goes := "it", "it can go, since it still reaches the build"
	if len(still) > 1 {
		it, goes = "them", "they can go, since they still reach the build"
	}
	found := finding(change, false)
	found.Message += fmt.Sprintf(", while the Portfile still has %s: unless something else needs %s, %s", strings.Join(still, " and "), it, goes)
	return found, true
}

// native is a crate new to Cargo.lock that links a native library, said
// where MacPorts has a port for the library, named, for the Portfile to
// declare rather than the crate linking whatever copy it finds. zola's
// jni-sys, system-configuration-sys, and aws-lc-sys named libraries
// MacPorts has no port for, which a person could do nothing about (the
// zola run with 68df8b57): one none of whose names (macports.LibraryPorts)
// is a port in the candidate's tree is set apart in coverage. Where that
// couldn't be observed, it's said as it was, MacPorts perhaps providing
// it; Wanted asks for each name not yet observed.
func (a *assessment) native(change sourcecompare.Change) (model.UpstreamChange, bool) {
	library := project.CargoPackage{Name: change.Name}.NativeLibrary()
	found := finding(change, false)
	absent := true
	for _, name := range macports.LibraryPorts(library) {
		observation, ok := a.input.Observed[Provider{Port: name}]
		switch {
		case !ok:
			a.wanted = append(a.wanted, Provider{Port: name})
			absent = false
		case observation.Problem != "":
			absent = false
		case !observation.Absent:
			where := name
			if observation.Directory != "" {
				where = observation.Directory
			}
			found.Message += fmt.Sprintf(", which MacPorts has as %s: the Portfile may declare it, rather than the crate linking whatever copy it finds", where)
			return found, true
		}
	}
	if absent {
		a.cover(model.Coverage{Path: change.Path, Relevance: "unknown", Treatment: "set-apart", Policy: "native-library-ports",
			Reason: fmt.Sprintf("%s links %s, which MacPorts has no port for", change.Name, library)})
		return model.UpstreamChange{}, false
	}
	found.Message += ": MacPorts may provide it, for the Portfile to declare, rather than the crate linking whatever copy it finds"
	return found, true
}

// build is a build file's change as a finding: one of the project's own
// version alone holds nothing; one the Python project's backend doesn't
// read, as sshuttle's hatchling doesn't read the setup.cfg bumpversion
// keeps its version in, is set apart (the sshuttle run with f075232d); any
// other holds, the build perhaps needing the Portfile to follow.
func (a *assessment) build(change sourcecompare.Change, pair Pair) model.UpstreamChange {
	// One that only adds options, each built as its default, holds
	// nothing, as the person decided of D12 (2026-10-01).
	if change.How == "version" || change.How == "options" {
		return finding(change, false)
	}
	if backend, ok := pair.After.PythonBackend(); ok && !project.BackendReads(backend, change.Path) {
		reason := fmt.Sprintf("the project builds with %s, which doesn't read %s", backend, path.Base(change.Path))
		a.cover(model.Coverage{Path: change.Path, System: string(change.System), Relevance: "unknown", Treatment: "set-apart", Policy: "build-backend", Reason: reason})
		found := finding(change, false)
		found.Message += "; " + reason + ", so it holds nothing"
		return found
	}
	found := finding(change, true)
	found.Message += "; the build may need the Portfile to follow"
	return found
}

// license is a license file's change as a finding, which holds, with
// what the project's manifest declares beside what the Portfile says:
// zola's "LICENSE-MIT was added" said nothing of Cargo.toml's EUPL-1.2,
// which the Portfile's MIT had missed since 0.22.0 (the zola run with
// 68df8b57). Where the candidate's license line names what the manifest
// declares and the base's didn't, the Portfile has followed, and it holds
// nothing.
func (a *assessment) license(change sourcecompare.Change, pair Pair, port macports.PortInfo) model.UpstreamChange {
	found := finding(change, true)
	declared, file, ok := pair.After.DeclaredLicense()
	evidence := ""
	if ok {
		evidence = file + " says " + declared
		if was, _, ok := pair.Before.DeclaredLicense(); ok && was != declared {
			evidence = fmt.Sprintf("%s's license moves from %s to %s", file, was, declared)
		}
		if named, ok := macports.License(declared); ok && macports.LicenseNames(port.Options["license"], named) && !macports.LicenseNames(a.input.Base.Options["license"], named) {
			found.Hold = false
			found.Message += "; " + evidence + ", which the Portfile's license line now names"
			return found
		}
		if line := port.Options["license"]; line != "" {
			evidence += ", and the Portfile says " + macports.LicenseWords(line)
		}
	}
	found.Message += "; the Portfile's license line may need to follow"
	if evidence != "" {
		found.Message += "; " + evidence
	}
	return found
}

// newLicense is a new port's license file, which has no earlier one to
// compare: said with the Portfile's license line, and what the project's
// manifest declares beside it, which holds only where the line doesn't
// name it. sand-runner's "LICENSE was added; the Portfile's license line
// may need to follow" asked a look of every new port (batch 28).
func newLicense(change sourcecompare.Change, pair Pair, port macports.PortInfo) model.UpstreamChange {
	found := finding(change, false)
	line := port.Options["license"]
	found.Message = fmt.Sprintf("upstream ships %s, and the Portfile names no license", change.Path)
	if line != "" {
		found.Message = fmt.Sprintf("upstream ships %s, and the Portfile says %s", change.Path, macports.LicenseWords(line))
	}
	declared, file, ok := pair.After.DeclaredLicense()
	if !ok {
		return found
	}
	if named, known := macports.License(declared); known && macports.LicenseNames(line, named) {
		found.Message += ", as " + file + " does"
		return found
	}
	found.Hold = true
	found.Message += fmt.Sprintf(", where %s says %s; the Portfile's license line may need to follow", file, declared)
	return found
}

// dependency is a declared dependency's change as a finding. One the new
// version adds holds, as another port the Portfile may need to declare,
// unless it applies only elsewhere, as a Windows-only one does; one that
// applied only elsewhere and now may apply here is as good as added, and
// holds as one (the helper-ownership review's finding 1). One dropped
// holds nothing.
func dependency(change sourcecompare.Change) model.UpstreamChange {
	found := model.UpstreamChange{Kind: "dependency", Path: change.Path, Message: change.Message, Subject: change.Name, Class: model.Introduced}
	switch change.How {
	case "adds":
		found.Rule, found.Hold = DependencyAdded, !elsewhere(change.Requirements)
	case "drops":
		found.Rule = DependencyDropped
	default:
		found.Rule = DependencyMoved
		if change.Requirements != nil && elsewhere(change.Before) && !elsewhere(change.Requirements) {
			found.Hold, found.Message = true, found.Message+", which now may apply to macOS"
		}
	}
	return found
}

// elsewhere reports declarations of a requirement that all apply only
// elsewhere than macOS, by their markers; none, or one that may apply, is
// not.
func elsewhere(declarations []project.Requirement) bool {
	for _, declaration := range declarations {
		if applies, err := declaration.OnMacOS(""); err != nil || applies != project.No {
			return false
		}
	}
	return len(declarations) > 0
}

// count says in one line how many of a proven manifest's declared
// dependencies it gained, lost, and moved, holding nothing (D9), naming
// them where there are few of a kind: "upstream: Cargo.toml: 1 added
// (inferno), 1 moved (open)", and "upstream: go.mod: 2 added, 14 moved"
// (the txt run's finding 6).
func count(file string, changes []sourcecompare.Change) model.UpstreamChange {
	names := map[string][]string{}
	for _, change := range changes {
		names[change.How] = append(names[change.How], change.Name)
	}
	var parts []string
	for _, part := range [][2]string{{"adds", "added"}, {"drops", "dropped"}, {"moves", "moved"}} {
		some := names[part[0]]
		switch {
		case len(some) == 0:
		case len(some) <= namedDependencies:
			parts = append(parts, fmt.Sprintf("%d %s (%s)", len(some), part[1], strings.Join(some, ", ")))
		default:
			parts = append(parts, fmt.Sprintf("%d %s", len(some), part[1]))
		}
	}
	return model.UpstreamChange{Kind: "dependency", Path: file, Rule: DependenciesCounted, Class: model.Introduced,
		Message: fmt.Sprintf("upstream: %s: %s", file, strings.Join(parts, ", "))}
}

// unchanged gathers the Python requirements of the new version's used
// manifests that no change names, so pins can find a provider the
// Portfile no longer depends on under a manifest that didn't change.
func (a *assessment) unchanged(pair Pair, unused func(project.System) bool) {
	for name, file := range pair.After.Files {
		if file.Truncated || unused(project.SystemOf(name)) {
			continue
		}
		now := declarations(name, file.Data)
		if now == nil {
			continue
		}
		var before map[string][]project.Requirement
		if old, ok := pair.Before.Files[name]; ok && !old.Truncated {
			before = declarations(name, old.Data)
		}
		for needed, declared := range now {
			a.need(requirement{manifest: name, name: needed, now: declared, before: before[needed]})
		}
	}
	slices.SortFunc(a.requirements, func(x, y requirement) int {
		return strings.Compare(x.manifest+"\x00"+x.name, y.manifest+"\x00"+y.name)
	})
}

// need records a requirement, once: one a change names is kept as changed
// whichever pair named it.
func (a *assessment) need(r requirement) {
	i := slices.IndexFunc(a.requirements, func(have requirement) bool { return have.manifest == r.manifest && have.name == r.name })
	if i < 0 {
		a.requirements = append(a.requirements, r)
		return
	}
	a.requirements[i].changed = a.requirements[i].changed || r.changed
}

// declarations are a Python manifest's requirements by name; nil for
// another kind of file, or one that can't be read, which the comparison
// has said.
func declarations(name string, data []byte) map[string][]project.Requirement {
	var found []project.Declaration
	switch path.Base(name) {
	case "requirements.txt":
		found = project.ReadRequirements(data).Declarations
	case "pyproject.toml":
		manifest, err := project.ReadPyproject(data)
		if err != nil || manifest.Project == nil {
			return nil
		}
		found = manifest.Project.Dependencies
	default:
		return nil
	}
	byName := map[string][]project.Requirement{}
	for _, declaration := range found {
		byName[declaration.Name] = append(byName[declaration.Name], declaration.Requirement)
	}
	return byName
}
