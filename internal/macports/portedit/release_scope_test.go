package portedit

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestSharedReleaseRequiresAuthorizationAndPreservesIndependentSibling(t *testing.T) {
	t.Parallel()
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{false: "refuse", true: "prepare"}[authorized], func(t *testing.T) {
			s, r, requests := archiveFixture(t, `version 1.2.3
revision 2
distname shared-${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-a {}
subport fixture-b {}
subport fixture-pinned {
 version 0.9.0
 revision 7
 distname pinned-0.9.0
 master_sites @SITE@/0.9.0
 checksums sha256 bbbb size 3
}
if {${subport} eq "fixture"} {distfiles; fetch {}; use_configure no; build {}}
`)
			r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-a"}
			r.SharedRelease = authorized
			result, err := s.Prepare(t.Context(), r)
			if !authorized {
				require.ErrorIs(t, err, ErrFidelity)
				require.Empty(t, *requests)
				return
			}
			require.NoError(t, err)
			require.Len(t, *requests, 1)
			require.Len(t, result.Scope.Affected, 3)
			built := 0
			for _, member := range result.Scope.Affected {
				if !member.MetadataOnly {
					built++
				}
			}
			require.Equal(t, 2, built, "the metadata-only parent is in scope but not built")
			require.Len(t, result.Scope.Protected, 1)
			require.Equal(t, "fixture-pinned", result.Scope.Protected[0].Target.Name)
			require.Equal(t, "1.2.3", result.Scope.Input.Before)
			require.Equal(t, "1.2.4", result.Scope.Input.After)
			require.Contains(t, string(result.Files[0].After), "version 0.9.0\n revision 7")
			require.Contains(t, string(result.Files[0].After), "checksums sha256 bbbb size 3")
		})
	}
}

func TestASharedReleaseWritesEachMembersOwnChecksumsBlock(t *testing.T) {
	t.Parallel()
	s, r, requests := archiveFixture(t, `version 1.2.3
revision 0
distname shared-${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-a {}
subport fixture-b {}
if {${subport} eq "fixture-b"} {
 checksums sha256 aaaa size 2
}
`)
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-a"}
	r.SharedRelease = true
	// Each subport's checksums are located, by the frame's line, and
	// fixture-b's are its own: its archive is fetched for it, and its block
	// written, where a shared release refused it as unprovable.
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.NotEmpty(t, *requests)
	require.NotContains(t, string(result.Files[0].After), "sha256 aaaa")
	require.Equal(t, 2, strings.Count(string(result.Files[0].After), "checksums sha256 "), "both blocks written")
}

// A Portfile whose subports each fetch an archive of their own at the one
// version, as xapian-core's bindings and omega do, is updated whole: each
// member's archive fetched at the new version and each checksums block
// written, with --shared-release, which a main port's update implies
// (field testing at 6dd7745c: xapian-core was refused, and updated by
// hand). Without it, a named subport is refused before anything is
// fetched.
func TestASharedReleaseFetchesEachMembersOwnArchive(t *testing.T) {
	t.Parallel()
	const portfile = `version 1.2.3
revision 0
master_sites @SITE@/${version}
distname fixture-core-${version}
checksums sha256 aaaa size 2
subport fixture-omega {
 distname fixture-omega-${version}
 checksums sha256 bbbb size 3
}
`
	s, r, requests := archiveFixture(t, portfile)
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/1.2.4/fixture-core-1.2.4.tar.gz", "/1.2.4/fixture-omega-1.2.4.tar.gz"}, *requests)
	require.Len(t, result.Downloads, 2)
	after := string(result.Files[0].After)
	require.NotContains(t, after, "sha256 aaaa")
	require.NotContains(t, after, "sha256 bbbb")
	require.Contains(t, after, "version 1.2.4")
	require.Len(t, result.Scope.Affected, 2)

	s, r, requests = archiveFixture(t, portfile)
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-omega"}
	_, err = s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	require.ErrorContains(t, err, "--shared-release moves both")
	require.Empty(t, *requests)
}

// An obsolete main port that is replaced_by a versioned subport and spells
// that subport's version as its own literal moves with the subport's bump,
// without shared-release authorization; the terraform Portfile has this shape.
func TestObsoleteFollowerMovesWithTheSubportItIsReplacedBy(t *testing.T) {
	t.Parallel()
	const portfile = `subport fixture-1 {
 version 1.2.3
 revision 0
 distname shared-${version}
 master_sites @SITE@/${version}
 checksums sha256 aaaa size 2
}
subport fixture-0 {
 version 0.9.0
 revision 0
 distname old-${version}
 master_sites @SITE@/${version}
 checksums sha256 bbbb size 3
}
if {${subport} eq ${name}} {
 replaced_by fixture-1
 version 1.2.3
 revision 0
 distfiles
 fetch {}
 use_configure no
 build {}
}
`
	s, r, _ := archiveFixture(t, portfile)
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-1"}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after := string(result.Files[0].After)
	require.Equal(t, 2, strings.Count(after, "version 1.2.4"), "the subport and its obsolete follower both move")
	require.Contains(t, after, "replaced_by fixture-1\n version 1.2.4")
	require.Contains(t, after, "version 0.9.0", "an unrelated series is untouched")
	require.NotNil(t, result.Scope)
	require.Len(t, result.Scope.Affected, 2)
	require.Equal(t, "fixture", result.Scope.Affected[0].Target.Name)
	require.True(t, result.Scope.Affected[0].MetadataOnly, "an obsolete port builds nothing")
	require.True(t, result.Scope.Affected[0].Follower, "the scope records why it moved")
	require.Equal(t, "1.2.4", result.Scope.Affected[0].After.Version)
	require.Equal(t, "fixture-1", result.Scope.Affected[1].Target.Name)
	require.False(t, result.Scope.Affected[1].Follower)
	require.False(t, result.Scope.Affected[1].MetadataOnly, "only the subport is built")
	for _, member := range result.Scope.Affected {
		require.False(t, member.NeedsAuthorization("fixture-1"), "what the editor moved needs no shared-release authorization")
	}

	// A main port at a different version is not a follower and is left alone.
	s, r, _ = archiveFixture(t, strings.Replace(portfile, "replaced_by fixture-1\n version 1.2.3", "replaced_by fixture-1\n version 1.1.0", 1))
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-1"}
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	after = string(result.Files[0].After)
	require.Equal(t, 1, strings.Count(after, "version 1.2.4"))
	require.Contains(t, after, "version 1.1.0")
	require.Nil(t, result.Scope, "nothing but the target moved")
}

// A stub selection is probed through its newest subport, which borrows the
// stub's livecheck: MacPorts disables livecheck on subports that share the
// stub's version, and the release they share is discovered once.
func TestStubProbeBorrowsTheStubsLivecheck(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
revision 0
distname shared-${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-313 { livecheck.type none }
subport fixture-314 { livecheck.type none }
if {${subport} eq ${name}} {
 distfiles
 fetch {}
 use_configure no
 build {}
 livecheck.type regex
 livecheck.url @SITE@/releases
 livecheck.regex {shared-(\d+(\.\d+)+)\.tar}
}
`)
	probe, err := s.Probe(t.Context(), ProbeSource{Source: r.Source, Workspace: r.Workspace, Selection: macports.Selection{Selector: "fixture"}, Platform: r.Platform})
	require.NoError(t, err)
	port := probe.Port()
	require.Equal(t, "fixture-314", port.Name, "the newest subport carries the edit")
	require.Equal(t, "regex", port.Options["livecheck.type"], "and discovers through the stub's livecheck")
	require.Contains(t, port.Options["livecheck.url"], "/releases")
	require.Equal(t, "1", port.Options["dockhand.livecheck_standard"])
}

// The ruby PortGroup's stub returns before declaring a livecheck while each
// subport declares the RubyGems check: the newest subport keeps its own
// livecheck rather than borrowing the stub's absent one.
func TestStubProbeKeepsAMembersOwnLivecheck(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, `version 1.2.3
revision 0
distname shared-${version}
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
subport fixture-33 {
 livecheck.type regex
 livecheck.url @SITE@/gems
 livecheck.regex {shared-(\d+(\.\d+)+)\.tar}
}
subport fixture-34 {
 livecheck.type regex
 livecheck.url @SITE@/gems
 livecheck.regex {shared-(\d+(\.\d+)+)\.tar}
}
if {${subport} eq ${name}} {
 distfiles
 fetch {}
 use_configure no
 build {}
}
`)
	probe, err := s.Probe(t.Context(), ProbeSource{Source: r.Source, Workspace: r.Workspace, Selection: macports.Selection{Selector: "fixture"}, Platform: r.Platform})
	require.NoError(t, err)
	port := probe.Port()
	require.Equal(t, "fixture-34", port.Name)
	require.Equal(t, "regex", port.Options["livecheck.type"], "the member's own livecheck stands")
	require.Contains(t, port.Options["livecheck.url"], "/gems")
}

// A refused update's attempt, for a person to finish by hand: the refused
// edit, with its checksums refreshed where it had none of the new
// version's, and, where the refusal came with no edit, the version
// written as its literal, its revision reset, and its checksums refreshed
// (update --hand-edit, Then's item 3).
func TestARefusedUpdatesAttemptIsWrittenForAHandEdit(t *testing.T) {
	t.Parallel()
	const portfile = `version 1.2.3
revision 1
master_sites @SITE@/${version}
distname fixture-core-${version}
checksums sha256 aaaa size 2
subport fixture-omega {
 distname fixture-omega-${version}
 checksums sha256 bbbb size 3
}
`
	s, r, requests := archiveFixture(t, portfile)
	r.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-omega"}
	refused, err := s.Prepare(t.Context(), r)
	require.ErrorIs(t, err, ErrFidelity)
	attempt, err := s.HandAttempt(t.Context(), r, refused)
	require.NoError(t, err)
	require.Len(t, attempt.Files, 1)
	after := string(attempt.Files[0].After)
	require.Contains(t, after, "version 1.2.4")
	require.NotContains(t, after, "sha256 bbbb", "the subport's checksums are the new version's")
	require.NotEmpty(t, *requests)

	s, r, _ = archiveFixture(t, "version 1.2.3\nrevision 2\nmaster_sites @SITE@/${version}\nchecksums sha256 aaaa size 2\n")
	attempt, err = s.HandAttempt(t.Context(), r, Result{})
	require.NoError(t, err)
	after = string(attempt.Files[0].After)
	require.Contains(t, after, "version 1.2.4")
	require.Contains(t, after, "revision 0")
	require.NotContains(t, after, "sha256 aaaa")
	require.Empty(t, attempt.Left)
}

// A refusal after the version was written, as a calculated checksum
// algorithm's, starts the attempt from the version as the plan wrote it,
// a github.setup's version word as well as a version command, and says
// the checksums it couldn't write (field testing's probe of --hand-edit
// on jq, 2026-10-25).
func TestARefusalAfterTheVersionStartsTheAttemptFromIt(t *testing.T) {
	t.Parallel()
	s, r, _ := archiveFixture(t, "version 1.2.3\nrevision 0\nmaster_sites @SITE@/${version}\nchecksums [join {sha 256} \"\"] aaaa size 2\n")
	refused, err := s.Prepare(t.Context(), r)
	require.ErrorContains(t, err, "calculated checksum algorithm")
	require.Contains(t, string(refused.Versioned), "version 1.2.4", "the version as the plan wrote it")
	attempt, err := s.HandAttempt(t.Context(), r, refused)
	require.NoError(t, err)
	require.Contains(t, string(attempt.Files[0].After), "version 1.2.4")
	require.Equal(t, []ChecksumLeft{{Kind: LeftDeclaration, What: "the checksums", Reason: "calculated checksum algorithm"}}, attempt.Left)
}
