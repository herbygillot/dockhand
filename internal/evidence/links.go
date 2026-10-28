package evidence

import (
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// LinkFinding is a target's build linking a library from a port it doesn't
// declare in depends_lib (model.FindingUndeclaredLink, v0.5.0 part 6), or,
// where Unread, a build whose links couldn't be read, so nothing is said of
// them: never that it links nothing; or, where Clean, a target whose links
// were read and each declared, so the scan is seen to have run (field
// testing's crystal 1.22.1, after rc3). A notice; it holds nothing.
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
			if link.Port == "" || slices.ContainsFunc(build.Declared, func(name string) bool { return strings.EqualFold(name, link.Port) }) {
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
		findings = append(findings, *finding)
		undeclared[k.target] = true
	}
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
		return strings.Compare(strings.ToLower(a.Port), strings.ToLower(b.Port))
	})
	return findings
}
