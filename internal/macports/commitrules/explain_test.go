package commitrules

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
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
		{ID: "e", Message: "jq: update to 1.8.1\n\nThis fixes https://trac.macports.org/ticket/71234.\n", Ports: []string{"jq"}},
	})
	lower := func(a, b string) (int, bool) { return strings.Compare(a, b), true }
	findings = append(findings, CheckPortfiles([]Portfile{
		{Path: "textproc/jq/Portfile", Before: "version 1\nrevision 2\n", After: "version 2\nrevision 2\n"},
		{Path: "textproc/yq/Portfile", Before: "version 2\n", After: "version 1\n"},
		{Path: "textproc/oq/Portfile", Before: "version 1\n", After: "version 1\nreplaced_by jq\n"},
		{Path: "devel/zq/Portfile", Before: "version 1\ndepends_lib port:a\n", After: "version 1\ndepends_lib port:b\n"},
		{Path: "devel/xq/Portfile", Before: "version 1\ndepends_build port:a\n", After: "version 1\nrevision 1\ndepends_build port:b\n"},
	}, lower)...)
	findings = append(findings, CheckDependencies([]Portfile{{Path: "devel/wq/Portfile", After: "depends_build port:pkgconfig\n"}},
		func(port string) ([]string, string) { return []string{"pkgconf"}, "path:bin/pkg-config:pkgconfig" })...)
	findings = append(findings, CheckRestated([]Portfile{{Path: "devel/vq/Portfile", After: "use_configure no\n"}},
		func(string, []byte) (macports.PortInfo, bool) {
			return macports.PortInfo{Options: map[string]string{"use_configure": "no"}}, true
		})...)
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
