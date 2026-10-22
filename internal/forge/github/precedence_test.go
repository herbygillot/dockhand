package github

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
)

// The REST inspection and the GraphQL reading count a check alike, each in
// its own spelling (REST's lowercase, GraphQL's uppercase), and stand on
// reviews alike (R3-24): the table each reading feeds.
func TestBothReadingsCountAlike(t *testing.T) {
	for _, c := range []struct {
		name                    string
		run                     bool
		state, conclusion       string
		passed, pending, failed int
	}{
		{"run passed", true, "completed", "success", 1, 0, 0},
		{"run neutral", true, "completed", "neutral", 1, 0, 0},
		{"run skipped", true, "completed", "skipped", 1, 0, 0},
		{"run in progress", true, "in_progress", "", 0, 1, 0},
		{"run awaiting approval", true, "completed", "action_required", 0, 1, 0},
		{"run failed", true, "completed", "failure", 0, 0, 1},
		{"run timed out", true, "completed", "timed_out", 0, 0, 1},
		{"status success", false, "success", "", 1, 0, 0},
		{"status pending", false, "pending", "", 0, 1, 0},
		{"status expected", false, "expected", "", 0, 1, 0},
		{"status error", false, "error", "", 0, 0, 1},
	} {
		for _, spell := range []func(string) string{func(s string) string { return s }, strings.ToUpper} {
			var status forge.PullRequestStatus
			check(&status, c.run, "macos-15", spell(c.state), spell(c.conclusion))
			require.Equal(t, [3]int{c.passed, c.pending, c.failed}, [3]int{status.Checks.Passed, status.Checks.Pending, status.Checks.Failed}, c.name)
		}
	}

	early, late := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC), time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	var status forge.PullRequestStatus
	review(&status, []opinion{{Login: "ryan", State: "APPROVED", At: late}, {Login: "ada", State: "changes_requested", At: early}, {Login: "lin", State: "DISMISSED", At: late}})
	require.Equal(t, "changes-requested", status.Review, "a request for changes holds over an approval")
	require.Equal(t, []string{"ada"}, status.ChangesRequestedBy)
	require.Equal(t, early, status.ReviewedAt, "when the review that sets it was made")
}
