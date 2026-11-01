package version

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInput means a requested spelling cannot be a version or tag.
var ErrInput = errors.New("version: invalid spelling")

// Validate accepts any non-empty spelling without whitespace, control
// characters, or a leading dash, which would read as an option.
func Validate(value string) error {
	if value == "" || !utf8.ValidString(value) || strings.HasPrefix(value, "-") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ErrInput
	}
	return nil
}

// TagPattern maps between a Portfile version and the upstream tag that
// carries it, such as v1.2.3 for 1.2.3.
type TagPattern struct {
	Prefix string
	Suffix string
}

func (p TagPattern) Tag(version string) string { return p.Prefix + version + p.Suffix }

// Version strips the pattern from a tag; ok is false when the tag does not fit.
func (p TagPattern) Version(tag string) (string, bool) {
	version, prefix := strings.CutPrefix(tag, p.Prefix)
	version, suffix := strings.CutSuffix(version, p.Suffix)
	return version, prefix && suffix && version != ""
}

// Explicit reports whether a requested spelling already carries the pattern,
// so it names a tag rather than a version.
func (p TagPattern) Explicit(value string) bool {
	return p.Prefix != "" && strings.HasPrefix(value, p.Prefix) || p.Suffix != "" && strings.HasSuffix(value, p.Suffix)
}

// Stability is the classification of one version spelling. Automatic
// selection admits stable versions, and prereleases only for a port already
// on one; an explicit version is always honored, and callers use the
// classification to say when a change takes a port out of stable.
type Stability string

const (
	// Stable is a numeric version: dotted or dashed integers, including calendar versions.
	Stable Stability = "stable"
	// Prerelease carries a recognized pre-release marker such as alpha, beta, or rc.
	Prerelease Stability = "prerelease"
	// Unknown is any other spelling; it is neither admitted automatically nor called a prerelease.
	Unknown Stability = "unknown"
)

var (
	// A leading v, as perl's dotted-decimal module versions spell it, is
	// part of a stable spelling.
	stable = regexp.MustCompile(`^v?[0-9]+(?:[._-][0-9]+)*$`)
	// pep440 covers 1.0a1 and 1.0b2, where a single letter between digits is a
	// pre-release segment rather than a patch letter such as 1.0.2u.
	pep440  = regexp.MustCompile(`[0-9](?:a|b|c|rc)[0-9]`)
	markers = map[string]bool{
		"alpha": true, "beta": true, "rc": true, "pre": true, "prerelease": true, "preview": true,
		"dev": true, "snapshot": true, "nightly": true, "canary": true, "next": true, "unstable": true,
		"milestone": true, "cr": true, "test": true,
	}
)

// Classify reports the stability of a version spelling.
func Classify(version string) Stability {
	if stable.MatchString(version) {
		return Stable
	}
	lower := strings.ToLower(version)
	for _, run := range strings.FieldsFunc(lower, func(r rune) bool { return r < 'a' || r > 'z' }) {
		if markers[run] {
			return Prerelease
		}
	}
	if pep440.MatchString(lower) {
		return Prerelease
	}
	return Unknown
}

// LeavesStable reports whether moving from one version to another takes a
// port from a stable version to a prerelease.
func LeavesStable(from, to string) bool {
	return Classify(from) == Stable && Classify(to) == Prerelease
}

// prereleaseTail is what goes on after a captured version where it was
// read from a prerelease's name: a prerelease word and its number, as
// 1.4.2pre1, 2.0-rc1, or 3.1.dev2 spell it.
var prereleaseTail = regexp.MustCompile(`^[-._]?(?i:pre|rc|alpha|beta|dev)[-._]?[0-9]+`)

// CapturedPrerelease is the prerelease's name a captured version was read
// from, where the text has the capture only so: each time the capture
// stands in the text whole, not inside a longer number, it goes on, before
// an extension or the text's end, with a prerelease word and a number.
// ap-utils's livecheck captured 1.4.2 from ap-utils-1.4.2pre1.tar.bz2,
// its regex stopping before "pre1", and 1.4.2 read as a stable release
// newer than 1.4.1 (field testing, the outdated-ports session's note 19).
// ok is false where the capture stands alone anywhere, or nowhere.
func CapturedPrerelease(text, capture string) (name string, ok bool) {
	if capture == "" {
		return "", false
	}
	for from := 0; ; {
		at := strings.Index(text[from:], capture)
		if at < 0 {
			return name, name != ""
		}
		start, end := from+at, from+at+len(capture)
		from = start + 1
		// Inside a longer number, 11.4.2 or 1.4.23, it isn't this capture.
		if start > 0 && (isDigit(text[start-1]) || text[start-1] == '.') {
			continue
		}
		rest := text[end:]
		if rest != "" && (isDigit(rest[0]) || len(rest) > 1 && rest[0] == '.' && isDigit(rest[1])) {
			continue
		}
		tail := prereleaseTail.FindString(rest)
		if after := rest[len(tail):]; tail == "" || after != "" && isAlphanumeric(after[0]) {
			return "", false
		}
		if name == "" {
			name = capture + tail
		}
	}
}

func isDigit(b byte) bool { return '0' <= b && b <= '9' }

func isAlphanumeric(b byte) bool { return isDigit(b) || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' }

// leading is a version's first numeric component, without a leading v.
var leading = regexp.MustCompile(`^v?([0-9]+)`)

// CrossesMajor reports whether moving from one version to another changes
// its first numeric component, semgrep's 0.14.0 to 1.179.0: the change a
// project's own numbering says may break what builds on it. False where
// either has none, and where one is above 999, a year or a date, as
// 2025.1 or 20250914, whose first component moves every year.
func CrossesMajor(from, to string) bool {
	a, b := leading.FindStringSubmatch(from), leading.FindStringSubmatch(to)
	if a == nil || b == nil {
		return false
	}
	x, y := strings.TrimLeft(a[1], "0"), strings.TrimLeft(b[1], "0")
	return x != y && len(x) <= 3 && len(y) <= 3
}

// seriesSpelling is a release series as a port's name ends in one: 0.52,
// 1.28, 17.
var seriesSpelling = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)

// Series is the release series a versioned port keeps to: 0.52 for
// terragrunt-0.52 at 0.52.1, 1.28 for kubectl-1.28 at 1.28.15, where its
// name ends in a series its version begins with; "" for any other port.
func Series(name, version string) string {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return ""
	}
	series := name[i+1:]
	if !seriesSpelling.MatchString(series) || !strings.HasPrefix(version, series+".") {
		return ""
	}
	return series
}

// InSeries reports whether a version is of a release series: 0.52.2 is of
// 0.52, and 1.2.1 and 0.520.1 aren't.
func InSeries(series, version string) bool {
	return version == series || strings.HasPrefix(version, series+".")
}

// OlderScheme reports whether a candidate compares newer than the current
// version only where a component is written with a leading zero that the
// current version's has none of: aften's SourceForge listing kept 0.07,
// an older scheme than 0.0.8 that compares newer, and outdated said the
// downgrade was ready to ship (field testing's outdated-ports session).
// Where they first differ by number, the candidate's component leads
// with a zero and the current one's doesn't; 2024.01 over 2023.12 differs
// first at 2024, and isn't.
func OlderScheme(current, candidate string) bool {
	split := func(v string) []string {
		return strings.FieldsFunc(strings.TrimPrefix(v, "v"), func(r rune) bool { return r < '0' || r > '9' })
	}
	ours, theirs := split(current), split(candidate)
	for i := range min(len(ours), len(theirs)) {
		if ours[i] == theirs[i] {
			continue
		}
		return len(theirs[i]) > 1 && theirs[i][0] == '0' && (len(ours[i]) == 1 || ours[i][0] != '0')
	}
	return false
}
