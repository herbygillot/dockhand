// Package sourcecompare compares two versions of a project's upstream
// source, as project read them, for what a reviewer would ask about before
// an update is submitted (Design v3 §6.12): a license file, a build file,
// or a declared dependency. What it couldn't read it says, so an empty
// reading never stands for one it couldn't make.
package sourcecompare

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// Change is one difference between two versions' source that a reviewer
// would ask about, as a fact: what changed and how. What it means for a
// port, and whether it holds a submission, is macports/assess's to say.
type Change struct {
	// Kind is license, build, dependency, or unread: a file the comparison
	// couldn't read, or read only in part, or an archive whose project
	// wasn't found.
	Kind string
	// How is what happened to it:
	//   - a license or build file "added", "removed", or "changed", or
	//     "years" where only a license's copyright years moved, or "moved"
	//     where license text moved between files and none is new, and
	//     "version" where a build file changed only the project's version
	//     it declares;
	//   - a dependency "adds", "drops", or "moves", or "native" for a crate
	//     new to a Cargo.lock that links a native library, and "unlinked"
	//     for one gone from it;
	//   - an unread file "truncated", "unreadable", or "unfollowed", for
	//     another file a manifest includes, and "ambiguous" for an archive
	//     whose project wasn't found.
	How string
	// Side is the version that couldn't be read, "old" or "new", of an
	// unread change.
	Side string
	// Path is the file, relative to the archive's top directory.
	Path    string
	Message string
	// System is the build system the file belongs to, a manifest's
	// language or a build file's tool; empty for a license file.
	System project.System
	// Name is a dependency's name, and Old and Now its version or
	// constraint in each version, as the manifest writes it.
	Name, Old, Now string
	// Requirements and Before are a Python dependency's declarations in
	// the new version and the old, each with its specifier and marker;
	// none for every other change.
	Requirements, Before []project.Requirement
	// Licenses are what a license file's text is in the old version and
	// the new (project.File.License), where it has one; zero for every
	// other change.
	Licenses [2]project.LicenseText
}

// A copyright line names its holder and years: "Copyright (c) 2016-2026
// Kenneth Shaw". Its years are one, or a range or list of them.
var (
	copyrightLine  = regexp.MustCompile(`(?i)copyright|\(c\)|©`)
	copyrightYears = regexp.MustCompile(`\b(19|20)\d\d(\s*(-|–|,|and)\s*(19|20)\d\d)*\b`)
)

// yearsOnly reports whether two versions of a license file differ only in
// the years of their copyright lines, as a new year moves them, and gives
// the first such line as it now reads. A line added, removed, or changed in
// anything but its years is a change to the license file, which is read no
// further: a new holder, or a license's own text, could need the Portfile's
// license line to follow.
func yearsOnly(old, now []byte) (string, bool) {
	before, after := strings.Split(string(old), "\n"), strings.Split(string(now), "\n")
	if len(before) != len(after) {
		return "", false
	}
	first := ""
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		if !copyrightLine.MatchString(before[i]) || !copyrightLine.MatchString(after[i]) ||
			copyrightYears.ReplaceAllString(before[i], "") != copyrightYears.ReplaceAllString(after[i], "") {
			return "", false
		}
		if first == "" {
			first = quotable(after[i])
		}
	}
	return first, first != ""
}

// Versions are the release an update moves from and to, which a build file
// that changes only the version it names spells.
type Versions struct{ Old, New string }

// Compare reports the license files, build files, and declared
// dependencies that differ between two versions' readings, the old and the
// new, and what it couldn't read of them. Each is by its path below its
// archive's top, whose name, the version's, is set aside so the same file
// compares across versions. A version whose project couldn't be found is
// said. named says whether the port's Portfile names a build option, so
// may set it, in any variant; nil names none.
func Compare(older, newer project.Reading, versions Versions, named func(option string) bool) []Change {
	if named == nil {
		named = func(string) bool { return false }
	}
	var lost []Change
	for _, side := range []struct {
		version string
		reading project.Reading
	}{{"old", older}, {"new", newer}} {
		if side.reading.Layout == project.Ambiguous {
			lost = append(lost, Change{Kind: "unread", How: "ambiguous", Side: side.version,
				Message: fmt.Sprintf("upstream's %s archive holds %s and no file beside them, so which is the project wasn't found, and it wasn't compared", side.version, strings.Join(side.reading.Tops, ", "))})
		}
	}
	if len(lost) > 0 {
		return lost
	}
	before, after := older.Files, newer.Files
	var names []string
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var changes []Change
	for _, name := range names {
		old, hadOld := before[name]
		now, hasNow := after[name]
		base := path.Base(name)
		if strings.HasSuffix(name, ".cmake") {
			// A file a CMakeLists.txt include()s is compared as part of it.
			continue
		}
		if old.Truncated || now.Truncated {
			changes = append(changes, Change{Kind: "unread", How: "truncated", Path: name,
				Message: fmt.Sprintf("upstream's %s is larger than the %d KiB the comparison reads, so it wasn't compared", name, project.FileLimit>>10)})
			continue
		}
		// A CMakeLists.txt is read with the files it include()s, as one
		// document, and compared as one.
		var documents [2]project.CMakeDocument
		if base == "CMakeLists.txt" {
			documents = [2]project.CMakeDocument{older.CMakeDocument(name), newer.CMakeDocument(name)}
			if hadOld && hasNow && documents[0].Source() == documents[1].Source() {
				continue
			}
			old.Data, now.Data = []byte(documents[0].Source()), []byte(documents[1].Source())
		} else if hadOld && hasNow && bytes.Equal(old.Data, now.Data) {
			continue
		}
		switch {
		case base == project.CargoLock:
			changes = append(changes, nativeLinks(name, old, hadOld, now, hasNow)...)
		case project.Manifest(base):
			changes = append(changes, manifestChanges(name, old, hadOld, now, hasNow)...)
		case project.LicenseFile(base) && hadOld && hasNow:
			if line, ok := yearsOnly(old.Data, now.Data); ok {
				changes = append(changes, Change{Kind: "license", How: "years", Path: name,
					Message: fmt.Sprintf("upstream's %s changed only its copyright years: %q", name, line)})
				continue
			}
			fallthrough
		case project.LicenseFile(base):
			change := Change{Kind: "license", How: "changed", Path: name}
			if hadOld {
				change.Licenses[0] = old.License()
			}
			if hasNow {
				change.Licenses[1] = now.License()
			}
			switch {
			case !hadOld:
				change.How = "added"
			case !hasNow:
				change.How = "removed"
			}
			change.Message = fmt.Sprintf("upstream's %s %s", name, licenseWords(change, old.Data, now.Data))
			changes = append(changes, change)
		default:
			if line, ok := versionOnly(name, old.Data, now.Data, versions); hadOld && hasNow && ok {
				changes = append(changes, Change{Kind: "build", How: "version", Path: name,
					Message: fmt.Sprintf("upstream's %s changed only the version it names: %q", name, line)})
				continue
			}
			// An R package's DESCRIPTION changes every release; what its
			// build reads of it are its dependency fields (field
			// testing, 2026-10-02: R-Matrix held on "DESCRIPTION changed").
			if base == "DESCRIPTION" && hadOld && hasNow {
				changes = append(changes, rDescription(name, old.Data, now.Data))
				continue
			}
			if base == "CMakeLists.txt" && hadOld && hasNow {
				if words, ok := cmakeOptionsOnly(name, documents[0], documents[1], versions, named); ok {
					changes = append(changes, Change{Kind: "build", How: "options", Path: name,
						Message: fmt.Sprintf("upstream's %s %s", name, words)})
					continue
				}
			}
			how, what := "changed", "changed"
			switch {
			case !hadOld:
				how, what = "added", "is new"
			case !hasNow:
				how, what = "removed", "was removed"
			case base == "configure.ac" || base == "configure.in":
				what += autoconfWords(project.ReadAutoconf(old.Data), project.ReadAutoconf(now.Data))
			case base == "CMakeLists.txt":
				summary := cmakeWords(documents[0], documents[1])
				what += summary
				if where := cmakeElsewhere(documents[0], documents[1], versions, named); where != "" && strings.HasPrefix(summary, ":") {
					what += "; besides, lines change " + where
				} else if where != "" {
					what += "; lines change " + where
				}
			}
			changes = append(changes, Change{Kind: "build", How: how, Path: name,
				Message: fmt.Sprintf("upstream's %s %s", name, what)})
		}
	}
	changes = licenseMove(changes, before, after)
	for i := range changes {
		changes[i].System = project.SystemOf(changes[i].Path)
	}
	return changes
}

// licenseWords say what happened to a license file, by what its text is
// at each end, as licensecheck classifies it: "was MIT, now Apache-2.0",
// or "is still MIT, and only drops text", where "changed" alone asked a
// person to read both (the architecture review's library survey, batch
// 33). Text no license it knows covers is said as it was.
func licenseWords(change Change, old, now []byte) string {
	was, is := change.Licenses[0], change.Licenses[1]
	switch change.How {
	case "added":
		if is.Known() {
			return "was added, " + is.String() + " by its text"
		}
		return "was added"
	case "removed":
		if was.Known() {
			return "was removed, which was " + was.String() + " by its text"
		}
		return "was removed"
	}
	dropped, drops := onlyDrops(old, now)
	switch {
	case was.Known() && is.Known() && !was.Same(is):
		return fmt.Sprintf("was %s, now %s, by its text", was, is)
	case was.Known() && is.Known() && drops:
		return fmt.Sprintf("is still %s by its text, and only drops text beside it, %s", is, dropped)
	case was.Known() && is.Known():
		return fmt.Sprintf("is still %s by its text, and changed beside it", is)
	case was.Known():
		return fmt.Sprintf("was %s by its text, and now reads as no license that's known", was)
	case is.Known():
		return "changed, and now reads as " + is.String()
	case drops:
		return "only drops text, " + dropped
	}
	return "changed"
}

// movedAside is the most lines of license text that may go, beside what
// moved, for a change of license files to be a move: libuv 1.52.1 moved
// its Joyent and tree.h sections out of LICENSE into LICENSE-extra, and
// seven lines naming files it no longer has, and separators, went with
// them (the batch 11 run on #34620).
const movedAside = 10

// licenseMove folds a version's license file changes into one, "moved",
// where its license text moved between files: a license file was added,
// none removed, no line of the new version's license files is new, and at
// most movedAside lines went beside what moved. Lines are compared
// trimmed, blank ones set aside, so text is the same wherever it moved
// to. Removing a license file isn't a move, since dropping one of two
// licenses adds nothing either. Otherwise the changes stand.
func licenseMove(changes []Change, before, after map[string]project.File) []Change {
	var added, from []string
	first := -1
	for i, change := range changes {
		if change.Kind != "license" {
			continue
		}
		if first < 0 {
			first = i
		}
		switch change.How {
		case "added":
			added = append(added, change.Path)
		case "changed":
			from = append(from, change.Path)
		default:
			// A file removed, or one whose years alone changed, isn't
			// text moved.
			return changes
		}
	}
	if len(added) == 0 {
		return changes
	}
	lines := func(files map[string]project.File) (map[string]int, bool) {
		counted := map[string]int{}
		for name, file := range files {
			if !project.LicenseFile(path.Base(name)) {
				continue
			}
			if file.Truncated {
				return nil, false
			}
			for line := range strings.SplitSeq(string(file.Data), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					counted[line]++
				}
			}
		}
		return counted, true
	}
	had, okOld := lines(before)
	has, okNew := lines(after)
	if !okOld || !okNew {
		return changes
	}
	went := 0
	for line, n := range has {
		if n > had[line] {
			return changes
		}
	}
	for line, n := range had {
		went += n - has[line]
	}
	if went > movedAside {
		return changes
	}
	message := fmt.Sprintf("upstream moved license text into %s", strings.Join(added, " and "))
	if len(from) > 0 {
		message += " from " + strings.Join(from, " and ")
	}
	message += ", and none of it is new"
	switch {
	case went == 1:
		message += "; a line went beside it"
	case went > 1:
		message += fmt.Sprintf("; %d lines went beside it", went)
	}
	moved := Change{Kind: "license", How: "moved", Path: added[0], Message: message}
	var folded []Change
	for i, change := range changes {
		switch {
		case i == first:
			folded = append(folded, moved)
		case change.Kind != "license":
			folded = append(folded, change)
		}
	}
	return folded
}

// licenseYear is a year in a license's text, or a range of them, which a
// change of only its years leaves, as yearsOnly has it.
var licenseYear = regexp.MustCompile(`^\(?(19|20)\d\d([-–,](19|20)\d\d)*[),.;:]*$`)

// onlyDrops says what a license file's change drops, where all it does,
// but its years and how its lines wrap, is drop text: entr 5.9's LICENSE
// dropped its "Compatibility Libraries" section, and said only "changed",
// for a look a sentence could have spared (the dogfood run with
// ce6a206d). It still holds, as the person decided: dropping text can
// narrow a license as surely as adding it can, "MIT or GPL-2" losing "MIT
// or". It names how many words went, from the first run of them.
func onlyDrops(old, now []byte) (string, bool) {
	// Years, and the punctuation a word ends with, aren't terms: entr 5.9
	// wrote "Eric Radman, 2012" for "Eric Radman" beside the section it
	// dropped (the dogfood run with 11d1df2f). Each word keeps its place
	// in the old text, to quote from.
	type word struct {
		text string
		at   int
	}
	normal := func(fields []string) []word {
		var words []word
		for i, field := range fields {
			if licenseYear.MatchString(field) {
				continue
			}
			if trimmed := strings.TrimRight(field, ",.;:"); trimmed != "" {
				words = append(words, word{text: trimmed, at: i})
			}
		}
		return words
	}
	original := strings.Fields(string(old))
	before, after := normal(original), normal(strings.Fields(string(now)))
	if len(after) >= len(before) {
		return "", false
	}
	var dropped []int
	j := 0
	for i, w := range before {
		if j < len(after) && w.text == after[j].text {
			j++
			continue
		}
		dropped = append(dropped, i)
	}
	if j < len(after) || len(dropped) == 0 {
		return "", false
	}
	first, end := dropped[0], dropped[0]
	for end+1 < len(before) && slices.Contains(dropped, end+1) && end-first < 15 {
		end++
	}
	quote := strings.Join(original[before[first].at:before[end].at+1], " ")
	if end+1 < len(before) && slices.Contains(dropped, end+1) {
		quote += " …"
	}
	count := fmt.Sprintf("%d words", len(dropped))
	if len(dropped) == 1 {
		count = "1 word"
	}
	return fmt.Sprintf("%s from %q on", count, quotable(quote)), true
}

// quotable is a line as a message quotes it: trimmed, and cut short past
// 120 characters.
func quotable(line string) string {
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > 120 {
		return string(runes[:119]) + "…"
	}
	return line
}

// versionOnly reports whether a build file's two versions differ only in
// the project's version they declare, as nuspell's CMakeLists.txt changed
// only project(nuspell VERSION 5.1.9), and gives the first such line as it
// now reads: each line that differs reads as the new one once the old
// version in it is the new one, and declares the project's version as its
// build system does. A line added or removed, or changed in anything else,
// is a change to the build, as find_package(SomeLibrary 1.0) moving with
// the project's version is (the update-workflow review's finding 2).
func versionOnly(name string, old, now []byte, versions Versions) (string, bool) {
	if versions.Old == "" || versions.New == "" || versions.Old == versions.New {
		return "", false
	}
	before, after := strings.Split(string(old), "\n"), strings.Split(string(now), "\n")
	if len(before) != len(after) {
		return "", false
	}
	first := ""
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		spelled := strings.ReplaceAll(before[i], versions.Old, versions.New) == after[i] && project.DeclaresVersion(name, after[i], versions.New)
		if !spelled && !project.DeclaresVersionPart(name, before[i], after[i], versions.Old, versions.New) {
			return "", false
		}
		if first == "" {
			first = quotable(after[i])
		}
	}
	return first, first != ""
}

// manifestChanges compares what a manifest declares in each version, and
// says what it couldn't read of either.
func manifestChanges(name string, old project.File, hadOld bool, now project.File, hasNow bool) []Change {
	base := path.Base(name)
	unread := func(how, side, what string) Change {
		return Change{Kind: "unread", How: how, Side: side, Path: name, Message: fmt.Sprintf("upstream's %s %s", name, what)}
	}
	var readings [2]reading
	for i, side := range []struct {
		version string
		file    project.File
		present bool
	}{{"old", old, hadOld}, {"new", now, hasNow}} {
		if !side.present {
			continue
		}
		found, err := readManifest(base, side.file.Data)
		if err != nil {
			return []Change{unread("unreadable", side.version, fmt.Sprintf("couldn't be read in the %s version, so its dependencies weren't compared: %v", side.version, err))}
		}
		readings[i] = found
	}
	// What it couldn't follow comes first: in either version, since a gap
	// in the old one leaves what changed as unknown as one in the new. A
	// gap both share is said once.
	var changes []Change
	for _, gap := range readings[1].unread {
		changes = append(changes, unread("unfollowed", "new", gap+", which the comparison doesn't follow"))
	}
	for _, gap := range readings[0].unread {
		if !slices.Contains(readings[1].unread, gap) {
			changes = append(changes, unread("unfollowed", "old", "in the old version "+gap+", which the comparison doesn't follow"))
		}
	}
	return append(changes, dependencyChanges(name, readings[0], readings[1])...)
}

// dependencyDelta is one declared dependency that differs between the
// versions: how, "adds", "drops", or "moves", and its version on each side,
// marked indirect where the build had or keeps it only for another's sake.
type dependencyDelta struct {
	how, name, old, now     string
	wasIndirect, isIndirect bool
}

// dependencyDeltas are what a manifest's declared dependencies gained,
// lost, and moved, by name. A dependency the build had or keeps for
// another's sake, as a Go module required indirectly, is neither gained
// nor lost: it moves only where its version does.
func dependencyDeltas(before, after reading) []dependencyDelta {
	var names []string
	for name := range after.dependencies {
		names = append(names, name)
	}
	for name := range before.dependencies {
		if _, ok := after.dependencies[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var deltas []dependencyDelta
	for _, name := range names {
		old, had := before.dependencies[name]
		now, has := after.dependencies[name]
		delta := dependencyDelta{name: name}
		if !had {
			old, delta.wasIndirect = before.indirect[name]
		}
		if !has {
			now, delta.isIndirect = after.indirect[name]
		}
		delta.old, delta.now = old, now
		switch {
		case !had && !delta.wasIndirect:
			delta.how = "adds"
		case !has && !delta.isIndirect:
			delta.how = "drops"
		case old != now:
			delta.how = "moves"
		default:
			continue
		}
		deltas = append(deltas, delta)
	}
	return deltas
}

// dependencyChanges are what a manifest's declared dependencies gained,
// lost, and moved, one change each, in name order.
func dependencyChanges(file string, before, after reading) []Change {
	spelled := func(version string, indirect bool) string {
		if indirect {
			return version + " (indirect)"
		}
		return version
	}
	var changes []Change
	for _, delta := range dependencyDeltas(before, after) {
		change := Change{Kind: "dependency", How: delta.how, Path: file, Name: delta.name, Old: delta.old, Now: delta.now,
			Requirements: after.requirements[delta.name], Before: before.requirements[delta.name]}
		switch delta.how {
		case "adds":
			change.Message = strings.TrimSpace(fmt.Sprintf("upstream: %s adds %s %s", file, delta.name, delta.now))
		case "drops":
			change.Message = fmt.Sprintf("upstream: %s drops %s", file, delta.name)
		default:
			change.Message = fmt.Sprintf("upstream: %s moves %s from %s to %s", file, delta.name, spelled(delta.old, delta.wasIndirect), spelled(delta.now, delta.isIndirect))
		}
		changes = append(changes, change)
	}
	return changes
}

// cmakeNamed is how many of a CMakeLists.txt's changes are said before
// the rest are counted.
const cmakeNamed = 5

// cmakeWords say what a CMakeLists.txt's change does to the options it
// offers and the packages it finds, as ": option FLB_KAFKA added, off by
// default; find_package(OpenSSL) added, under FLB_TLS", or that it changes
// neither: fluent-bit's "CMakeLists.txt changed" sent the person to the
// diff, where nothing concerned the port (the fluent-bit run, batch 23).
// It says; what holds is assess's, and D12's: any other change to a build
// file still holds.
// autoconfWords say what a configure.ac's change asks of the build, as
// its macros name it: ": --enable-gui added; pkg-config module gtk4
// added", or nothing where none moved (field testing, batch 11:
// dateutils's "configure.ac changed" held with nothing to act on).
func autoconfWords(old, now project.AutoconfFacts) string {
	var said []string
	moved := func(before, after []string, what string) {
		for _, name := range after {
			if !slices.Contains(before, name) {
				said = append(said, what+name+" added")
			}
		}
		for _, name := range before {
			if !slices.Contains(after, name) {
				said = append(said, what+name+" removed")
			}
		}
	}
	moved(old.Enables, now.Enables, "--enable-")
	moved(old.Withs, now.Withs, "--with-")
	moved(old.Modules, now.Modules, "pkg-config module ")
	moved(old.Libraries, now.Libraries, "library ")
	if len(said) == 0 {
		return ", in nothing it names as an option, a pkg-config module, or a library"
	}
	return ": " + strings.Join(said, "; ")
}

func cmakeWords(old, now project.CMakeDocument) string {
	before, after := old.Facts(), now.Facts()
	var said []string
	for _, name := range slices.Sorted(maps.Keys(after.Options)) {
		option, was := after.Options[name], before.Options[name]
		switch _, had := before.Options[name]; {
		case !had:
			by := option.Default
			if by == "ON" || by == "OFF" {
				by = strings.ToLower(by)
			}
			said = append(said, fmt.Sprintf("option %s added, %s by default", name, by))
		case was.Default != option.Default:
			said = append(said, fmt.Sprintf("option %s's default moves from %s to %s", name, was.Default, option.Default))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(before.Options)) {
		if _, has := after.Options[name]; !has {
			said = append(said, "option "+name+" removed")
		}
	}
	find := func(packages []project.CMakePackage, name string) (project.CMakePackage, bool) {
		for _, pkg := range packages {
			if pkg.Name == name {
				return pkg, true
			}
		}
		return project.CMakePackage{}, false
	}
	under := func(pkg project.CMakePackage) string {
		if len(pkg.Under) == 0 {
			return ""
		}
		return ", under " + strings.Join(pkg.Under, " and ")
	}
	seen := map[string]bool{}
	for _, pkg := range after.Packages {
		if seen[pkg.Name] {
			continue
		}
		seen[pkg.Name] = true
		was, had := find(before.Packages, pkg.Name)
		switch {
		case !had:
			said = append(said, fmt.Sprintf("find_package(%s) added%s", pkg.Name, under(pkg)))
		case was.Version != pkg.Version && pkg.Version != "":
			said = append(said, fmt.Sprintf("find_package(%s) now asks for %s%s", pkg.Name, pkg.Version, under(pkg)))
		case !was.Required && pkg.Required:
			said = append(said, fmt.Sprintf("find_package(%s) is now REQUIRED%s", pkg.Name, under(pkg)))
		case strings.Join(was.Under, " and ") != strings.Join(pkg.Under, " and "):
			said = append(said, fmt.Sprintf("find_package(%s) moves%s", pkg.Name, cmp.Or(under(pkg), ", out of its if()")))
		}
	}
	for _, pkg := range before.Packages {
		if _, has := find(after.Packages, pkg.Name); !has && !seen[pkg.Name] {
			seen[pkg.Name] = true
			said = append(said, fmt.Sprintf("find_package(%s) dropped", pkg.Name))
		}
	}
	switch {
	case len(said) == 0:
		return ", though no option or find_package did"
	case len(said) > cmakeNamed:
		return fmt.Sprintf(": %s; and %d more", strings.Join(said[:cmakeNamed], "; "), len(said)-cmakeNamed)
	}
	return ": " + strings.Join(said, "; ")
}

// cmakeElsewhere says where a CMakeLists.txt changed beside what it
// declares, its options and the packages it finds, among what the default
// build reaches, by the if() each change is under, so a hold names what
// it's for: fluent-bit 5.1.3's read as held for FLB_PROTOBUF_ENCODER,
// which the person had just decided holds nothing, where it held for
// lines under if(FLB_ALL) and a block re-gated on FLB_AVRO_ENCODER OR
// FLB_PROTOBUF_ENCODER (the dogfood run with 11d1df2f). The version it
// names isn't a change, nor are its comments.
func cmakeElsewhere(old, now project.CMakeDocument, versions Versions, named func(string) bool) string {
	return cmakePlaces(cmakeWhere(versioned(old.Rest(old.Off(named)), versions), now.Rest(now.Off(named))))
}

// versioned are statements with the version an update moves from spelled
// as the one it moves to, whole or in parts, so the version they name
// isn't a change.
func versioned(statements []project.CMakeStatement, versions Versions) []project.CMakeStatement {
	spelled := make([]project.CMakeStatement, len(statements))
	for i, statement := range statements {
		statement.Text = withVersion(statement.Text, versions)
		statement.Under = slices.Clone(statement.Under)
		for j := range statement.Under {
			statement.Under[j].Text = withVersion(statement.Under[j].Text, versions)
		}
		spelled[i] = statement
	}
	return spelled
}

// withVersion is a CMakeLists.txt's text with the version an update moves
// from spelled as the one it moves to, whole or in parts, so the version
// it names isn't a change.
func withVersion(text string, versions Versions) string {
	if versions.Old != "" && versions.New != "" {
		return project.VersionPartsAs(strings.ReplaceAll(text, versions.Old, versions.New), versions.Old, versions.New)
	}
	return text
}

// cmakePlace is where a CMakeLists.txt changes: the outermost if() its
// statements are under, empty for none, as a person finds the block, the
// file it's in, empty for the CMakeLists.txt itself, and each if() they're
// within.
type cmakePlace struct {
	Condition string
	File      string
	Within    []project.CMakeCondition
}

// cmakeWhere are where a CMakeLists.txt's changed statements are, each
// place once, in the order the new document has them and then the old.
// Statements are compared with the conditions they're under, as the
// document reads them, so an endif() pairs only with its own block's:
// fluent-bit 5.1.3's blocks added after if(FLB_UTF8_ENCODER)'s read as
// under it, its endif() paired with theirs (the dogfood run with
// 91340a56).
func cmakeWhere(was, rest []project.CMakeStatement) []cmakePlace {
	keys := func(statements []project.CMakeStatement) []string {
		keyed := make([]string, len(statements))
		for i, statement := range statements {
			var under []string
			for _, condition := range statement.Under {
				under = append(under, condition.Text)
			}
			keyed[i] = statement.File + "\x02" + strings.Join(under, "\x00") + "\x01" + statement.Text
		}
		return keyed
	}
	gone, come := changedLines(keys(was), keys(rest))
	var where []cmakePlace
	for _, changed := range []struct {
		statements []project.CMakeStatement
		at         []int
	}{{rest, come}, {was, gone}} {
		for _, i := range changed.at {
			statement := changed.statements[i]
			place := cmakePlace{File: statement.File, Within: statement.Under}
			if len(statement.Under) > 0 {
				place.Condition = statement.Under[0].Text
			}
			at := slices.IndexFunc(where, func(p cmakePlace) bool { return p.Condition == place.Condition && p.File == place.File })
			if at < 0 {
				where = append(where, place)
				continue
			}
			for _, condition := range statement.Under {
				if !slices.ContainsFunc(where[at].Within, func(c project.CMakeCondition) bool { return c.Text == condition.Text }) {
					where[at].Within = append(where[at].Within, condition)
				}
			}
		}
	}
	return where
}

// cmakePlaces says the conditions changes are under, "under if(FLB_ALL)"
// or "outside any if()", with the file a change is in where it's one the
// CMakeLists.txt include()s, the first five, and how many more.
func cmakePlaces(places []cmakePlace) string {
	var where []string
	for _, place := range places {
		words := "outside any if()"
		if place.Condition != "" {
			words = "under if(" + place.Condition + ")"
		}
		if place.File != "" {
			words += " in " + place.File
		}
		where = append(where, words)
	}
	if len(where) > cmakeNamed {
		return fmt.Sprintf("%s, and in %d more places", strings.Join(where[:cmakeNamed], ", "), len(where)-cmakeNamed)
	}
	return strings.Join(where, ", ")
}

// changedLines are the lines of each side a longest common subsequence
// leaves out: those gone from the old, and those come in the new. Past
// what's cheap to compare line by line, the lines between the common
// start and end are all said.
func changedLines(old, now []string) (gone, come []int) {
	start := 0
	for start < len(old) && start < len(now) && old[start] == now[start] {
		start++
	}
	endOld, endNew := len(old), len(now)
	for endOld > start && endNew > start && old[endOld-1] == now[endNew-1] {
		endOld--
		endNew--
	}
	a, b := old[start:endOld], now[start:endNew]
	if len(a)*len(b) > 4_000_000 {
		for i := range a {
			gone = append(gone, start+i)
		}
		for j := range b {
			come = append(come, start+j)
		}
		return gone, come
	}
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i++
			j++
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			come = append(come, start+j)
			j++
		default:
			gone = append(gone, start+i)
			i++
		}
	}
	return gone, come
}

// cmakeOptionsOnly says a CMakeLists.txt's change where all it does that
// the default build reads, but the version it names, is add options, each
// built as its default: the person decided an added option holds nothing,
// and that what the default build doesn't reach holds nothing, as a
// branch whose condition is false where each option off by default that
// nothing turns on is OFF (project.CMakeOff): fluent-bit 5.1.3's
// find_package(Protobuf) under FLB_PROTOBUF_ENCODER, and its lines under
// if(FLB_ALL) (D12, revisited 2026-10-01). What an option on by default
// gates holds, as do a default that flips and an option removed; so does
// a change under an option the Portfile names, in any variant, or the
// file sets. False where anything else changed.
func cmakeOptionsOnly(name string, old, now project.CMakeDocument, versions Versions, named func(string) bool) (string, bool) {
	before, after := old.Facts(), now.Facts()
	added := map[string]bool{}
	for option := range after.Options {
		if _, had := before.Options[option]; !had {
			added[option] = true
		}
	}
	offOld, offNew := old.Off(named), now.Off(named)
	rest, was := project.StatementsText(now.Without(added, offNew)), project.StatementsText(old.Without(nil, offOld))
	if rest != was {
		if _, ok := versionOnly(name, []byte(was), []byte(rest), versions); !ok {
			return "", false
		}
	}
	var said []string
	for _, option := range slices.Sorted(maps.Keys(added)) {
		by := after.Options[option].Default
		if by == "ON" || by == "OFF" {
			by = strings.ToLower(by)
		}
		words := fmt.Sprintf("option %s, %s by default", option, by)
		var gated []string
		for _, pkg := range after.Packages {
			if offNew[option] && slices.Contains(pkg.Under, option) {
				gated = append(gated, "find_package("+pkg.Name+")")
			}
		}
		if len(gated) > 0 {
			words += ", which gates " + strings.Join(gated, ", ")
		}
		said = append(said, words)
	}
	// Where else it changed is said, with what leaves it unreached; what
	// an added option's own block gates is said with the option.
	addedOff := map[string]bool{}
	for option := range added {
		if offNew[option] {
			addedOff[option] = true
		}
	}
	places := cmakeWhere(versioned(old.Without(nil, nil), versions), now.Without(added, addedOff))
	var words []string
	if len(said) > 0 {
		words = append(words, "adds "+strings.Join(said, "; "))
	}
	if len(places) > 0 {
		// The options named are those off that an if() over a change
		// tests, an else()'s negated test aside.
		tested := func(option string) bool {
			return slices.ContainsFunc(places, func(place cmakePlace) bool {
				return slices.ContainsFunc(place.Within, func(condition project.CMakeCondition) bool { return condition.Tests(option) })
			})
		}
		var gates []string
		for option := range offNew {
			if tested(option) {
				gates = append(gates, option)
			}
		}
		for option := range offOld {
			if !offNew[option] && tested(option) {
				gates = append(gates, option)
			}
		}
		slices.Sort(gates)
		reach := "changes only what the default build doesn't reach, " + cmakePlaces(places)
		if len(gates) > 0 {
			reach += ": neither it nor the Portfile turns " + orList(gates) + " on"
		}
		if len(words) > 0 {
			reach = "otherwise " + reach
		}
		words = append(words, reach)
	}
	switch {
	case len(words) == 0:
		return "changes only comments and layout, which the build doesn't read", true
	case len(places) == 0:
		words[0] += ", and changes nothing else the default build reads"
	}
	what := strings.Join(words, "; ")
	if len(said) > 0 {
		what += "; each option builds as its default"
	}
	return what, true
}

// orList joins words as a sentence lists alternatives: "A", "A or B", "A,
// B, or C".
func orList(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " or " + words[1]
	}
	return strings.Join(words[:len(words)-1], ", ") + ", or " + words[len(words)-1]
}

// subset reports whether every version in some is in all.
func subset(some, all []string) bool {
	return !slices.ContainsFunc(some, func(v string) bool { return !slices.Contains(all, v) })
}

// lockMoves are what a Cargo.lock changes of the crates it pins from
// elsewhere, each crate once: added, dropped, or moved to other versions,
// which assess counts in one line, holding nothing (D9). A workspace's own
// crates, which move with its release, aren't counted. rust 1.99.0's
// Cargo.lock changed much, and its assessment said nothing of it, since
// only crates linking a native library were looked for (rust 1.99.0,
// batch 23).
func lockMoves(name string, earlier, packages []project.CargoPackage) []Change {
	versions := func(packages []project.CargoPackage) map[string][]string {
		found := map[string][]string{}
		for _, pkg := range packages {
			if pkg.Source != project.FromLocal {
				found[pkg.Name] = append(found[pkg.Name], pkg.Version)
			}
		}
		for crate := range found {
			slices.Sort(found[crate])
		}
		return found
	}
	before, now := versions(earlier), versions(packages)
	var changes []Change
	for _, crate := range slices.Sorted(maps.Keys(now)) {
		old, had := before[crate]
		switch {
		case !had:
			changes = append(changes, Change{Kind: "dependency", How: "adds", Path: name, Name: crate, Now: strings.Join(now[crate], ", "),
				Message: fmt.Sprintf("upstream: %s adds %s %s", name, crate, strings.Join(now[crate], ", "))})
		case slices.Equal(old, now[crate]):
		case subset(old, now[crate]):
			// Another version beside those it pinned is an addition: skim's
			// nix 0.30.1, beside 0.28, 0.29, and 0.31, read as a move
			// (field testing, 2026-10-02).
			added := slices.DeleteFunc(slices.Clone(now[crate]), func(v string) bool { return slices.Contains(old, v) })
			changes = append(changes, Change{Kind: "dependency", How: "adds", Path: name, Name: crate, Now: strings.Join(added, ", "),
				Message: fmt.Sprintf("upstream: %s adds %s %s, beside %s", name, crate, strings.Join(added, ", "), strings.Join(old, ", "))})
		case subset(now[crate], old):
			dropped := slices.DeleteFunc(slices.Clone(old), func(v string) bool { return slices.Contains(now[crate], v) })
			changes = append(changes, Change{Kind: "dependency", How: "drops", Path: name, Name: crate, Old: strings.Join(dropped, ", "),
				Message: fmt.Sprintf("upstream: %s drops %s %s, keeping %s", name, crate, strings.Join(dropped, ", "), strings.Join(now[crate], ", "))})
		default:
			changes = append(changes, Change{Kind: "dependency", How: "moves", Path: name, Name: crate, Old: strings.Join(old, ", "), Now: strings.Join(now[crate], ", "),
				Message: fmt.Sprintf("upstream: %s moves %s from %s to %s", name, crate, strings.Join(old, ", "), strings.Join(now[crate], ", "))})
		}
	}
	for _, crate := range slices.Sorted(maps.Keys(before)) {
		if _, has := now[crate]; !has {
			changes = append(changes, Change{Kind: "dependency", How: "drops", Path: name, Name: crate, Old: strings.Join(before[crate], ", "),
				Message: fmt.Sprintf("upstream: %s drops %s", name, crate)})
		}
	}
	return changes
}

// nativeLinks lists the crates new to a Cargo.lock that link a native
// library, as Cargo's -sys crates do. Such a crate often links a copy of
// the library it finds installed, and builds one it bundles otherwise,
// which a clean check can't tell apart: where MacPorts has the library,
// the Portfile may want to declare it, which is assess's to say. It lists
// those gone from it too,
// "unlinked": what the Portfile declared for the library may be left.
func nativeLinks(name string, old project.File, hadOld bool, now project.File, hasNow bool) []Change {
	if !hasNow {
		return nil
	}
	unread := func(version string, err error) []Change {
		return []Change{{Kind: "unread", How: "unreadable", Side: version, Path: name, Message: fmt.Sprintf("upstream's Cargo.lock couldn't be read in the %s version, so the crates new to it that link a native library weren't looked for: %v", version, err)}}
	}
	packages, err := project.ReadCargoLock(now.Data)
	if err != nil {
		return unread("new", err)
	}
	had, has := map[string]bool{}, map[string]bool{}
	var earlier []project.CargoPackage
	if hadOld {
		earlier, err = project.ReadCargoLock(old.Data)
		if err != nil {
			return unread("old", err)
		}
		for _, pkg := range earlier {
			had[pkg.Name] = true
		}
	}
	for _, pkg := range packages {
		has[pkg.Name] = true
	}
	changes := lockMoves(name, earlier, packages)
	for _, pkg := range packages {
		library := pkg.NativeLibrary()
		if library == "" || had[pkg.Name] {
			continue
		}
		// Once, whichever versions the lock pins.
		had[pkg.Name] = true
		changes = append(changes, Change{Kind: "dependency", How: "native", Path: name, Name: pkg.Name, Now: pkg.Version,
			Message: fmt.Sprintf("upstream: Cargo.lock adds %s %s, which links the native library %s", pkg.Name, pkg.Version, library)})
	}
	for _, pkg := range earlier {
		library := pkg.NativeLibrary()
		if library == "" || has[pkg.Name] {
			continue
		}
		has[pkg.Name] = true
		changes = append(changes, Change{Kind: "dependency", How: "unlinked", Path: name, Name: pkg.Name, Old: pkg.Version,
			Message: fmt.Sprintf("upstream: Cargo.lock drops %s, which linked the native library %s", pkg.Name, library)})
	}
	return changes
}

// rDescription is an R package's DESCRIPTION's change as its build reads
// it: its dependency fields changed, which holds as a build file's change
// does, or not, which holds nothing.
func rDescription(name string, old, now []byte) Change {
	before, after := project.RDependencies(old), project.RDependencies(now)
	var moved []string
	for _, field := range project.RDependencyFields {
		was, had := before[field]
		is, has := after[field]
		switch {
		case was == is && had == has:
		case !had:
			moved = append(moved, fmt.Sprintf("%s is new: %s", field, is))
		case !has:
			moved = append(moved, fmt.Sprintf("%s was removed, which was %s", field, was))
		default:
			moved = append(moved, fmt.Sprintf("%s was %s, now %s", field, was, is))
		}
	}
	if len(moved) == 0 {
		return Change{Kind: "build", How: "version", Path: name,
			Message: fmt.Sprintf("upstream's %s changed, but not its %s", name, strings.Join(project.RDependencyFields, ", "))}
	}
	return Change{Kind: "build", How: "changed", Path: name, Message: fmt.Sprintf("upstream's %s changed: %s", name, strings.Join(moved, "; "))}
}
