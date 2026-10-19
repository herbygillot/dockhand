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
	// Terms are the first line, trimmed, of each paragraph no license
	// found covers that comes after a copyright line or a label: the terms
	// of its own a file grants beside its licenses, to quote, apart from
	// the prose about them a file of several opens with.
	Terms []string
}

// wholeText is how much of a license file's text the licenses found must
// cover for them to be what it is: what's left is its copyright lines and
// headings. A short notice naming a license, or a license with paragraphs
// of its own beside it, says more than the licenses found.
const wholeText = 75

// License classifies a license file's text: what licenses it is, of its
// text but for its copyright lines and the labels a file of several
// licenses heads each with ("SPDX full name:", "SPDX short identifier:",
// "License text:"), which are no license's words: libxkbcommon 1.13.2's
// LICENSE, MIT, HPND, and HPND-sell-variant as 1.7.0's was, with more
// copyright lines, fell from 79.5% covered to 64.0% and held its update
// (the person's coalesced roadmap, 2026-10-19).
func (f File) License() LicenseText {
	if f.Truncated {
		return LicenseText{}
	}
	coverage := licensecheck.Scan(f.Data)
	percent, terms := coveredWords(f.Data, coverage)
	text := LicenseText{Percent: percent, Terms: terms}
	for _, match := range coverage.Match {
		if !match.IsURL && !slices.Contains(text.IDs, match.ID) {
			text.IDs = append(text.IDs, match.ID)
		}
	}
	return text
}

// coveredWords is how much of a license file's words the licenses found
// cover, leaving out the copyright lines and license labels no license's
// match takes in: a template takes in a copyright line above its text, and
// the ones beside it, and the labels, are no license's words. The terms
// beside them are said too (LicenseText.Terms).
func coveredWords(data []byte, coverage licensecheck.Coverage) (float64, []string) {
	inMatch := func(start, end int) bool {
		for _, match := range coverage.Match {
			if start < match.End && end > match.Start {
				return true
			}
		}
		return false
	}
	matched := 0
	for _, match := range coverage.Match {
		matched += len(strings.Fields(string(data[match.Start:match.End])))
	}
	total, offset := 0, 0
	var terms []string
	// after is whether the last line that wasn't blank was a copyright
	// line or a label; opening is a paragraph's first line, while none of
	// its lines is a match's.
	after, opening, uncovered := false, "", false
	for _, line := range strings.SplitAfter(string(data), "\n") {
		start, end := offset, offset+len(line)
		offset = end
		covered := inMatch(start, end)
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			if uncovered && opening != "" {
				terms = append(terms, opening)
			}
			opening, uncovered = "", false
			continue
		case labelLine(line) && !covered:
			after, opening, uncovered = true, "", false
			continue
		case opening == "" && !uncovered && after:
			opening, uncovered = trimmed, !covered
		case covered:
			uncovered = false
		}
		after = false
		total += len(strings.Fields(line))
	}
	if uncovered && opening != "" {
		terms = append(terms, opening)
	}
	if total == 0 {
		return 0, terms
	}
	return min(100, 100*float64(matched)/float64(total)), terms
}

// labelLine reports a copyright line, or a label heading a license in a
// file of several.
func labelLine(line string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range []string{"copyright", "©", "(c)", "spdx full name:", "spdx short identifier:", "spdx-license-identifier:", "license text:"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
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

// String names the licenses: "MIT", "MIT and ISC", "MIT, HPND and
// HPND-sell-variant".
func (t LicenseText) String() string {
	if len(t.IDs) < 3 {
		return strings.Join(t.IDs, " and ")
	}
	return strings.Join(t.IDs[:len(t.IDs)-1], ", ") + " and " + t.IDs[len(t.IDs)-1]
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
