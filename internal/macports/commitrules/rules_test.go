package commitrules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
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

// The revbump classifier's non-ABI half, read from the diff alone (Then's
// item 9): a runtime dependency changed with neither the version nor the
// revision raised, and a revision raised for build-time dependencies
// alone, are each a warning; a runtime change with the revision raised,
// and one with a new version, are neither.
func TestARevisionBumpIsReadFromTheDiff(t *testing.T) {
	t.Parallel()
	codes := func(before, after string) []string {
		var found []string
		for _, finding := range CheckPortfiles([]Portfile{{Path: "devel/zq/Portfile", Before: before, After: after}}, nil) {
			found = append(found, finding.Code)
		}
		return found
	}
	require.Equal(t, []string{"dependencies-unbumped"}, codes("version 1\ndepends_lib-append \\\n    port:a\n", "version 1\ndepends_lib-append \\\n    port:a port:b\n"))
	require.Empty(t, codes("version 1\ndepends_lib port:a\n", "version 1\nrevision 1\ndepends_lib port:b\n"), "the revision raised")
	require.Empty(t, codes("version 1\ndepends_run port:a\n", "version 2\ndepends_run port:b\n"), "a new version")
	require.Equal(t, []string{"revbump-build-only"}, codes("version 1\nrevision 1\ndepends_build port:a\n", "version 1\nrevision 2\ndepends_build port:b\n"))
	require.Empty(t, codes("version 1\nrevision 1\n", "version 1\nrevision 2\n"), "a bare revbump, for a dependency's rebuild")
	require.Empty(t, codes("version 1\ndepends_build port:a\n", "version 1\ndepends_build port:b\n"), "build-time alone, unbumped")
}

// A Trac ticket cited by its URL in the subject or a sentence is offered
// as a trailer; one a trailer cites already, as Closes: or See:, is said
// nothing of (the review survey's §3).
func TestATicketOutsideATrailerIsOfferedAsOne(t *testing.T) {
	ticket := func(message string) []Finding {
		var found []Finding
		for _, finding := range CheckCommits([]Commit{{ID: "a", Message: message, Ports: []string{"jq"}}}) {
			if finding.Code == "ticket-trailer" {
				found = append(found, finding)
			}
		}
		return found
	}
	found := ticket("jq: update to 1.8.1\n\nThis fixes https://trac.macports.org/ticket/71234.\n")
	require.Len(t, found, 1)
	require.Equal(t, Warning, found[0].Severity)
	require.Contains(t, found[0].Message, "Closes: https://trac.macports.org/ticket/71234")
	require.Len(t, ticket("jq: update to 1.8.1, https://trac.macports.org/ticket/71234\n"), 1, "in the subject")
	require.Empty(t, ticket("jq: update to 1.8.1\n\nCloses: https://trac.macports.org/ticket/71234\n"))
	require.Empty(t, ticket("jq: update to 1.8.1\n\nSee: https://trac.macports.org/ticket/71234\n"))
	require.Empty(t, ticket("jq: update to 1.8.1\n"))
}

// A port: dependency the change adds that another port can satisfy is
// offered path-style; one the Portfile had, or one nothing else provides,
// is said nothing of (the review survey's §5).
func TestADependencyAnotherPortProvidesIsOfferedPathStyle(t *testing.T) {
	providers := func(port string) []string {
		return map[string][]string{"pkgconfig": {"pkgconf"}, "libfoo": {"libfoo-devel"}}[port]
	}
	findings := CheckDependencies([]Portfile{
		{Path: "devel/harbor/Portfile", Before: "name harbor\ndepends_lib port:libfoo\n", After: "name harbor\ndepends_lib port:libfoo\ndepends_build-append port:pkgconfig port:cmake\n"},
		{Path: "devel/wharf/Portfile", After: "name wharf\ndepends_lib port:libfoo\n"},
	}, providers)
	require.Equal(t, []Finding{
		{Code: "path-style", Severity: Warning, Where: "devel/harbor/Portfile:3", Message: "depends_build-append takes port:pkgconfig, which pkgconf can satisfy too; a path-style dependency, path:bin/pkg-config:pkgconfig, lets either"},
		{Code: "path-style", Severity: Warning, Where: "devel/wharf/Portfile:2", Message: "depends_lib takes port:libfoo, which libfoo-devel can satisfy too; a path-style dependency, path:<a file both install>:libfoo, lets either"},
	}, findings, "a new port's every dependency is one it adds")
	require.Empty(t, CheckDependencies([]Portfile{{Path: "devel/wharf/Portfile", After: "depends_lib port:libfoo\n"}}, nil))
}

// A line a change adds that sets what the port's PortGroups or MacPorts
// set already is said, as its Portfile without it evaluates the same; one
// that changes the evaluation, or one the change kept, isn't (the review
// survey's §1).
func TestALineAPortGroupAlreadySetsIsSaid(t *testing.T) {
	// The evaluation stands in for MacPorts': github.setup sets version,
	// and the python PortGroup adds py-setuptools; the line setting
	// homepage is the port's own.
	evaluate := func(_ string, text []byte) (macports.PortInfo, bool) {
		info := macports.PortInfo{Name: "py-harbor", Version: "1.8.1", Options: map[string]string{"homepage": "https://pypi.org/project/harbor", "use_configure": "yes"},
			Dependencies: []macports.Dependency{{Port: "py-setuptools", Phase: "build"}}}
		if strings.Contains(string(text), "homepage https://harbor.dev") {
			info.Options["homepage"] = "https://harbor.dev"
		}
		return info, true
	}
	before := "PortGroup python 1.0\nname py-harbor\n"
	after := before + "version 1.8.1\nhomepage https://harbor.dev\ndepends_build-append port:py-setuptools\ndepends_lib-append port:py-click\n"
	findings := CheckRestated([]Portfile{{Path: "python/py-harbor/Portfile", Before: before, After: after}}, evaluate)
	require.Equal(t, []Finding{
		{Code: "restated", Severity: Warning, Where: "python/py-harbor/Portfile:3", Message: `"version 1.8.1" restates what its PortGroups or MacPorts set already: without it, the port evaluates the same`},
		{Code: "restated", Severity: Warning, Where: "python/py-harbor/Portfile:5", Message: `"depends_build-append port:py-setuptools" restates what its PortGroups or MacPorts set already: without it, the port evaluates the same`},
	}, findings, "homepage changes the evaluation; py-click isn't declared without its line; name was there")
	require.Empty(t, CheckRestated([]Portfile{{Path: "python/py-harbor/Portfile", Before: before, After: after}}, func(string, []byte) (macports.PortInfo, bool) { return macports.PortInfo{}, false }))
}
