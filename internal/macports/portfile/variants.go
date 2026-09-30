package portfile

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// archiveOptions are the options whose declaration in a variant's body
// changes what the port fetches when the variant is asked for.
var archiveOptions = []string{"distfiles", "checksums", "master_sites"}

// ArchiveVariants are the variants the Portfile defines whose own body
// declares what the port fetches, its distfiles, their checksums, or where
// they come from, in the order the Portfile defines them: git's +doc,
// which appends git-htmldocs. It reads the Portfile's text only, to find
// which variants MacPorts should be asked about; what each fetches is
// MacPorts' to say. A variant a PortGroup defines, or a body that reaches
// its declarations through a procedure, isn't named.
func ArchiveVariants(src []byte) []string {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil
	}
	var variants []string
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		if name, _ := cmd.Name(src); name != "variant" || len(cmd.Words) < 3 {
			continue
		}
		variant, ok := cmd.Words[1].Literal(src)
		body, braced := cmd.Words[len(cmd.Words)-1].BracedScript(src)
		if !ok || !braced || slices.Contains(variants, variant) {
			continue
		}
		for inner := range body.Commands(src, func(syntax.Command) bool { return true }) {
			name, _ := inner.Name(src)
			option, _, _ := strings.Cut(name, "-")
			if slices.Contains(archiveOptions, option) {
				variants = append(variants, variant)
				break
			}
		}
	}
	return variants
}
