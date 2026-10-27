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
	}, nil)
	require.Len(t, findings, 1)
	require.Equal(t, "✗ textproc/jq/Portfile:3: revision is 1 after a version update; MacPorts expects 0 [revision-after-update]", findings[0].String())
}

// A version MacPorts orders lower than the one it replaces raises the
// epoch, which port upgrade needs to take it; one vercmp orders forward,
// as a scheme change, needs none, and an epoch raised at the same time is
// fine. An obsoleting replaced_by changes the version, revision, or epoch,
// or no upgrade ever replaces an install (the docs dig's I3).
func TestALowerVersionOwesAnEpoch(t *testing.T) {
	// The order of the test: 1.6 is below 1.7, and 2026.1 above 1.7, as
	// vercmp has them; anything else can't be told.
	order := func(a, b string) (int, bool) {
		rank := map[string]int{"1.6": 1, "1.7": 2, "2026.1": 3}
		x, okA := rank[a]
		y, okB := rank[b]
		return x - y, okA && okB
	}
	findings := CheckPortfiles([]Portfile{
		{Path: "textproc/jq/Portfile", Before: "version 1.7\n", After: "version 1.6\n"},
		{Path: "textproc/yq/Portfile", Before: "version 1.7\n", After: "version 1.6\nepoch 1\n"},
		{Path: "textproc/oq/Portfile", Before: "version 1.7\nepoch 1\n", After: "version 1.6\nepoch 1\n"},
		{Path: "textproc/xq/Portfile", Before: "version 1.7\n", After: "version 2026.1\n"},
		{Path: "textproc/zq/Portfile", Before: "version 1.7\n", After: "version 0.9\n"},
	}, order)
	require.Equal(t, []string{"version-order", "version-order"}, codes(findings))
	require.Equal(t, "✗ textproc/jq/Portfile: version goes from 1.7 to 1.6, which MacPorts orders lower, without raising epoch; port upgrade never takes it [version-order]", findings[0].String())
	require.Equal(t, "textproc/oq/Portfile", findings[1].Where, "an epoch that stands raises nothing")
	require.Empty(t, CheckPortfiles([]Portfile{{Path: "textproc/jq/Portfile", Before: "version 1.7\n", After: "version 1.6\n"}}, nil), "no order, no check")

	findings = CheckPortfiles([]Portfile{
		{Path: "textproc/jq/Portfile", Before: "version 1.7\n", After: "version 1.7\nreplaced_by gojq\n"},
		{Path: "textproc/yq/Portfile", Before: "version 1.7\n", After: "version 1.7\nrevision 1\nreplaced_by gojq\n"},
		{Path: "textproc/oq/Portfile", Before: "version 1.7\nreplaced_by gojq\n", After: "version 1.7\nreplaced_by gojq\n"},
	}, nil)
	require.Equal(t, []string{"replaced-by-unchanged"}, codes(findings))
	require.Equal(t, "textproc/jq/Portfile", findings[0].Where)
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

// A line that is only a URL, or a trailer whose value is one, may run past
// 72 characters; a sentence holding a URL may not (the exiv2 session's
// notes, 2026-10-21).
func TestALongLineBesideAURLIsFlagged(t *testing.T) {
	long := "Backport the fix for https://bugs.gnunet.org/view.php?id=9223 from upstream now"
	findings := CheckCommits([]Commit{{ID: "a", Message: "libextractor: fix the build\n\n" + long + "\n"}})
	require.Equal(t, []string{"body-wrap"}, codes(findings))
	spared := "libextractor: fix the build\n\nhttps://github.com/macports/macports-ports/blob/master/graphics/exiv2/files/patch-x.diff\n" +
		"Closes: https://trac.macports.org/ticket/71234#comment:12-and-a-long-anchor-here\n" +
		"Generated-By: Dockhand v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480 (https://github.com/herbygillot/dockhand)\n"
	require.Empty(t, CheckCommits([]Commit{{ID: "b", Message: spared}}))
}
