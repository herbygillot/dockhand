package project

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A Python requirement's specifier and a version, as PEP 440 reads them,
// and a package's name as PEP 503 compares it.

// NormalizeName is a Python package's name as PEP 503 compares names:
// lower case, each run of "-", "_", and "." one "-". textual_fastdatatable
// and Textual.FastDataTable are textual-fastdatatable.
func NormalizeName(name string) string {
	return strings.ToLower(nameRuns.ReplaceAllString(name, "-"))
}

var nameRuns = regexp.MustCompile(`[-_.]+`)

// pep440Version is PEP 440's version pattern, its appendix's, with the
// leading "v" and the separators it allows.
var pep440Version = regexp.MustCompile(`(?i)^v?(?:(\d+)!)?(\d+(?:\.\d+)*)(?:[-_.]?(a|b|c|rc|alpha|beta|pre|preview)[-_.]?(\d*))?(?:-(\d+)|[-_.]?(post|rev|r)[-_.]?(\d*))?(?:[-_.]?(dev)[-_.]?(\d*))?(?:\+([a-z0-9]+(?:[-_.][a-z0-9]+)*))?$`)

// version is a PEP 440 version, as it orders: its epoch, its release, and
// its pre-, post-, and development release, each absent or numbered.
type version struct {
	epoch   int
	release []int
	pre     *[2]int // phase (a 0, b 1, rc 2) and number
	post    *int
	dev     *int
	local   string
}

func parseVersion(text string) (version, error) {
	m := pep440Version.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return version{}, fmt.Errorf("%q isn't a PEP 440 version", text)
	}
	number := func(s string) int {
		n, _ := strconv.Atoi(s)
		return n
	}
	v := version{epoch: number(m[1]), local: localLabel(m[10])}
	for _, part := range strings.Split(m[2], ".") {
		v.release = append(v.release, number(part))
	}
	if m[3] != "" {
		phase := map[string]int{"a": 0, "alpha": 0, "b": 1, "beta": 1, "c": 2, "rc": 2, "pre": 2, "preview": 2}[strings.ToLower(m[3])]
		v.pre = &[2]int{phase, number(m[4])}
	}
	switch {
	case m[5] != "":
		post := number(m[5])
		v.post = &post
	case m[6] != "":
		post := number(m[7])
		v.post = &post
	}
	if m[8] != "" {
		dev := number(m[9])
		v.dev = &dev
	}
	return v, nil
}

// localLabel is a local version label as PEP 440 compares one: its
// segments, separated by "-", "_", or ".", each in lower case, and a
// number's without its leading zeros, so "deadbeef.00" is "deadbeef.0".
func localLabel(label string) string {
	segments := strings.FieldsFunc(strings.ToLower(label), func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i, segment := range segments {
		if n, err := strconv.Atoi(segment); err == nil {
			segments[i] = strconv.Itoa(n)
		}
	}
	return strings.Join(segments, ".")
}

// compare orders two versions as PEP 440 does, local labels aside: a
// development release before its pre-releases, those before the release,
// and the release before its post-releases.
func (v version) compare(w version) int {
	if c := v.epoch - w.epoch; c != 0 {
		return c
	}
	for i := range max(len(v.release), len(w.release)) {
		if c := at(v.release, i) - at(w.release, i); c != 0 {
			return c
		}
	}
	if c := slices.Compare(v.preKey(), w.preKey()); c != 0 {
		return c
	}
	if c := optional(v.post, -1) - optional(w.post, -1); c != 0 {
		return c
	}
	return optional(v.dev, 1<<31-1) - optional(w.dev, 1<<31-1)
}

// preKey places a version among its release's pre-releases: a development
// release of the release itself before every one of them, and the release,
// or its post-release, after.
func (v version) preKey() []int {
	switch {
	case v.pre != nil:
		return []int{0, v.pre[0], v.pre[1]}
	case v.post == nil && v.dev != nil:
		return []int{-1}
	}
	return []int{1}
}

func at(release []int, i int) int {
	if i < len(release) {
		return release[i]
	}
	return 0
}

func optional(n *int, absent int) int {
	if n == nil {
		return absent
	}
	return *n
}

// clause is one comparison of a specifier: "==0.19.0", "~=1.4", "!=2.*".
var clause = regexp.MustCompile(`^(~=|===|==|!=|<=|>=|<|>)\s*(\S+)$`)

// Admits reports whether a requirement's specifier admits a version, as
// PEP 440 reads each of its comma-separated clauses. An empty specifier
// admits any version. What it can't read, a Poetry constraint, a direct
// reference, or a version that isn't PEP 440's, is an error, never a no.
// A pre-release is admitted wherever its version is: what's asked is
// whether an installed version meets the requirement, not which to pick.
func Admits(specifier, installed string) (bool, error) {
	specifier = strings.TrimSpace(specifier)
	if specifier == "" {
		return true, nil
	}
	have, err := parseVersion(installed)
	if err != nil {
		return false, err
	}
	for _, part := range strings.Split(specifier, ",") {
		m := clause.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil {
			return false, fmt.Errorf("%q isn't a PEP 440 specifier", part)
		}
		ok, err := admitsClause(m[1], m[2], have, installed)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func admitsClause(operator, text string, have version, installed string) (bool, error) {
	if operator == "===" {
		return strings.EqualFold(text, installed), nil
	}
	if prefix, ok := strings.CutSuffix(text, ".*"); ok {
		if operator != "==" && operator != "!=" {
			return false, fmt.Errorf("%s%s: only == and != take a wildcard", operator, text)
		}
		want, err := parseVersion(prefix)
		if err != nil {
			return false, err
		}
		// The candidate's release is padded with zeros to the prefix's
		// length: 2 is 2.0.0 to ==2.0.0.*.
		release := have.release
		for len(release) < len(want.release) {
			release = append(slices.Clone(release), 0)
		}
		matches := want.epoch == have.epoch && slices.Equal(release[:len(want.release)], want.release)
		return matches == (operator == "=="), nil
	}
	want, err := parseVersion(text)
	if err != nil {
		return false, err
	}
	if want.local == "" {
		have.local = ""
	}
	c := have.compare(want)
	switch operator {
	case "==":
		return c == 0 && have.local == want.local, nil
	case "!=":
		return c != 0 || have.local != want.local, nil
	case "<=":
		return c <= 0, nil
	case ">=":
		return c >= 0, nil
	case "<":
		// Not a pre-release of the version itself, a development release
		// included, unless it is one.
		return c < 0 && (want.prerelease() || !have.prerelease() || !sameRelease(have, want)), nil
	case ">":
		// Not a post-release of the version itself, unless it is one.
		return c > 0 && (want.post != nil || have.post == nil || !sameRelease(have, want)), nil
	case "~=":
		if len(want.release) < 2 {
			return false, fmt.Errorf("~=%s: a compatible release needs two release numbers", text)
		}
		prefix := want.release[:len(want.release)-1]
		return c >= 0 && want.epoch == have.epoch && len(have.release) >= len(prefix) && slices.Equal(have.release[:len(prefix)], prefix), nil
	}
	return false, fmt.Errorf("%s: an unknown comparison", operator)
}

// prerelease reports a pre-release or a development release.
func (v version) prerelease() bool { return v.pre != nil || v.dev != nil }

func sameRelease(a, b version) bool {
	for i := range max(len(a.release), len(b.release)) {
		if at(a.release, i) != at(b.release, i) {
			return false
		}
	}
	return a.epoch == b.epoch
}
