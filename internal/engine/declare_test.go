package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// What a branch's passed check found its build linking and not
// declaring, tidy declares in a commit of its own, after the branch's,
// with the check's evidence in its body; serve does it unattended where
// it held, and the branch waits for its check, which reuses the build
// (#49; docs/designs/declare-linked-libraries.md, parts 2 and 4).
func TestTidyDeclaresWhatTheCheckFoundLinked(t *testing.T) {
	t.Parallel()
	f := setup(t)
	testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nchecksums           sha256  0\n"})
	testsupport.Git(t, f.upstream, "commit", "-qam", "jq: checksums")
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	lzma := map[model.TargetID][]model.Link{"jq": {{Binary: "/opt/local/bin/jq", Library: "/opt/local/lib/liblzma.5.dylib", Port: "xz"}}}
	branch := preparedWith(t, e, model.OriginServe, &scriptedProvider{active: []model.ActivePort{{Name: "xz"}}, readsLinks: true, links: lzma}, port("jq"))

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.NotNil(t, plan.Declare)
	require.Equal(t, "jq: declare xz", plan.Declare.Subject())
	require.Contains(t, plan.Declare.Message, "check-1's builds of jq on")
	require.Contains(t, plan.Declare.Message, "link liblzma.5.dylib from xz, which the Portfile didn't declare.")
	require.Equal(t, plan.Declare.Subject(), plan.Series()[len(plan.Series())-1].Subject(), "after the branch's own commits")

	candidates, _, err := e.serveCandidates(t.Context())
	require.NoError(t, err)
	require.Empty(t, candidates, "declared, it waits for its check")
	require.Contains(t, testsupport.Git(t, branch.Worktree, "show", "HEAD:textproc/jq/Portfile"), "checksums           sha256  0\n\ndepends_lib-append  port:xz")
	require.Equal(t, "jq: declare xz", testsupport.Git(t, branch.Worktree, "log", "-1", "--format=%s"))
}

// A finding tidy can't write is said and left to the person: a variant
// build's, a subport's of a Portfile with several, and libiconv where the
// port links the system's with rust.system_libiconv yes, whose build
// linking MacPorts' says C code it builds uses iconv, so the option goes
// and the rust PortGroup declares it (macports/macports-ports#36385).
func TestAFindingTidyCantWriteIsSaid(t *testing.T) {
	t.Parallel()
	finding := evidence.LinkFinding{Target: "omnidiff", Port: "libiconv", Library: "/opt/local/lib/libiconv.2.dylib"}
	plain := []byte("PortSystem 1.0\nPortGroup cargo 1.0\nname omnidiff\nchecksums sha256 0\n")
	require.Empty(t, undeclarable(plain, model.Target{Name: "omnidiff"}, finding))
	system := []byte("PortSystem 1.0\nPortGroup cargo 1.0\nname omnidiff\nrust.system_libiconv yes\nchecksums sha256 0\n")
	require.Equal(t, "omnidiff sets rust.system_libiconv yes, but its build links MacPorts' libiconv (C code it builds uses iconv): drop the option, and the rust PortGroup declares port:libiconv",
		undeclarable(system, model.Target{Name: "omnidiff"}, finding))
	require.True(t, strings.HasSuffix(undeclarable(plain, model.Target{Name: "omnidiff", Variants: map[string]bool{"tests": true}}, finding), "in its +tests build alone; the line belongs in that variant's body, yours to write"))
	several := []byte("name omnidiff\nchecksums sha256 0\nsubport omnidiff-devel {}\n")
	require.True(t, strings.HasSuffix(undeclarable(several, model.Target{Name: "omnidiff"}, finding), "its Portfile has several ports, and the line belongs in the port's own block, yours to write"))
}
