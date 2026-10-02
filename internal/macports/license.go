package macports

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// A Portfile's license line, as the Guide writes one (portfile-keywords,
// license): each license by its name, a hyphen, and its version with any
// ".0" dropped, "+" for "or any later version"; licenses separated by
// spaces all apply, and a braced sub-list is a choice of one of them. The
// names are the ones the ports tree uses: Base's lint checks their form,
// not a list of them, and the wiki's list is the tree's values as of 2020.

// spdxLicenses are MacPorts' names for SPDX identifiers. A work dedicated
// to the public domain, by CC0 or the Unlicense, is public-domain, as 446
// ports say; CC0-1 is refused by lint, since a name can't end in a digit.
var spdxLicenses = map[string]string{
	"MIT": "MIT", "Apache-2.0": "Apache-2", "BSD-2-Clause": "BSD", "BSD-3-Clause": "BSD", "0BSD": "BSD", "ISC": "ISC",
	"GPL-2.0": "GPL-2", "GPL-2.0-only": "GPL-2", "GPL-2.0-or-later": "GPL-2+", "GPL-3.0": "GPL-3", "GPL-3.0-only": "GPL-3", "GPL-3.0-or-later": "GPL-3+",
	"LGPL-2.1": "LGPL-2.1", "LGPL-2.1-only": "LGPL-2.1", "LGPL-2.1-or-later": "LGPL-2.1+", "LGPL-3.0": "LGPL-3", "LGPL-3.0-only": "LGPL-3", "LGPL-3.0-or-later": "LGPL-3+",
	"AGPL-3.0": "AGPL-3", "AGPL-3.0-only": "AGPL-3", "AGPL-3.0-or-later": "AGPL-3+", "MPL-2.0": "MPL-2", "Zlib": "zlib",
	"BSL-1.0": "Boost-1", "EPL-2.0": "EPL-2", "Artistic-2.0": "Artistic-2", "WTFPL": "WTFPL-2",
	"CC0-1.0": "public-domain", "Unlicense": "public-domain",
	"EUPL-1.1": "EUPL-1.1", "EUPL-1.2": "EUPL-1.2",
}

// License is MacPorts' license line for an SPDX license expression, as
// a forge detects one or a manifest declares one: "MIT OR Apache-2.0" is
// {MIT Apache-2}, "MIT AND Zlib" is MIT zlib, and "(MIT OR Apache-2.0)
// AND Unicode-3.0" would be {MIT Apache-2} and its third, were that one
// named. Cargo's old "MIT/Apache-2.0" is a choice too. The expression is
// read as SPDX's specification has it first (project.LicenseExpression),
// so one that isn't an expression at all, as "Apache 2", is refused rather
// than read word by word. It reports false for an expression it can't say
// in MacPorts' words: one that isn't valid, a license it has no name for,
// GitHub's NOASSERTION, an exception (WITH), or a choice among licenses
// that apply together, which a braced sub-list can't say.
func License(expression string) (string, bool) {
	normalized, ok := project.LicenseExpression(expression)
	if !ok {
		return "", false
	}
	tokens := unwrap(spdxTokens(normalized))
	// OR binds loosest, as SPDX's precedence has it, and go-spdx drops the
	// parentheses that say no more than it: "MIT AND Zlib OR ISC" is a
	// choice, of which one applies two licenses together, which a braced
	// sub-list can't say.
	if top := splitTop(tokens, "OR"); len(top) > 1 {
		choice, ok := choiceOf(top)
		return choice, ok
	}
	// Each term of the AND is one license, or a choice among single
	// licenses; nothing nests deeper.
	var terms []string
	for _, term := range splitTop(tokens, "AND") {
		choice, ok := choiceOf(splitTop(unwrap(term), "OR"))
		if !ok {
			return "", false
		}
		terms = append(terms, choice)
	}
	return strings.Join(terms, " "), true
}

// choiceOf is a choice among single licenses in MacPorts' words, braced
// where there's more than one name; false where a choice isn't a single
// license, or one MacPorts has no name for.
func choiceOf(choices [][]string) (string, bool) {
	var names []string
	for _, choice := range choices {
		choice = unwrap(choice)
		if len(choice) != 1 {
			return "", false
		}
		name, ok := spdxLicenses[choice[0]]
		if !ok {
			return "", false
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 1 {
		return names[0], true
	}
	return "{" + strings.Join(names, " ") + "}", true
}

// spdxTokens splits a valid expression, as go-spdx normalizes one, into
// identifiers, operators, and parentheses: go-spdx validates an
// expression's structure, and doesn't export it, so the terms MacPorts'
// line has are found here.
func spdxTokens(expression string) []string {
	return strings.Fields(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(expression))
}

// splitTop splits tokens at an operator outside parentheses.
func splitTop(tokens []string, operator string) [][]string {
	var parts [][]string
	depth, start := 0, 0
	for i, token := range tokens {
		switch token {
		case "(":
			depth++
		case ")":
			depth--
		case operator:
			if depth == 0 {
				parts = append(parts, tokens[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, tokens[start:])
}

// unwrap takes off the parentheses around tokens, as many pairs as
// enclose them all: "((MIT))" is MIT, while "(MIT) OR (ISC)" stays.
func unwrap(tokens []string) []string {
	for len(tokens) >= 2 && tokens[0] == "(" && tokens[len(tokens)-1] == ")" && balanced(tokens[1:len(tokens)-1]) {
		tokens = tokens[1 : len(tokens)-1]
	}
	return tokens
}

// balanced reports tokens whose parentheses pair up without closing below
// the start.
func balanced(tokens []string) bool {
	depth := 0
	for _, token := range tokens {
		switch token {
		case "(":
			depth++
		case ")":
			if depth--; depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// LicenseNames reports whether a Portfile's license line names each
// license another line does, as one that applies or among a choice: zola's
// "EUPL-1.2 MIT" names Cargo.toml's EUPL-1.2, and "MIT" doesn't. A line
// that isn't a Tcl list names nothing.
func LicenseNames(line, other string) bool {
	has, ok := licenseNames(line)
	wanted, wantedOK := licenseNames(other)
	if !ok || !wantedOK || len(wanted) == 0 {
		return false
	}
	for _, name := range wanted {
		if !slices.Contains(has, name) {
			return false
		}
	}
	return true
}

// licenseNames are the licenses a license line names, each choice's too.
func licenseNames(line string) ([]string, bool) {
	terms, err := readList("license", line)
	if err != nil {
		return nil, false
	}
	var names []string
	for _, term := range terms {
		choices, err := readList("license", term)
		if err != nil {
			return nil, false
		}
		names = append(names, choices...)
	}
	return names, true
}

// LicenseWords says a Portfile's license line for a person: a braced
// choice as "MIT or Apache-2", licenses that all apply as "and", "(MIT or
// Apache-2) and Boost-1". A line that isn't a Tcl list is said as written.
func LicenseWords(license string) string {
	terms, err := readList("license", license)
	if err != nil || len(terms) == 0 {
		return strings.TrimSpace(license)
	}
	words := make([]string, len(terms))
	for i, term := range terms {
		choices, err := readList("license", term)
		if err != nil || len(choices) < 2 {
			words[i] = term
			continue
		}
		words[i] = strings.Join(choices, " or ")
		if len(terms) > 1 {
			words[i] = "(" + words[i] + ")"
		}
	}
	return strings.Join(words, " and ")
}
