package portfile

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
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
	// Only the commands MacPorts runs (commands), never data: a variant
	// written inside a string, or a checksums inside a variant's set, is
	// neither a variant nor a declaration (the helper-ownership review's
	// finding 3).
	var variants []string
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		if name, _ := cmd.Name(src); name != "variant" || len(cmd.Words) < 3 {
			return
		}
		variant, ok := cmd.Words[1].Literal(src)
		body, braced := cmd.Words[len(cmd.Words)-1].BracedScript(src)
		if !ok || !braced || slices.Contains(variants, variant) {
			return
		}
		declares := false
		commands(src, body, true, func(inner syntax.Command, _ bool) {
			name, _ := inner.Name(src)
			option, _, _ := strings.Cut(name, "-")
			declares = declares || slices.Contains(archiveOptions, option)
		})
		if declares {
			variants = append(variants, variant)
		}
	})
	return variants
}

// DependencyVariants are the variants the Portfile defines whose own body
// declares a dependency on a port, by its name, as MacPorts' depends_*
// options take one (macports.ParseDependency), in the order the Portfile
// defines them: enchant2's +nuspell, under which alone it links nuspell.
// The port index records only what default variants depend on, so it
// can't say so. Like ArchiveVariants, it reads the text only, to find
// which variants MacPorts should be asked about; what each depends on is
// MacPorts' to say. A dependency written through a variable, or a variant
// a PortGroup defines, isn't found.
func DependencyVariants(src []byte, port string) []string {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil
	}
	var variants []string
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		if name, _ := cmd.Name(src); name != "variant" || len(cmd.Words) < 3 {
			return
		}
		variant, ok := cmd.Words[1].Literal(src)
		body, braced := cmd.Words[len(cmd.Words)-1].BracedScript(src)
		if !ok || !braced || slices.Contains(variants, variant) {
			return
		}
		depends := false
		commands(src, body, true, func(inner syntax.Command, _ bool) {
			name, _ := inner.Name(src)
			option, _, _ := strings.Cut(name, "-")
			phase, ok := strings.CutPrefix(option, "depends_")
			if !ok {
				return
			}
			for _, word := range inner.Words[1:] {
				spec, literal := word.Literal(src)
				if !literal {
					continue
				}
				if dependency, err := macports.ParseDependency(phase, spec); err == nil && strings.EqualFold(dependency.Port, port) {
					depends = true
				}
			}
		})
		if depends {
			variants = append(variants, variant)
		}
	})
	return variants
}
