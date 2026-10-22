package evidence

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// LinkFinding is a target's build linking a library from a port it doesn't
// declare in depends_lib (model.FindingUndeclaredLink, v0.5.0 part 6), or,
// where Unread, a build whose links couldn't be read, so nothing is said of
// them: never that it links nothing. A notice; it holds nothing.
type LinkFinding struct {
	Target model.TargetID
	// Port is the port the target links and doesn't declare; Library and
	// Binary the first of its libraries the target links, and the target's
	// file that links it, with Libraries counting them all.
	Port, Library, Binary string
	Libraries             int
	Unread                bool
}

// Subject is what the finding is about, as a decision on it names it:
// "harbor links oniguruma".
func (f LinkFinding) Subject() string { return string(f.Target) + " links " + f.Port }

// UndeclaredLinks are what a check's results link that their ports don't
// declare (v0.5.0 part 6): for each target, every port its passed builds
// link a library from but for those declared names, which are the ports
// in its depends_lib, itself, and its Portfile's other subports, as plain
// names; a library no port provides is no port to declare. A passed result
// whose links weren't read says so, once a target. In target order, then
// by port.
func UndeclaredLinks(results []model.TargetResult, declared map[model.TargetID][]string) []LinkFinding {
	type key struct {
		target model.TargetID
		port   string
	}
	found := map[key]*LinkFinding{}
	libraries := map[key]map[string]bool{}
	read, passed := map[model.TargetID]bool{}, map[model.TargetID]bool{}
	for _, result := range results {
		if result.Outcome != model.OutcomePassed {
			continue
		}
		passed[result.Target] = true
		if result.Links == nil {
			continue
		}
		read[result.Target] = true
		for _, link := range result.Links {
			if link.Port == "" || slices.ContainsFunc(declared[result.Target], func(name string) bool { return strings.EqualFold(name, link.Port) }) {
				continue
			}
			k := key{result.Target, strings.ToLower(link.Port)}
			if found[k] == nil {
				found[k] = &LinkFinding{Target: result.Target, Port: link.Port, Library: link.Library, Binary: link.Binary}
				libraries[k] = map[string]bool{}
			}
			libraries[k][link.Library] = true
		}
	}
	var findings []LinkFinding
	for k, finding := range found {
		finding.Libraries = len(libraries[k])
		findings = append(findings, *finding)
	}
	// A target read in one environment and not another is read.
	for target := range passed {
		if !read[target] {
			findings = append(findings, LinkFinding{Target: target, Unread: true})
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
