package commitrules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func codes(findings []Finding) []string {
	var all []string
	for _, f := range findings {
		all = append(all, f.Code)
	}
	return all
}

func TestAGoodSeriesPasses(t *testing.T) {
	findings := CheckCommits([]Commit{
		{ID: "a", Message: "libharbor: update to 2.0\n\nCloses: https://trac.macports.org/ticket/71234\n", Ports: []string{"libharbor"}},
		{ID: "b", Message: "harbor-viewer, harbor-cli: rebuild for libharbor 2.0\n", Ports: []string{"harbor-viewer", "harbor-cli"}},
	})
	require.Empty(t, findings)
}

func TestTheMessageRules(t *testing.T) {
	findings := CheckCommits([]Commit{
		{ID: "a1", Message: "Update jq\n", Ports: []string{"jq"}},
		{ID: "a2", Message: "jq: fix\n", Ports: []string{"jq"}},
		{ID: "a3", Message: "jq: fix checksums\n", Ports: []string{"jq"}},
		{ID: "a4", Message: "jq: " + strings.Repeat("x", 60) + "\n\nsee #71234 " + strings.Repeat("y", 80) + "\n", Ports: []string{"jq"}},
		{ID: "a5", Message: "Merge branch 'master'\n", Merge: true},
	})
	require.Equal(t, []string{"subject-port", "subject-vague", "follow-up", "subject-length", "body-wrap", "ticket-url", "merge"}, codes(findings))
	require.True(t, Errors(findings))
	require.Equal(t, `✗ commit a1: subject "Update jq" should start with the port it changes: "jq: …" [subject-port]`, findings[0].String())
}

func TestAFollowUpNeedsAnEarlierCommitToTheSamePort(t *testing.T) {
	require.Empty(t, CheckCommits([]Commit{{ID: "a", Message: "jq: fix typo in description\n", Ports: []string{"jq"}}}))
	require.Empty(t, CheckCommits([]Commit{
		{ID: "a", Message: "fd: update to 10.3.0\n", Ports: []string{"fd"}},
		{ID: "b", Message: "jq: fix typo in description\n", Ports: []string{"jq"}},
	}))
}

func TestRevisionAfterAnUpdate(t *testing.T) {
	findings := CheckPortfiles([]Portfile{
		{Path: "textproc/jq/Portfile", Before: "name jq\nversion 1.7.1\nrevision 2\n", After: "name jq\nversion 1.8.1\nrevision 1\n"},
		{Path: "sysutils/fd/Portfile", Before: "github.setup sharkdp fd 10.2.0 v\nrevision 1\n", After: "github.setup sharkdp fd 10.3.0 v\nrevision 0\n"},
		{Path: "net/croc/Portfile", Before: "version 10.2.4\nrevision 0\n", After: "version 10.2.4\nrevision 1\n"},
	})
	require.Len(t, findings, 1)
	require.Equal(t, "✗ textproc/jq/Portfile:3: revision is 1 after a version update; MacPorts expects 0 [revision-after-update]", findings[0].String())
}

// A subject may name the subport it changes, which tidy writes: the rules
// held "terraform-1.16: update to 1.16.5" as not naming terraform (field
// testing, 2026-10-02). A port the directory doesn't define still holds.
func TestASubjectMayNameTheSubportItChanges(t *testing.T) {
	commit := func(message string) []Commit {
		return []Commit{{ID: "a1", Message: message, Ports: []string{"terraform"}, Defined: []string{"terraform", "terraform-1.15", "terraform-1.16", "terraform_select"}}}
	}
	require.Empty(t, CheckCommits(commit("terraform-1.16: update to 1.16.5\n")))
	require.Empty(t, CheckCommits(commit("terraform: update terraform-1.16 to 1.16.5\n")))
	require.Equal(t, []string{"subject-port"}, codes(CheckCommits(commit("terraform-2.0: update to 2.0.1\n"))))
}
