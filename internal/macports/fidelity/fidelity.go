package fidelity

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// ErrMismatch means an evaluation does not match the intended change.
var ErrMismatch = errors.New("fidelity: evaluation does not match the intended change")

// Report is what an evaluated edit was expected to change and what it changed
// beyond that. Any unexpected change refuses the edit.
type Report struct {
	Before            macports.Snapshot
	After             macports.Snapshot
	ExpectedChanges   []string
	UnexpectedChanges []string
}

// Err is the report's unexpected changes as one ErrMismatch, said once and
// joined: printed as a list, they read "[fidelity: …: rust-src has …]"
// inside another "fidelity: …" (the rust and cargo run).
func (r Report) Err() error {
	return fmt.Errorf("%w: %s", ErrMismatch, strings.Join(r.UnexpectedChanges, "; "))
}

// CheckSnapshot requires an evaluation to describe its bound source, target,
// and platform completely.
func CheckSnapshot(snapshot macports.Snapshot, bound macports.Context) error {
	platform := snapshot.Platform
	if snapshot.Source != bound.Source() || !reflect.DeepEqual(snapshot.Target, bound.Target()) || platform.OS == "" || platform.Version == "" || platform.Architecture == "" || (bound.Platform().OS != "" && platform != bound.Platform()) {
		return fmt.Errorf("%w: evaluation does not match its bound source, target, or platform", ErrMismatch)
	}
	if len(snapshot.Ports) == 0 {
		return fmt.Errorf("%w: evaluation contains no ports", ErrMismatch)
	}
	for name, port := range snapshot.Ports {
		if name != port.Name || port.Version == "" || port.Revision < 0 || port.Epoch < 0 {
			return fmt.Errorf("%w: invalid evaluated port %s", ErrMismatch, name)
		}
	}
	return nil
}

// Revision expects only the selected port's revision to advance by one.
func Revision(before, after macports.Snapshot, selected string) Report {
	prediction := Predict(before)
	prediction.Expected = []string{selected + ".revision +1"}
	for name, old := range before.Ports {
		next, is := after.Ports[name]
		// A sibling that shares the selected port's revision line moves
		// with it, as the python PortGroup's subports share their stub's:
		// one at the selected port's revision that rises with it.
		shared := name != selected && is && old.Revision == before.Ports[selected].Revision && next.Revision == old.Revision+1
		if name != selected && !shared {
			continue
		}
		port := prediction.Port(name)
		port.Revision++
		if shared {
			prediction.Expect(name, port, name+".revision +1, shared")
		} else {
			prediction.Expect(name, port)
		}
	}
	return prediction.Observe(before, after)
}

// ComparablePort normalizes a port for comparison: the revision is compared
// separately, the port's code is left out, and the workspace root the
// port was evaluated in is replaced so relocated snapshots compare equal.
// An empty root, as a snapshot read back from a record has, leaves values
// as they are.
func ComparablePort(port macports.PortInfo, root string) macports.PortInfo {
	port.Options = maps.Clone(port.Options)
	delete(port.Options, "revision")
	// The port's code is a change record's to compare (Changes), and part
	// of it moves without an edit.
	delete(port.Options, codeOption)
	port.OptionErrors = maps.Clone(port.OptionErrors)
	if root == "" {
		return port
	}
	for key, value := range port.Options {
		port.Options[key] = strings.ReplaceAll(value, root, "<source>")
	}
	for key, value := range port.OptionErrors {
		port.OptionErrors[key] = strings.ReplaceAll(value, root, "<source>")
	}
	return port
}

// Compare lists the metadata differences between two normalized ports.
func Compare(name string, old, next macports.PortInfo) []string {
	var changed []string
	for _, change := range differences(old, next) {
		changed = append(changed, name+"."+change.Field+" changed")
	}
	return changed
}

// Changes are the evaluated fields that moved between two evaluations of
// a port, each normalized by the root it was evaluated in: the revision,
// and every field Compare compares. A change record is made of them.
func Changes(old, next macports.PortInfo, oldRoot, nextRoot string) []model.FieldChange {
	var changes []model.FieldChange
	if old.Revision != next.Revision {
		changes = append(changes, model.FieldChange{Field: "revision", From: strconv.Itoa(old.Revision), To: strconv.Itoa(next.Revision)})
	}
	changes = append(changes, differences(ComparablePort(old, oldRoot), ComparablePort(next, nextRoot))...)
	return append(changes, codeChanges(old.Options[codeOption], next.Options[codeOption])...)
}

// codeOption is the evaluator's checksum of each of a port's hook and
// variant bodies and of its other procedures, which no option shows: a
// phase's body changed, a variant's, or a procedure the Portfile defines.
const codeOption = "dockhand.code"

// unstableCode starts the field of a part of a port's code known to move
// without an edit, such as source_date_epoch, the Portfile's modification
// time: said in a change record, but no change.
const unstableCode = "code unstable:"

// codeChanges are the parts of a port's code whose checksums moved
// between two evaluations, each as the field "code <part>": a hook's is
// Base's name for it, such as userproc-post-org.macports.destroot-
// destroot-0, a variant's variant-<name>, the rest of its procedures'
// together "procedures", every option's value together "options", a
// patch it applies patch:<name>, and an option known to move without an
// edit unstable:<name>.
func codeChanges(old, next string) []model.FieldChange {
	pairs := func(value string) map[string]string {
		fields := strings.Fields(value)
		sums := map[string]string{}
		for i := 0; i+1 < len(fields); i += 2 {
			sums[fields[i]] = fields[i+1]
		}
		return sums
	}
	before, after := pairs(old), pairs(next)
	names := slices.Sorted(maps.Keys(before))
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var changes []model.FieldChange
	for _, name := range names {
		if before[name] != after[name] {
			changes = append(changes, model.FieldChange{Field: "code " + name, From: before[name], To: after[name]})
		}
	}
	return changes
}

// differences are the fields that moved between two normalized ports:
// the name, version, epoch, dependencies, and fetch's kind, then the
// options in order, then the options' errors.
func differences(old, next macports.PortInfo) []model.FieldChange {
	var changes []model.FieldChange
	field := func(name, from, to string) {
		if from != to {
			changes = append(changes, model.FieldChange{Field: name, From: from, To: to})
		}
	}
	field("name", old.Name, next.Name)
	field("version", old.Version, next.Version)
	field("epoch", strconv.Itoa(old.Epoch), strconv.Itoa(next.Epoch))
	if !reflect.DeepEqual(old.Dependencies, next.Dependencies) {
		changes = append(changes, model.FieldChange{Field: "dependencies", From: dependencyWords(old.Dependencies), To: dependencyWords(next.Dependencies)})
	}
	// The fetch is compared by its kind, which the options' compatible
	// flag doesn't tell apart among the kinds a direct download repeats.
	field("fetch", fetchKind(old), fetchKind(next))
	keys := slices.Sorted(maps.Keys(old.Options))
	for key := range next.Options {
		if _, ok := old.Options[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		// The name, version, and epoch are the port's own fields, compared
		// above; their options say them again. The port's code is a
		// change record's to compare (Changes), not an edit's.
		if key == "name" || key == "version" || key == "epoch" || key == codeOption {
			continue
		}
		a, aok := old.Options[key]
		b, bok := next.Options[key]
		// A path below the source directory is the same path in two
		// evaluations: qemu's configure.cmd is ${worksrcpath}/configure,
		// whose build directory is named for where the Portfile was
		// evaluated and whose source directory for the version, and it
		// read as a change, so update, --outdated, and checksums each
		// refused qemu (field testing, 2026-10-02).
		if key != "worksrcdir" {
			a, b = belowSource(a), belowSource(b)
		}
		if aok != bok || a != b {
			changes = append(changes, model.FieldChange{Field: key, From: a, To: b})
		}
	}
	if !maps.Equal(old.OptionErrors, next.OptionErrors) {
		changes = append(changes, model.FieldChange{Field: "option-errors", From: errorWords(old.OptionErrors), To: errorWords(next.OptionErrors)})
	}
	return changes
}

// dependencyWords are dependencies as one line, each phase:port.
func dependencyWords(dependencies []macports.Dependency) string {
	words := make([]string, 0, len(dependencies))
	for _, d := range dependencies {
		words = append(words, d.Phase+":"+d.Spec)
	}
	return strings.Join(words, " ")
}

// errorWords are options' errors as one line, by option.
func errorWords(errs map[string]string) string {
	var words []string
	for _, key := range slices.Sorted(maps.Keys(errs)) {
		words = append(words, key+": "+errs[key])
	}
	return strings.Join(words, "; ")
}

// workPath is a path below a port's source directory as MacPorts lays
// it out: ${portdbpath}/build/<the port's directory, hashed>/work/
// <worksrcdir>.
var workPath = regexp.MustCompile(`/build/[^/\s]+/work/[^/\s"'}]+`)

// belowSource is value with each path to a port's source directory
// written as one placeholder, so a path below it reads alike wherever,
// and at whichever version, the port was evaluated.
func belowSource(value string) string {
	return workPath.ReplaceAllString(value, "/<worksrcpath>")
}

func fetchKind(port macports.PortInfo) string {
	if port.Fetch == nil {
		return ""
	}
	return port.Fetch.Kind
}

// Equivalent requires two evaluations of the same context to agree on every
// port and every compared field, each normalized by its own root.
func Equivalent(expected, actual macports.Snapshot) error {
	if expected.Platform != actual.Platform || !reflect.DeepEqual(expected.Target, actual.Target) {
		return fmt.Errorf("%w: candidate evaluation changed evaluation context", ErrMismatch)
	}
	if len(expected.Ports) != len(actual.Ports) {
		return fmt.Errorf("%w: candidate evaluation changed port set", ErrMismatch)
	}
	for name, old := range expected.Ports {
		next, ok := actual.Ports[name]
		if !ok || old.Revision != next.Revision {
			return fmt.Errorf("%w: candidate evaluation changed %s", ErrMismatch, name)
		}
		if changes := Compare(name, ComparablePort(old, expected.Root), ComparablePort(next, actual.Root)); len(changes) > 0 {
			return fmt.Errorf("%w: candidate evaluation: %v", ErrMismatch, changes)
		}
	}
	return nil
}

// GitVersion is the version expectation for a port fetched with git: the
// version moves, the revision resets, nothing is downloaded, and git.branch
// lands on branch, the resolved tag or the resolved commit. An empty branch
// leaves git.branch unchecked, for a source whose tag is not known.
func GitVersion(shared bool, before, after macports.Snapshot, selected string, release model.Release, branch string) Report {
	release.Tag = branch
	report := ScopedVersion(shared, before, after, selected, release, "")
	for i, change := range report.ExpectedChanges {
		if change == selected+".distfiles and checksums" {
			report.ExpectedChanges[i] = selected + ".git.branch -> " + branch
			if branch == "" {
				report.ExpectedChanges[i] = selected + ".git.branch follows the version"
			}
		}
	}
	return report
}

// Checksums expects only the selected port's checksums to change.
func Checksums(before, after macports.Snapshot, selected, checksums string) Report {
	prediction := Predict(before)
	port := prediction.Port(selected)
	port.Options["checksums"] = checksums
	prediction.Expect(selected, port, selected+".checksums")
	// A sibling of the same version whose checksums were the selected
	// port's, and are its new ones now, shares its declaration, as a python
	// stub and its subports share one, and moves with it. A sibling that
	// keeps its own is left as it was.
	shared := before.Ports[selected]
	for _, name := range slices.Sorted(maps.Keys(before.Ports)) {
		sibling := before.Ports[name]
		if name == selected || sibling.Version != shared.Version || sibling.Options["checksums"] != shared.Options["checksums"] || after.Ports[name].Options["checksums"] != checksums {
			continue
		}
		port := prediction.Port(name)
		port.Options["checksums"] = checksums
		prediction.Expect(name, port, name+".checksums, shared")
	}
	// The selected port's checksums are its list's values, however the
	// list is written.
	normalized := after
	normalized.Ports = maps.Clone(after.Ports)
	var problems []string
	if next, ok := normalized.Ports[selected]; ok {
		next.Options = maps.Clone(next.Options)
		values, errs := syntax.ListValues(next.Options["checksums"])
		wanted, wantedErrs := syntax.ListValues(checksums)
		if len(errs) > 0 || len(wantedErrs) > 0 || !slices.Equal(values, wanted) {
			problems = append(problems, selected+".checksums differ from intended values")
		}
		next.Options["checksums"] = checksums
		normalized.Ports[selected] = next
	}
	report := prediction.Observe(before, normalized)
	report.After = after
	report.UnexpectedChanges = append(report.UnexpectedChanges, problems...)
	slices.Sort(report.UnexpectedChanges)
	return report
}

// ReleaseScope partitions sibling ports into those a release changes and those
// it must protect, and refuses what no answer takes (Findings): a sibling
// that moves without shared authorization, one that keeps a version of its
// own, or one that fetches a source of its own.
func ReleaseScope(before, after macports.Snapshot, selected string, authorized bool) (*macports.ReleaseScope, error) {
	scope, findings, err := Scope(before, after, selected)
	if err != nil {
		return nil, err
	}
	for _, finding := range findings {
		if !finding.AnsweredBy(authorized) {
			return nil, fmt.Errorf("%w: %s", ErrMismatch, finding)
		}
	}
	return scope, nil
}

// ScopeKind is the kind of what partitioning a release found of a sibling.
type ScopeKind string

const (
	// SiblingMoves is a sibling that moves with the selected port's
	// release, which --shared-release, the person's decision, answers.
	SiblingMoves ScopeKind = "sibling-moves"
	// SiblingOwnVersion is a sibling that moves to a version of its own,
	// which updating the selected port can't take.
	SiblingOwnVersion ScopeKind = "sibling-own-version"
	// SiblingOwnSource is a sibling at the release's version that fetches
	// a source of its own, as xapian-core's bindings do.
	SiblingOwnSource ScopeKind = "sibling-own-source"
)

// ScopeFinding is what partitioning a release found of one sibling: a
// finding with a kind, where it was a sentence that refused (strict
// fidelity, architecture's point 4).
type ScopeFinding struct {
	Kind ScopeKind
	Port string
	// Field is what a sibling of its own source differs from the selected
	// port's in: git.branch, distfiles, master_sites, or checksums.
	Field    string
	Selected string
}

// String is the finding as a person reads it: what they see of the
// Portfile, not how the release is prepared, where "rust-src has
// independent distfiles; shared-source preparation is required" said
// neither what nor why (the rust and cargo run).
func (f ScopeFinding) String() string {
	switch f.Kind {
	case SiblingMoves:
		return fmt.Sprintf("%s, another port of the same Portfile, moves with %s's release; --shared-release moves both, and --plan shows what that changes first", f.Port, f.Selected)
	case SiblingOwnVersion:
		return fmt.Sprintf("%s, another port of the same Portfile, keeps a version of its own, which updating %s doesn't move", f.Port, f.Selected)
	}
	return fmt.Sprintf("%s, another port of the same Portfile, fetches its own source (its %s differ from %s's), which updating %s doesn't move", f.Port, f.Field, f.Selected, f.Selected)
}

// AnsweredBy reports whether a finding is answered: a sibling moving, by
// shared authorization; neither of the others, by anything yet.
func (f ScopeFinding) AnsweredBy(shared bool) bool { return f.Kind == SiblingMoves && shared }

// Scope partitions sibling ports into those a release changes and those it
// must protect, with what it found of each that moves: whether it moves
// with the release, keeps a version of its own, or fetches a source of its
// own. An obsolete follower, which builds nothing and is replaced by the
// selected port, moves with it and asks no answer.
func Scope(before, after macports.Snapshot, selected string) (*macports.ReleaseScope, []ScopeFinding, error) {
	scope := &macports.ReleaseScope{}
	var findings []ScopeFinding
	oldRoot, nextRoot := before.Ports[selected], after.Ports[selected]
	if len(before.Ports) != len(after.Ports) {
		return nil, nil, fmt.Errorf("%w: release changes the port set", ErrMismatch)
	}
	for _, name := range slices.Sorted(maps.Keys(before.Ports)) {
		old := before.Ports[name]
		next, ok := after.Ports[name]
		if !ok {
			return nil, nil, fmt.Errorf("%w: sibling disappeared", ErrMismatch)
		}
		target := before.Target
		target.Name = name
		target.Subport = ""
		if name != before.Target.Name {
			target.Subport = name
		}
		needsXcode, err := next.Bool("use_xcode")
		if err != nil {
			return nil, nil, err
		}
		metadataOnly, err := next.MetadataOnly()
		if err != nil {
			return nil, nil, err
		}
		member := macports.ReleaseMember{Target: target, Before: macports.ReleaseStateOf(old), After: macports.ReleaseStateOf(next), NeedsXcode: needsXcode, MetadataOnly: metadataOnly}
		if old.Version == next.Version {
			scope.Protected = append(scope.Protected, member)
			continue
		}
		member.Follower = followsObsolete(name, selected, old, next, oldRoot, nextRoot)
		if member.NeedsAuthorization(selected) {
			findings = append(findings, ScopeFinding{Kind: SiblingMoves, Port: name, Selected: selected})
		}
		switch {
		case old.Version != oldRoot.Version || next.Version != nextRoot.Version:
			findings = append(findings, ScopeFinding{Kind: SiblingOwnVersion, Port: name, Selected: selected})
		// A shared input must describe the same source, not just
		// coincident versions. A member with no source, one that builds
		// nothing or fetches nothing, as libgcc14 beside gcc14, has none
		// to get wrong.
		case !member.MetadataOnly && !fetchesNothing(old, next):
			for _, key := range []string{"git.branch", "distfiles", "master_sites", "checksums"} {
				if old.Options[key] != oldRoot.Options[key] || next.Options[key] != nextRoot.Options[key] {
					findings = append(findings, ScopeFinding{Kind: SiblingOwnSource, Port: name, Field: key, Selected: selected})
					break
				}
			}
		}
		scope.Affected = append(scope.Affected, member)
	}
	slices.SortFunc(scope.Affected, func(a, b macports.ReleaseMember) int { return model.CompareTargets(a.Target, b.Target) })
	slices.SortFunc(scope.Protected, func(a, b macports.ReleaseMember) int { return model.CompareTargets(a.Target, b.Target) })
	return scope, findings, nil
}

// fetchesNothing reports a port with nothing to download, before the
// release and after.
func fetchesNothing(old, next macports.PortInfo) bool {
	before, err := old.FetchesNothing()
	if err != nil || !before {
		return false
	}
	after, err := next.FetchesNothing()
	return err == nil && after
}

// followsObsolete reports whether name is an obsolete follower of the selected
// port: replaced by it, carrying its version before and after, and building
// nothing. Such a sibling moves with the selected port without shared-release
// authorization, since it has no source of its own to get wrong.
func followsObsolete(name, selected string, old, next, oldRoot, nextRoot macports.PortInfo) bool {
	only, err := next.MetadataOnly()
	return name != selected && next.Options["replaced_by"] == selected && err == nil && only &&
		old.Version == oldRoot.Version && next.Version == nextRoot.Version
}

// ScopedVersion is the version step's prediction, observed: each member
// of the release the evaluation moved (ReleaseScope) at the release's
// version, its revision reset, and git.branch on the release's tag where
// it fetches its own source, the selected port with the checksums
// intended, and every protected sibling as it was. What the version's own
// evaluation changes in a member that moves is the version's: its
// archives, paths, and descriptions follow it however the Portfile derives
// them, where a fixed list of options that followed it refused what it
// didn't name (strict fidelity, as architecture settled it, 2026-10-22).
// Its dependencies are the exception, as what a version-conditional
// Portfile adds there is a change for a person to make, not the version's.
// Without shared authorization the members are the selected port and its
// obsolete followers.
func ScopedVersion(shared bool, before, after macports.Snapshot, selected string, release model.Release, checksums string) Report {
	scope, err := ReleaseScope(before, after, selected, shared)
	if err != nil {
		return Report{Before: before, After: after, UnexpectedChanges: []string{strings.TrimPrefix(err.Error(), ErrMismatch.Error()+": ")}}
	}
	prediction := Predict(before)
	prediction.Expected = []string{selected + ".version -> " + release.Version, selected + ".revision -> 0", selected + ".distfiles and checksums"}
	var problems []string
	for _, member := range scope.Affected {
		name := member.Target.Name
		next := ComparablePort(after.Ports[name], after.Root)
		if next.Version != release.Version {
			problems = append(problems, fmt.Sprintf("%s.version: expected %s, got %s", name, release.Version, next.Version))
		}
		if release.Tag != "" && !member.MetadataOnly && next.Options["git.branch"] != release.Tag {
			problems = append(problems, name+".git.branch differs from selected tag")
		}
		if name == selected {
			actual, errs := syntax.ListValues(next.Options["checksums"])
			expected, expectedErrs := syntax.ListValues(checksums)
			if len(errs) > 0 || len(expectedErrs) > 0 || !slices.Equal(actual, expected) {
				problems = append(problems, name+".checksums differ from intended values")
			}
		} else {
			prediction.Expected = append(prediction.Expected, name+" moves with "+selected)
		}
		next.Revision, next.Dependencies = 0, prediction.Ports[name].Dependencies
		prediction.Ports[name] = next
	}
	report := prediction.Observe(before, after)
	report.UnexpectedChanges = append(report.UnexpectedChanges, problems...)
	slices.Sort(report.UnexpectedChanges)
	return report
}

// ScopedChecksums applies Checksums across the affected members of a scope.
func ScopedChecksums(scope *macports.ReleaseScope, before, after macports.Snapshot, selected, checksums string) Report {
	if scope == nil {
		return Checksums(before, after, selected, checksums)
	}
	prediction := Predict(before)
	for _, member := range scope.Affected {
		name := member.Target.Name
		info := before.Ports[name]
		if info.Version != member.After.Version || info.Options["checksums"] != before.Ports[selected].Options["checksums"] {
			continue
		}
		port := prediction.Port(name)
		port.Options["checksums"] = checksums
		prediction.Expect(name, port, name+".checksums")
	}
	return prediction.Observe(before, after)
}

// SubportChanges are what moved in each subport of a directory between
// its evaluation at a base and at a revision, each side normalized by its
// own root: the main port first, the one the directory is named for, then
// the rest in MacPorts' order. A nil side is a directory the base or the
// revision doesn't have, whose subports are all added or all removed.
func SubportChanges(directory string, base, revision *macports.Snapshot) []model.SubportChange {
	var names []string
	main := path.Base(directory)
	for _, side := range []*macports.Snapshot{revision, base} {
		if side == nil {
			continue
		}
		for name := range side.Ports {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == main:
			return -1
		case b == main:
			return 1
		}
		return macports.ComparePortNames(a, b)
	})
	changes := make([]model.SubportChange, 0, len(names))
	for _, name := range names {
		var old, next macports.PortInfo
		had, has := false, false
		if base != nil {
			old, had = base.Ports[name]
		}
		if revision != nil {
			next, has = revision.Ports[name]
		}
		change := model.SubportChange{Port: name}
		switch {
		case !had:
			change.Kind = model.SubportAdded
		case !has:
			change.Kind = model.SubportRemoved
		default:
			for _, field := range Changes(old, next, base.Root, revision.Root) {
				if strings.HasPrefix(field.Field, unstableCode) {
					change.Unstable = append(change.Unstable, field)
				} else {
					change.Fields = append(change.Fields, field)
				}
			}
			change.Kind = model.SubportUnchanged
			if len(change.Fields) > 0 {
				change.Kind = model.SubportChanged
			}
		}
		changes = append(changes, change)
	}
	return changes
}
