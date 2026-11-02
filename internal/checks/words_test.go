package checks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// Trouble no other attempt was made at says why: its own words where it
// gives them, as a guest program someone killed, without "another attempt
// won't fix it", which a kill's next attempt might (rc7's G9 on the M1).
func TestTroubleNotTriedAgainSaysWhy(t *testing.T) {
	t.Parallel()
	killed := buildenv.NotTriedAgain("the guest program in /opt/local was killed; no other attempt was made")
	require.ErrorIs(t, killed, buildenv.ErrNeedsAttention)
	require.Equal(t, "local: the guest program in /opt/local was killed; no other attempt was made", notTriedWords("local", killed))
	refused := buildenv.Infrastructure("%w: sudo refused", buildenv.ErrNeedsAttention)
	require.Equal(t, "local: provider infrastructure failed: another attempt won't fix it: sudo refused; another attempt won't fix it, so none was made", notTriedWords("local", refused))
}

// An unmet minimum Xcode says what the environment has, where the plan
// kept it: "needs Xcode 26.0, which Tart macOS 15 hasn't" left the person
// to find 16.4 (batch 28's leftover).
func TestAnUnmetMinimumSaysWhatTheEnvironmentHas(t *testing.T) {
	t.Parallel()
	unmet := model.Unmet{Target: "sand-runner", Needs: model.RequiresXcodeVersion("26.0"), Has: "16.4"}
	require.Equal(t, "needs Xcode 26.0 (Xcode 16.4 there)", UnmetWords(unmet))
	unmet.Has, unmet.Through = "none", "sand-runner"
	require.Equal(t, "needs Xcode 26.0 (only the Command Line Tools there), through sand-runner", UnmetWords(unmet))
	unmet.Has = ""
	require.Equal(t, "needs Xcode 26.0, through sand-runner", UnmetWords(unmet), "a plan made before it was kept")
}

// A plan nothing can be built from says why: each unresolved target and
// its reason, or that it builds nothing.
func TestAnUnrunnablePlanSaysWhy(t *testing.T) {
	t.Parallel()
	require.Equal(t, "it builds nothing", UnresolvedWords(model.Plan{}))
	require.Equal(t, "jq: no Portfile; fd: excluded on arm64", UnresolvedWords(model.Plan{Unresolved: []model.Unresolved{
		{Target: model.Target{Name: "jq"}, Reason: "no Portfile"}, {Target: model.Target{Name: "fd"}, Reason: "excluded on arm64"}}}))
}

// Kept archives found missing are said once a check: one by its port and
// digest, several by their count and ports, where a DOCKHAND_DB copy
// without its archives said each of 58 on a line of its own.
func TestMissingArchivesAreSaidOnce(t *testing.T) {
	t.Parallel()
	require.Equal(t, "check-3: the saved archive for rust (sha256:aa) is missing. Removing its record; the build environment will use a MacPorts archive or build it from source",
		goneWords("check-3", []model.DependencyArchive{{Port: "rust", Digest: "sha256:aa"}}))
	require.Equal(t, "check-3: 3 saved archives are missing, of rust and gcc15. Removing their records; the build environment will use MacPorts archives or build them from source",
		goneWords("check-3", []model.DependencyArchive{{Port: "rust", Digest: "sha256:aa"}, {Port: "gcc15", Digest: "sha256:bb"}, {Port: "rust", Digest: "sha256:cc"}}))
}

// A check that reused builds over a change that builds nothing
// differently says what changed since, naming the options and each port
// judged, never the rule, and lint's: run again here, or the earlier
// Portfile's lines kept, and why.
func TestACarriedReuseSaysWhatChangedSince(t *testing.T) {
	t.Parallel()
	maintainers := model.Carried{Check: "check-766", Commit: "3f2a1c9e0b", Ports: []model.InertPort{{Port: "omnidiff", Options: []string{"maintainers"}}}, Linted: true}
	require.Equal(t, "check-767 reused check-766's builds (commit 3f2a1c9): since then only omnidiff's maintainers changed, which builds nothing differently, and lint ran again on this Portfile",
		CarriedWords("check-767", []model.Carried{maintainers, maintainers}), "one port in two environments")
	moved := model.Carried{Check: "check-767", Ports: []model.InertPort{{Port: "omnidiff", Options: []string{"categories"}, From: "devel/omnidiff"}, {Port: "omnidiff-data", Options: []string{"description", "homepage"}}}, LintKept: "lint can't run on this Mac"}
	require.Equal(t, "check-768 reused check-767's builds: since then only omnidiff's categories and omnidiff-data's description and homepage changed, and omnidiff moved from devel/omnidiff, which builds nothing differently; lint's lines are the earlier Portfile's, since lint can't run on this Mac",
		CarriedWords("check-768", []model.Carried{moved}))
	only := model.Carried{Check: "check-767", Ports: []model.InertPort{{Port: "omnidiff", From: "devel/omnidiff"}}}
	require.Equal(t, "check-768 reused check-767's builds: since then only omnidiff moved from devel/omnidiff, which builds nothing differently", CarriedWords("check-768", []model.Carried{only}))
	require.Empty(t, CarriedWords("check-768", nil))
}
