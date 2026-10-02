package project

import (
	"slices"
	"strings"

	"github.com/github/go-spdx/v2/spdxexp"
	"github.com/google/licensecheck"
)

// LicenseText is what licenses a license file's text is, by their SPDX
// identifiers, as licensecheck (pkg.go.dev's own classifier) finds them,
// each once in the order the text has them, and how much of the text they
// cover: MIT's text below a copyright line is MIT, most of the file. Text
// that no license's matches, as MIT with a clause dropped, is no license
// it knows, which isn't to say it's none.
type LicenseText struct {
	IDs []string
	// Percent is how much of the text, by its words, the licenses found
	// cover.
	Percent float64
}

// wholeText is how much of a license file's text the licenses found must
// cover for them to be what it is: what's left is its copyright lines and
// headings. A short notice naming a license, or a license with paragraphs
// of its own beside it, says more than the licenses found.
const wholeText = 75

// License classifies a license file's text: what licenses it is.
func (f File) License() LicenseText {
	if f.Truncated {
		return LicenseText{}
	}
	coverage := licensecheck.Scan(f.Data)
	text := LicenseText{Percent: coverage.Percent}
	for _, match := range coverage.Match {
		if !match.IsURL && !slices.Contains(text.IDs, match.ID) {
			text.IDs = append(text.IDs, match.ID)
		}
	}
	return text
}

// Known reports text the licenses found are, nearly all of it.
func (t LicenseText) Known() bool {
	return len(t.IDs) > 0 && t.Percent >= wholeText
}

// Same reports two classifications that name the same licenses, whatever
// their order.
func (t LicenseText) Same(other LicenseText) bool {
	return slices.Equal(slices.Sorted(slices.Values(t.IDs)), slices.Sorted(slices.Values(other.IDs)))
}

// String names the licenses: "MIT", "MIT and ISC".
func (t LicenseText) String() string {
	return strings.Join(t.IDs, " and ")
}

// LicenseExpression is an SPDX license expression as its specification
// reads it, through go-spdx: its identifiers and operators in their
// canonical case, "mit or apache-2.0" being "MIT OR Apache-2.0", or false
// where it isn't one, as "Apache 2" and GitHub's NOASSERTION aren't.
// Cargo's deprecated "MIT/Apache-2.0", which the Cargo Book reads as a
// choice, is read as one, and an operator in lower case, which go-spdx
// refuses and manifests write, as its upper.
func LicenseExpression(expression string) (string, bool) {
	words := strings.Fields(strings.NewReplacer("/", " OR ", "(", " ( ", ")", " ) ").Replace(expression))
	if len(words) == 0 {
		return "", false
	}
	for i, word := range words {
		switch upper := strings.ToUpper(word); upper {
		case "AND", "OR", "WITH":
			words[i] = upper
		}
	}
	expression = strings.Join(words, " ")
	normalized, invalid := spdxexp.ValidateAndNormalizeLicensesWithOptions([]string{expression}, spdxexp.ValidateLicensesOptions{})
	if len(invalid) > 0 || len(normalized) != 1 {
		return "", false
	}
	return normalized[0], true
}
