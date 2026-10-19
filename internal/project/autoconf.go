package project

import (
	"regexp"
	"slices"
	"strings"
)

// AutoconfFacts are what a configure.ac asks of the build, as its macros
// name them: the options it adds, --enable-* and --with-*, the pkg-config
// modules it checks for, and the libraries it links. A person reading
// "configure.ac changed" was left to diff it (field testing, batch 11:
// dateutils). Macros it reads literally; one whose name a macro computes
// isn't listed. A library named by a shell variable, AC_CHECK_LIB([$zstd],
// ...), is the literal word the file sets it to, zstd=zstd, and where
// none is set, as `$pnglibbase` from sed's output, LibrariesIncomplete
// says the list leaves one out: GraphicsMagick 1.3.49 read as twenty
// libraries removed (the field notes, 2026-10-19).
type AutoconfFacts struct {
	Enables, Withs, Modules, Libraries []string
	LibrariesIncomplete                bool
}

var (
	autoconfEnable  = regexp.MustCompile(`AC_ARG_ENABLE\(\s*\[?([A-Za-z0-9_-]+)`)
	autoconfWith    = regexp.MustCompile(`AC_ARG_WITH\(\s*\[?([A-Za-z0-9_-]+)`)
	autoconfModules = regexp.MustCompile(`PKG_CHECK_MODULES(?:_STATIC)?\(\s*\[?[A-Za-z0-9_]+\]?\s*,\s*\[?([^\],)]+)`)
	autoconfLib     = regexp.MustCompile(`AC_CHECK_LIB\(\s*\[?(\$\{?[A-Za-z_][A-Za-z0-9_]*\}?|[A-Za-z0-9_+.-]+)`)
	// A shell variable set to one literal word, as zstd=zstd, and a
	// reference to one, $zstd or ${zstd}.
	autoconfAssign   = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)=([A-Za-z0-9_+.-]+)[ \t]*$`)
	autoconfVariable = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)
	autoconfSearch   = regexp.MustCompile(`AC_SEARCH_LIBS\(\s*\[?[A-Za-z0-9_]+\]?\s*,\s*\[?([^\],)]+)`)
	// A module's version constraint, ">= 2.0", isn't a module.
	autoconfConstraint = regexp.MustCompile(`^[<>=!]+$|^[0-9][0-9.]*$`)
)

// ReadAutoconf reads a configure.ac's facts, each list sorted and once.
func ReadAutoconf(data []byte) AutoconfFacts {
	text := string(data)
	names := func(pattern *regexp.Regexp, split bool) []string {
		var found []string
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			words := []string{match[1]}
			if split {
				words = strings.Fields(match[1])
			}
			for _, word := range words {
				word = strings.Trim(word, "[] \t")
				if word != "" && !autoconfConstraint.MatchString(word) && !slices.Contains(found, word) {
					found = append(found, word)
				}
			}
		}
		slices.Sort(found)
		return found
	}
	facts := AutoconfFacts{Enables: names(autoconfEnable, false), Withs: names(autoconfWith, false), Modules: names(autoconfModules, true)}
	set := map[string][]string{}
	for _, match := range autoconfAssign.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(set[match[1]], match[2]) {
			set[match[1]] = append(set[match[1]], match[2])
		}
	}
	for _, name := range append(names(autoconfLib, false), names(autoconfSearch, true)...) {
		variable := autoconfVariable.FindStringSubmatch(name)
		switch {
		case variable == nil:
			facts.Libraries = append(facts.Libraries, name)
		case len(set[variable[1]]) == 0:
			facts.LibrariesIncomplete = true
		default:
			facts.Libraries = append(facts.Libraries, set[variable[1]]...)
		}
	}
	slices.Sort(facts.Libraries)
	facts.Libraries = slices.Compact(facts.Libraries)
	return facts
}
