package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func templateNote(runs map[string]verifyRun) verifyNote {
	return verifyNote{Schema: noteSchema, Sha: "0123456789abcdef0123",
		Port: "jq", Runs: runs}
}

// The body is the upstream PR template with only vouchable boxes
// checked: install passed and tested, single minted commit.
func TestPromoteBodyChecksWhatItCanVouchFor(t *testing.T) {
	n := templateNote(map[string]verifyRun{
		"Sonoma":   {State: "passed", Tested: true},
		"Monterey": {State: "unsupported", Detail: "declares known_fail on Monterey"},
	})
	body := promoteBody(n, true, "", 1, true)

	assert.Contains(t, body,
		"Verified with [dockhand]("+dockhandRepoURL+") at commit `0123456789ab` — "+
			"Monterey: the port declines this platform (known_fail); Sonoma: built and tested in a pristine VM.")
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
	n := templateNote(map[string]verifyRun{"Sonoma": {State: "passed"}})
	body := promoteBody(n, true, "", 1, false)
	assert.Contains(t, body, "Sonoma: built in a pristine VM")
	assert.Contains(t, body, "- [ ] checked that there aren't other open [pull requests]")
	assert.Contains(t, body, "- [ ] tried existing tests with `sudo port test`?")
	assert.Contains(t, body, "- [x] tried a full install with")
}

func TestPromoteBodyUnverifiedChecksNothing(t *testing.T) {
	body := promoteBody(verifyNote{}, false, "12345", 1, false)
	assert.Contains(t, body, "Not locally verified")
	assert.Contains(t, body, "Closes: https://trac.macports.org/ticket/12345")
	assert.NotContains(t, body, "###### Tested on")
	// A commit-message box is still checkable — dockhand wrote it — but
	// no build claim survives an unverified promotion.
	assert.NotContains(t, body, "- [x] tried")
}

func TestPromoteBodyManyCommitsAreTheUsersToVouchFor(t *testing.T) {
	n := templateNote(map[string]verifyRun{"Sonoma": {State: "passed"}})
	body := promoteBody(n, true, "", 3, false)
	assert.Contains(t, body, "- [ ] followed our [Commit Message Guidelines]")
	assert.Contains(t, body, "- [ ] squashed and [minimized your commits]")
}

// Every checklist line in the body must be one of the upstream
// template's own lines (modulo the strikethrough rewrite): a drifted
// checklist would read as dockhand inventing its own ceremony.
func TestPromoteBodyKeepsTheTemplateShape(t *testing.T) {
	n := templateNote(map[string]verifyRun{"Sonoma": {State: "passed", Tested: true}})
	body := promoteBody(n, true, "7", 1, true)
	require.True(t, strings.HasPrefix(body, "#### Description\n"))
	for _, section := range []string{"###### Type(s)", "###### Tested on", "###### Verification"} {
		assert.Contains(t, body, section)
	}
	assert.Equal(t, 12, strings.Count(body, "- ["), "3 type boxes + 9 checklist boxes")
}
