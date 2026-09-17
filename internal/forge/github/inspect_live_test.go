package github_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// TestInspectLivePullRequest reads one real pull request when
// DOCKHAND_TEST_GITHUB_PR names it as owner/repo#number. It only reads.
func TestInspectLivePullRequest(t *testing.T) {
	selector := os.Getenv("DOCKHAND_TEST_GITHUB_PR")
	if selector == "" {
		t.Skip("set DOCKHAND_TEST_GITHUB_PR=owner/repo#number to inspect a live pull request")
	}
	repository, number, ok := strings.Cut(selector, "#")
	require.True(t, ok)
	n, err := strconv.Atoi(number)
	require.NoError(t, err)
	client := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{Token: os.Getenv("DOCKHAND_TEST_GITHUB_TOKEN")}}}
	status, err := client.Inspect(t.Context(), record.PullRequestRef{Forge: "github", Repository: repository, Number: n})
	require.NoError(t, err)
	require.Contains(t, []string{"yes", "no", "unknown"}, status.Mergeable)
	require.Contains(t, []string{"approved", "changes-requested", "none"}, status.Review)
	t.Logf("%s: %s", selector, status.Summary())
}
