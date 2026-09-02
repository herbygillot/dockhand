package forge

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// update rewrites the goldens under testdata/golden from what the code
// renders now: `go test ./internal/forge -update`. A golden is a pinned
// PR body, so a rewrite is reviewed as a change to what dockhand tells
// upstream, not as test maintenance.
var update = flag.Bool("update", false, "rewrite golden files")

func templateNote(runs map[string]record.Run) record.Record {
	return record.Record{Sha: "0123456789abcdef0123",
		Port: "jq", Runs: runs}
}

// The body is the upstream PR template with only vouchable boxes
// checked: install passed and tested, a single minted commit.
func TestPromoteBodyChecksWhatItCanVouchFor(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Sonoma":   {State: "passed", Tested: true},
		"Monterey": {State: "unsupported", Detail: "declares known_fail on Monterey"},
	})
	body := PromoteBody(n, true, "", 1, true)

	assert.Contains(t, body,
		"Verified with [dockhand]("+RepoURL+") at commit `0123456789ab`\n"+
			"  — Monterey: the port declines this platform (known_fail).\n"+
			"  — Sonoma: built and tested in a pristine VM.\n")
	assert.Contains(t, body, "- macOS Sonoma — pristine tart VM, via dockhand")
	assert.Contains(t, body, "- [x] followed our [Commit Message Guidelines]")
	assert.Contains(t, body, "- [x] squashed and [minimized your commits]")
	assert.Contains(t, body, "- [x] tried existing tests with `sudo port test`?")
	assert.Contains(t, body, "- [x] checked that there aren't other open [pull requests]")
	assert.Contains(t, body,
		"- [x] tried a full install with ~~`sudo port -vst install`~~ `sudo port install` in a pristine VM")
	// What dockhand cannot vouch for stays with the human.
	assert.Contains(t, body, "- [ ] checked your Portfile with `port lint`?")
	assert.Contains(t, body, "- [ ] tested basic functionality of all binary files?")
}

func TestPromoteBodyWithoutTestsLeavesTheTestBoxOpen(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sonoma": {State: "passed"}})
	body := PromoteBody(n, true, "", 1, false)
	assert.Contains(t, body, "Sonoma: built in a pristine VM")
	assert.Contains(t, body, "- [ ] checked that there aren't other open [pull requests]")
	assert.Contains(t, body, "- [ ] tried existing tests with `sudo port test`?")
	assert.Contains(t, body, "- [x] tried a full install with")
}

func TestPromoteBodySignsOffEveryBody(t *testing.T) {
	signoff := "\nAutomated by [dockhand](" + RepoURL + ")\n"
	verified := templateNote(map[string]record.Run{"Sonoma": {State: "passed"}})
	for name, body := range map[string]string{
		"verified":   PromoteBody(verified, true, "", 1, true),
		"unverified": PromoteBody(record.Record{}, false, "", 1, false),
	} {
		assert.True(t, strings.HasSuffix(body, signoff), "%s body must end with the sign-off", name)
	}
}

func TestPromoteBodyUnverifiedChecksNothing(t *testing.T) {
	body := PromoteBody(record.Record{}, false, "12345", 1, false)
	assert.Contains(t, body, "Not locally verified")
	assert.Contains(t, body, "Closes: https://trac.macports.org/ticket/12345")
	assert.NotContains(t, body, "###### Tested on")
	// A commit-message box is still checkable — dockhand wrote it — but
	// no build claim survives an unverified promotion.
	assert.NotContains(t, body, "- [x] tried")
}

func TestPromoteBodyManyCommitsAreTheUsersToVouchFor(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sonoma": {State: "passed"}})
	body := PromoteBody(n, true, "", 3, false)
	assert.Contains(t, body, "- [ ] followed our [Commit Message Guidelines]")
	assert.Contains(t, body, "- [ ] squashed and [minimized your commits]")
}

// Every checklist line in the body must be one of the upstream
// template's own lines (modulo the strikethrough rewrite): a drifted
// checklist would read as dockhand inventing its own ceremony.
func TestPromoteBodyKeepsTheTemplateShape(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sonoma": {State: "passed", Tested: true}})
	body := PromoteBody(n, true, "7", 1, true)
	require.True(t, strings.HasPrefix(body, "#### Description\n"))
	for _, section := range []string{"###### Type(s)", "###### Tested on", "###### Verification"} {
		assert.Contains(t, body, section)
	}
	assert.Equal(t, 12, strings.Count(body, "- ["), "3 type boxes + 9 checklist boxes")
}

func TestPromoteBodyChecksLintWhenTheRunLinted(t *testing.T) {
	n := templateNote(map[string]record.Run{"Tahoe": {State: "passed", Linted: true, Lint: "clean"}})
	body := PromoteBody(n, true, "", 1, false)
	assert.Contains(t, body, "- [x] checked your Portfile with `port lint`?")
	// The checked box is only honest if the evidence line states what
	// backs it — the field-caught gap.
	assert.Contains(t, body, "Tahoe: linted clean, built in a pristine VM")
}

func TestPromoteBodyStatesLintWarnings(t *testing.T) {
	n := templateNote(map[string]record.Run{"Tahoe": {State: "passed", Linted: true, Lint: "2 warnings", Tested: true}})
	body := PromoteBody(n, true, "", 1, false)
	assert.Contains(t, body, "Tahoe: linted with 2 warnings, built and tested in a pristine VM")
	assert.Contains(t, body, "- [x] checked your Portfile with `port lint`?")
}

// goldenDir holds one pinned body per reachable PromoteBody variant.
const goldenDir = "testdata/golden"

// bodyVariant is one input shape promote can hand PromoteBody today.
// The golden is named for the variant, so a diff names what changed.
type bodyVariant struct {
	name       string
	runs       map[string]record.Run
	verified   bool
	closes     string
	ownCommits int
	checkedPRs bool
}

// bodyVariants enumerates the branches in PromoteBody: the verified
// evidence line per run state and per lint/test record, the sections
// that come and go with them, and the flags the checklist boxes read.
// Unverified bodies ignore the note entirely, which the failed and
// blocked cases pin: whatever the local record holds, the PR only ever
// says verified or not.
var bodyVariants = []bodyVariant{
	{name: "verified_one_platform",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_several_platforms",
		runs: map[string]record.Run{
			"Sequoia": {State: "passed"},
			"Sonoma":  {State: "passed"},
			"Ventura": {State: "passed"},
		},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_with_unsupported",
		runs: map[string]record.Run{
			"Sequoia":  {State: "passed"},
			"Monterey": {State: "unsupported", Detail: "declares known_fail on Monterey"},
		},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_tested",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Tested: true}},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_linted_without_summary",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true}},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_lint_clean",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true, Lint: "clean"}},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_lint_warnings",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true, Lint: "2 warnings"}},
		verified: true, ownCommits: 1, checkedPRs: true},
	// A lint summary with no Linted record is a note nothing wrote:
	// the claim needs both halves, so neither the line nor the box
	// carries it. Its golden is byte-identical to verified_one_platform,
	// which is the point.
	{name: "verified_lint_summary_without_linted",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Lint: "clean"}},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_tested_and_lint_clean",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Tested: true, Linted: true, Lint: "clean"}},
		verified: true, ownCommits: 1, checkedPRs: true},
	// The checklist boxes read the set: one tested, linted platform
	// checks them for the whole body while each evidence line keeps
	// its own record.
	{name: "verified_evidence_differs_by_platform",
		runs: map[string]record.Run{
			"Sequoia":  {State: "passed", Tested: true, Linted: true, Lint: "clean"},
			"Sonoma":   {State: "passed"},
			"Monterey": {State: "unsupported", Detail: "declares known_fail on Monterey"},
		},
		verified: true, ownCommits: 1, checkedPRs: true},
	// Every state that is not a verdict is local business: canceled
	// by this very promote, blocked on a neighbor, deferred for a
	// slot, superseded, errored. None of it reaches the reviewer, so
	// the golden is byte-identical to verified_one_platform.
	{name: "verified_omits_non_verdict_states",
		runs: map[string]record.Run{
			"Sequoia":  {State: "passed"},
			"Sonoma":   {State: "canceled", Detail: "canceled: promoted without waiting"},
			"Ventura":  {State: "blocked", Detail: "dependency oniguruma failed to build"},
			"Monterey": {State: "deferred", Detail: "2 of 2 workers busy"},
			"Tahoe":    {State: "superseded"},
			"Big Sur":  {State: "errored", Detail: "worker vanished"},
		},
		verified: true, ownCommits: 1, checkedPRs: true},
	{name: "verified_with_closes",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, closes: "71234", ownCommits: 1, checkedPRs: true},
	{name: "verified_many_commits",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, ownCommits: 3, checkedPRs: true},
	{name: "verified_prs_unchecked",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, ownCommits: 1, checkedPRs: false},
	{name: "unverified",
		ownCommits: 1, checkedPRs: true},
	// --no-verify past a failed build: the refusal is overridden, the
	// body says unverified, and the failure's detail stays local. The
	// golden is byte-identical to unverified: an unverified body
	// ignores the note entirely.
	{name: "unverified_failed_overridden",
		runs:       map[string]record.Run{"Sequoia": {State: "failed", Handle: "dockhand-jq-1", Detail: "Failed to build jq: boom"}},
		ownCommits: 1, checkedPRs: true},
	// Likewise byte-identical to unverified.
	{name: "unverified_blocked_and_canceled",
		runs: map[string]record.Run{
			"Sequoia": {State: "blocked", Detail: "dependency oniguruma failed to build"},
			"Sonoma":  {State: "canceled", Detail: "canceled: promoted without waiting"},
		},
		ownCommits: 1, checkedPRs: true},
	{name: "unverified_with_closes",
		closes: "71234", ownCommits: 1, checkedPRs: true},
	{name: "unverified_many_commits",
		ownCommits: 3, checkedPRs: true},
	{name: "unverified_prs_unchecked",
		ownCommits: 1, checkedPRs: false},
}

// Each variant's whole body is pinned: a checklist box flipping, a
// section appearing, or a phrase drifting is a change in what dockhand
// vouches for upstream, and the golden makes it a reviewed one.
func TestPromoteBodyGoldens(t *testing.T) {
	rendered := map[string]bool{}
	for _, v := range bodyVariants {
		require.False(t, rendered[v.name], "variant %q is listed twice", v.name)
		rendered[v.name] = true
		t.Run(v.name, func(t *testing.T) {
			body := PromoteBody(templateNote(v.runs), v.verified, v.closes, v.ownCommits, v.checkedPRs)
			checkGolden(t, v.name, body)
		})
	}
	// A golden no variant renders is a shape that stopped existing; it
	// leaves with the code that produced it rather than lingering as a
	// claim nobody checks.
	entries, err := os.ReadDir(goldenDir)
	require.NoError(t, err)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".golden")
		assert.True(t, rendered[name], "stale golden %s: no variant renders it", filepath.Join(goldenDir, e.Name()))
	}
}

// checkGolden compares got with testdata/golden/<name>.golden, or
// rewrites it under -update. A mismatch prints the line diff and the
// remedy, so the failure reads as the change it is.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden at %s; `go test ./internal/forge -update` writes it", path)
	if string(want) != got {
		t.Errorf("body differs from %s:\n%s\nif the new body is intended, `go test ./internal/forge -update` rewrites the golden", path, lineDiff(path, string(want), got))
	}
}

// lineDiff renders want against got as a unified diff with every line
// as context: the bodies are a few dozen lines, so trimming to hunks
// would hide more than it saves. Hand-rolled over a quadratic LCS
// because go-cmp is not vendored and the inputs are tiny.
func lineDiff(path, want, got string) string {
	a, b := diffLines(want), diffLines(got)
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	out.WriteString("--- want (" + path + ")\n+++ got\n")
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out.WriteString(" " + a[i] + "\n")
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out.WriteString("-" + a[i] + "\n")
			i++
		default:
			out.WriteString("+" + b[j] + "\n")
			j++
		}
	}
	for ; i < len(a); i++ {
		out.WriteString("-" + a[i] + "\n")
	}
	for ; j < len(b); j++ {
		out.WriteString("+" + b[j] + "\n")
	}
	return out.String()
}

// diffLines splits text for the diff, marking a missing final newline
// as its own line so the one difference a line split hides still shows.
func diffLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if !strings.HasSuffix(s, "\n") {
		lines = append(lines, `\ No newline at end of file`)
	}
	return lines
}
