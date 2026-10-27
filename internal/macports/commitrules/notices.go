package commitrules

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// CheckNotices says the review survey's other lint-shaped findings on the
// lines a change touches (§8, §10, and §11), each a notice holding
// nothing: a license named by its SPDX identifier where MacPorts names it
// otherwise, supported_archs noarch without any in platforms,
// commented-out Tcl, and a file added under files/ that the Portfile
// doesn't name.
func CheckNotices(portfiles []Portfile) []Finding {
	var findings []Finding
	for _, p := range portfiles {
		if p.After == "" {
			continue
		}
		before, after := []byte(p.Before), []byte(p.After)
		for _, line := range portfile.AddedLines(before, after) {
			if line.Option != "license" {
				continue
			}
			for _, named := range macports.SPDXNames(strings.TrimSpace(strings.TrimPrefix(line.Text, "license"))) {
				findings = append(findings, Finding{Code: "license-spdx", Severity: Warning, Where: fmt.Sprintf("%s:%d", p.Path, line.Line),
					Message: fmt.Sprintf("license %s is SPDX's name; MacPorts' is %s", named.SPDX, named.Name)})
			}
		}
		if line, touched, ok := portfile.NoarchWithoutAny(before, after); ok && touched {
			platforms := "any"
			if references, _ := portfile.PortGroupReferences(after); slices.ContainsFunc(references, func(group macports.PortGroup) bool { return group.Name == "python" }) {
				platforms = "{darwin any}"
			}
			findings = append(findings, Finding{Code: "noarch-platforms", Severity: Warning, Where: fmt.Sprintf("%s:%d", p.Path, line),
				Message: fmt.Sprintf("supported_archs noarch builds nothing for an architecture, so platforms says %s, which MacPorts' buildbots build once", platforms)})
		}
		for _, line := range portfile.CommentedCode(before, after) {
			findings = append(findings, Finding{Code: "commented-code", Severity: Warning, Where: fmt.Sprintf("%s:%d", p.Path, line.Line),
				Message: fmt.Sprintf("%q is commented-out code; a reviewer asks for it to go, or for a comment saying why it stays", line.Text)})
		}
		for _, file := range p.Files {
			if !portfile.NamesFile(after, file) {
				findings = append(findings, Finding{Code: "unreferenced-file", Severity: Warning, Where: path.Join(path.Dir(p.Path), "files", file),
					Message: "the change adds it, and the Portfile doesn't name it"})
			}
		}
	}
	return findings
}
