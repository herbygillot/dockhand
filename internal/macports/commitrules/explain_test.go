package commitrules

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEveryRuleIsExplained keeps the rules and their explanations in step:
// each code the rules can report is explained, and each explanation is of
// a rule that exists.
func TestEveryRuleIsExplained(t *testing.T) {
	findings := CheckCommits([]Commit{
		{ID: "a", Message: "update\n\n" + strings.Repeat("x", 80) + "\nsee #71234\n", Ports: []string{"jq"}},
		{ID: "b", Message: "jq: fix checksums\n", Ports: []string{"jq"}},
		{ID: "c", Message: "jq: " + strings.Repeat("y", 70) + "\n", Ports: []string{"jq"}, Merge: true},
		{ID: "d", Message: "jq: fix\n", Ports: []string{"jq"}},
	})
	findings = append(findings, CheckPortfiles([]Portfile{{Path: "textproc/jq/Portfile", Before: "version 1\nrevision 2\n", After: "version 2\nrevision 2\n"}})...)
	var reported []string
	for _, finding := range findings {
		if !slices.Contains(reported, finding.Code) {
			reported = append(reported, finding.Code)
		}
		explanation, ok := Explain(finding.Code)
		require.True(t, ok, "%s is not explained", finding.Code)
		require.NotEmpty(t, explanation.Rule)
		require.NotEmpty(t, explanation.Sources)
	}
	slices.Sort(reported)
	codes := Codes()
	slices.Sort(codes)
	require.Equal(t, codes, reported, "every explanation is of a rule the checks report")
	_, ok := Explain("no-such-rule")
	require.False(t, ok)
}
