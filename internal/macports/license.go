package macports

import (
	"slices"
	"strings"
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
}

// License is MacPorts' license line for an SPDX license expression, as
// a forge detects one or a manifest declares one: "MIT OR Apache-2.0" is
// {MIT Apache-2}, "MIT AND Zlib" is MIT zlib, and "(MIT OR Apache-2.0)
// AND Unicode-3.0" would be {MIT Apache-2} and its third, were that one
// named. Cargo's old "MIT/Apache-2.0" is a choice too. It reports false
// for an expression it can't say in MacPorts' words: a license it has no
// name for, GitHub's NOASSERTION, an exception (WITH), or a choice among
// licenses that apply together, which a braced sub-list can't say.
func License(expression string) (string, bool) {
	tokens := spdxTokens(expression)
	if len(tokens) == 0 {
		return "", false
	}
	// Each term of the top-level AND is one license, or a choice among
	// single licenses; nothing nests deeper.
	var terms []string
	for _, term := range splitTop(tokens, "AND") {
		choices := splitTop(unwrap(term), "OR")
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
			terms = append(terms, names[0])
		} else {
			terms = append(terms, "{"+strings.Join(names, " ")+"}")
		}
	}
	return strings.Join(terms, " "), true
}

// spdxTokens splits an expression into identifiers, operators, and
// parentheses. Cargo's deprecated "/" is OR.
func spdxTokens(expression string) []string {
	expression = strings.NewReplacer("(", " ( ", ")", " ) ", "/", " OR ").Replace(expression)
	fields := strings.Fields(expression)
	for i, field := range fields {
		switch strings.ToUpper(field) {
		case "AND", "OR", "WITH":
			fields[i] = strings.ToUpper(field)
		}
	}
	return fields
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
