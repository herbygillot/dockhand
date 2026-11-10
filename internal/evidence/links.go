package evidence

import (
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// LinkFinding is a target's build linking a library from a port it doesn't
// declare in depends_lib (v0.5.0 part 6), or, where Unread, a build whose
// links couldn't be read, so nothing is said of them: never that it links
// nothing; or, where Clean, a target whose links were read and each
// declared, so the scan is seen to have run (field testing's crystal
// 1.22.1, after rc3). A notice; it holds nothing.
type LinkFinding struct {
	Target model.TargetID
	// Port is the port the target links and doesn't declare; Library and
	// Binary the first of its libraries the target links, and the target's
	// file that links it, with Libraries counting them all.
	Port, Library, Binary string
	Libraries             int
	// Environments are the builds' environments where it links the port
	// and doesn't declare it, in the order given (Codex's review of
	// 8c007979, C8-2: a declaration in one hid another's link).
	Environments []model.Environment
	Unread       bool
	// Reason is why an Unread target's links weren't read, as its first
	// unread build says it: five "couldn't read" lines said no reason (the
	// exiv2 session's notes, 2026-10-24).
	Reason string
	// Clean is a target whose read builds link only ports it declares,
	// Ports naming them, the target's own Portfile's left out: ace's
	// notice counted ace itself (the ace update, 2026-10-28).
	Clean bool
	Ports []string
	// Unlinked is a port the target's own Portfile declares in depends_lib,
	// Port, at Option and Line, that installs a library none of its
	// builds links, where each build installed a Mach-O file: a notice,
	// never a hold, since a library loaded with dlopen is the maintainer's
	// to keep (the abook session's libiconv).
	Unlinked bool
	Option   string
	Line     int
	// Through is how an undeclared link came, where a declared port's
	// pkg-config modules name its library: gnome-voice links cairo through
	// gtk4's Libs, gtk4 requiring cairo (gnome-voice's note 9).
	Through *LinkThrough
}

// LinkThrough is the declared port whose pkg-config brought a library in,
// and the modules followed from its own to the one whose Libs names it.
type LinkThrough struct {
	Port    string
	Modules []string
}

// WrittenLib is a port a Portfile's own text declares in depends_lib: the
// port, the option that declares it, and its line.
type WrittenLib struct {
	Port, Option string
	Line         int
}

// Subject is what the finding is about, as a decision on it names it:
// "harbor links oniguruma".
func (f LinkFinding) Subject() string { return string(f.Target) + " links " + f.Port }

// LinkedBuild is one passed build as the link rule reads it: its result,
// the environment it built in, and the ports it declares there, its
// depends_lib as that environment evaluates it, itself, and its
// Portfile's other subports, as plain names.
type LinkedBuild struct {
	Result      model.TargetResult
	Environment model.Environment
	Declared    []string
	// Own are the target's own Portfile's ports, itself among them, which
	// it links as its own and no other port's.
	Own []string
	// Unread is why the build's links weren't read, where they weren't.
	Unread string
	// Written are the ports the target's own Portfile declares in
	// depends_lib, by its text, which alone an unlinked finding names:
	// a dependency a PortGroup adds is never said.
	Written []WrittenLib
}

// UndeclaredLinks are what a check's builds link that their ports don't
// declare (v0.5.0 part 6): each build judged against its own environment's
// declarations, since a port's dependencies can differ by architecture or
// release, then those of one target and port combined, naming the
// environments; a library no port provides is no port to declare. A target
// none of whose passed builds' links were read says so, once; one read in
// any environment is read. In target order, then by port.
func UndeclaredLinks(builds []LinkedBuild) []LinkFinding {
	type key struct {
		target model.TargetID
		port   string
	}
	found := map[key]*LinkFinding{}
	libraries := map[key]map[string]bool{}
	read, passed := map[model.TargetID]bool{}, map[model.TargetID]bool{}
	reasons := map[model.TargetID]string{}
	linked := map[model.TargetID]map[string]bool{}
	for _, build := range builds {
		result := build.Result
		if result.Outcome != model.OutcomePassed {
			continue
		}
		passed[result.Target] = true
		if result.Links == nil {
			if reasons[result.Target] == "" {
				reasons[result.Target] = build.Unread
			}
			continue
		}
		read[result.Target] = true
		if linked[result.Target] == nil {
			linked[result.Target] = map[string]bool{}
		}
		for _, link := range result.Links {
			if link.Port != "" && !slices.ContainsFunc(build.Own, func(name string) bool { return strings.EqualFold(name, link.Port) }) {
				linked[result.Target][link.Port] = true
			}
			// A port a path: dependency reaches through its links is
			// declared by it, as curl's path:lib/libssl.dylib:openssl
			// declares openssl3.
			if link.Port == "" || link.Declared != "" || slices.ContainsFunc(build.Declared, func(name string) bool { return strings.EqualFold(name, link.Port) }) {
				continue
			}
			k := key{result.Target, strings.ToLower(link.Port)}
			if found[k] == nil {
				found[k] = &LinkFinding{Target: result.Target, Port: link.Port, Library: link.Library, Binary: link.Binary}
				libraries[k] = map[string]bool{}
			}
			libraries[k][link.Library] = true
			if !slices.Contains(found[k].Environments, build.Environment) {
				found[k].Environments = append(found[k].Environments, build.Environment)
			}
		}
	}
	var findings []LinkFinding
	undeclared := map[model.TargetID]bool{}
	for k, finding := range found {
		finding.Libraries = len(libraries[k])
		finding.Through = through(builds, finding.Target, finding.Library, finding.Environments)
		findings = append(findings, *finding)
		undeclared[k.target] = true
	}
	findings = append(findings, unlinked(builds)...)
	for target := range passed {
		switch {
		case !read[target]:
			findings = append(findings, LinkFinding{Target: target, Unread: true, Reason: reasons[target]})
		case !undeclared[target]:
			ports := slices.Collect(maps.Keys(linked[target]))
			slices.SortFunc(ports, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
			findings = append(findings, LinkFinding{Target: target, Clean: true, Ports: ports})
		}
	}
	slices.SortFunc(findings, func(a, b LinkFinding) int {
		if c := strings.Compare(string(a.Target), string(b.Target)); c != 0 {
			return c
		}
		if a.Unlinked != b.Unlinked {
			if a.Unlinked {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(a.Port), strings.ToLower(b.Port))
	})
	return findings
}

// unlinked are the declarations nothing links (unlinked-dependency): a
// port D a target's own Portfile declares in depends_lib is said where
// (1) its text declares it, (2) D installs a dynamic library, (3) each
// build installed a Mach-O file, and (4) no build, in any environment,
// links a library D installs. A target any of whose passed builds' links
// or declared ports weren't read says nothing of it.
func unlinked(builds []LinkedBuild) []LinkFinding {
	type seen struct {
		written   []WrittenLib
		libraries map[string]map[string]bool
		linked    map[string]bool
		machos    bool
		unread    bool
	}
	targets := map[model.TargetID]*seen{}
	var order []model.TargetID
	for _, build := range builds {
		result := build.Result
		if result.Outcome != model.OutcomePassed {
			continue
		}
		t := targets[result.Target]
		if t == nil {
			t = &seen{libraries: map[string]map[string]bool{}, linked: map[string]bool{}, machos: true}
			targets[result.Target] = t
			order = append(order, result.Target)
		}
		if result.Links == nil || result.Declared == nil {
			t.unread = true
			continue
		}
		t.written = build.Written
		t.machos = t.machos && result.Declared.MachOs > 0
		for _, port := range result.Declared.Ports {
			name := strings.ToLower(port.Port)
			if t.libraries[name] == nil {
				t.libraries[name] = map[string]bool{}
			}
			for _, library := range port.Libraries {
				t.libraries[name][library] = true
			}
		}
		for _, link := range result.Links {
			t.linked[strings.ToLower(link.Port)] = true
			t.linked["\x00"+link.Library] = true
		}
	}
	var findings []LinkFinding
	for _, target := range order {
		t := targets[target]
		if t.unread || !t.machos {
			continue
		}
		said := map[string]bool{}
		for _, written := range t.written {
			name := strings.ToLower(written.Port)
			libraries := t.libraries[name]
			if said[name] || len(libraries) == 0 || t.linked[name] {
				continue
			}
			if slices.ContainsFunc(slices.Collect(maps.Keys(libraries)), func(library string) bool { return t.linked["\x00"+library] }) {
				continue
			}
			said[name] = true
			findings = append(findings, LinkFinding{Target: target, Port: written.Port, Unlinked: true, Option: written.Option, Line: written.Line})
		}
	}
	return findings
}

// through is how a target's builds in these environments, those that made
// the finding, came to link library, where a port it
// declares has a pkg-config module whose Libs names it, directly or by
// Requires: followed module to module among the declared ports' modules;
// nil where none does.
func through(builds []LinkedBuild, target model.TargetID, library string, environments []model.Environment) *LinkThrough {
	stem := libraryStem(library)
	if stem == "" {
		return nil
	}
	for _, build := range builds {
		// Only the builds that made the finding explain it: an arm64
		// finding took an x86_64 build's pkg-config path (Codex's sixth
		// review, #77).
		if build.Result.Target != target || build.Result.Declared == nil || !slices.Contains(environments, build.Environment) {
			continue
		}
		modules := map[string]model.PkgConfigModule{}
		for _, port := range build.Result.Declared.Ports {
			for _, module := range port.Modules {
				if _, ok := modules[module.Name]; !ok {
					modules[module.Name] = module
				}
			}
		}
		for _, port := range build.Result.Declared.Ports {
			for _, start := range port.Modules {
				if path := modulePath(modules, start, stem, nil); path != nil {
					return &LinkThrough{Port: port.Port, Modules: path}
				}
			}
		}
	}
	return nil
}

// modulePath is the modules from module to one whose Libs names stem,
// following Requires:, each module once; nil where none does.
func modulePath(modules map[string]model.PkgConfigModule, module model.PkgConfigModule, stem string, visited []string) []string {
	if slices.Contains(visited, module.Name) {
		return nil
	}
	visited = append(visited, module.Name)
	if slices.Contains(module.Libs, stem) {
		return visited
	}
	for _, name := range module.Requires {
		if next, ok := modules[name]; ok {
			if path := modulePath(modules, next, stem, visited); path != nil {
				return path
			}
		}
	}
	return nil
}

// libraryStem is the -l name a library's file answers to: libcairo.2.dylib
// is cairo; empty for a file not named lib<name>.
func libraryStem(library string) string {
	base := library[strings.LastIndexByte(library, '/')+1:]
	name, ok := strings.CutPrefix(base, "lib")
	if !ok {
		return ""
	}
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	return name
}
