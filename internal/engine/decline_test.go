package engine

// A cohort with a member that refuses the platform before anything
// boots. The refusal is a fact about that port, so the others are still
// built — and everything the note says about the change has to survive
// a member being left out of the build.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// evaluablePortfile is the smallest Portfile a real evaluation will
// answer questions about. The tests below need the pre-flight to
// actually run, which the one-line fixtures elsewhere in this package
// deliberately do not.
func evaluablePortfile(name, category, extra string) string {
	return `PortSystem          1.0
name                ` + name + `
version             1.0
categories          ` + category + `
platforms           darwin
maintainers         nomaintainer
description         d
long_description    d
homepage            https://example.org
distfiles
` + extra
}

// decliningCohort is a hand-made two-portdir branch whose SECOND member
// in build order declares known_fail. Second is the whole point: a
// record that learned its subjects from the refusal would put the
// declining port at the head of the change.
func decliningCohort(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{
		"sysutils/jq/Portfile":        evaluablePortfile("jq", "sysutils", ""),
		"textproc/oniguruma/Portfile": evaluablePortfile("oniguruma", "textproc", ""),
	})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha, err := repo.Mint(ctx, git.MintRequest{
		Branch: "dockhand/jq-1.8", Base: primary, Commits: []git.Commit{{
			Files: []git.File{
				{Path: "sysutils/jq/Portfile",
					Content: []byte(strings.Replace(evaluablePortfile("jq", "sysutils", ""),
						"version             1.0", "version             1.8", 1))},
				{Path: "textproc/oniguruma/Portfile",
					Content: []byte(evaluablePortfile("oniguruma", "textproc", "known_fail          yes\n"))},
			},
			Message: "jq: update to 1.8, with oniguruma rebuilt",
		}},
	})
	require.NoError(t, err)
	return repo, sha
}

// cohortMembers is the roster in build order, headline first.
var cohortMembers = []Member{
	{Port: "jq", Portdir: "sysutils/jq"},
	{Port: "oniguruma", Portdir: "textproc/oniguruma"},
}

// The declining member is left out of the build and left where it
// stands in the change. Subjects are built by adoption for a branch
// nobody minted, adoption appends in call order, and the refusal is
// written before the surviving members are — so a roster learned from
// the refusals would be headlined by whichever port pre-flight threw
// out first. Headline() is the branch's resolution, the pull request's
// target, and the member an unattributable failure lands on.
func TestAPreflightDeclineDoesNotRenameTheChange(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := decliningCohort(t)
	fake := &verifytest.Fake{}

	_, err := testState(t, repo, fake).SubmitRelease(ctx, repo, "dockhand/jq-1.8", sha,
		cohortMembers, fake.Capabilities().Platforms[0], false)
	require.NoError(t, err)

	require.Len(t, fake.Submitted, 1, "one guest for what is left of the cohort")
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports, "the declining member is not built")

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"jq", "oniguruma"}, n.Ports(), "build order, whoever declined")
	assert.Equal(t, "jq", n.Headline().Port, "pre-flight does not get to rename the change")
	assert.Equal(t, record.Unsupported, runFor(n, "oniguruma", "Testos").State)
	assert.Equal(t, record.Running, runFor(n, "jq", "Testos").State)
}

// The note says of each member what the argv said of it. Ignoring the
// binary archive is the headline's property — the change left its
// version and revision where they were, so the archive that matches
// them predates it — and a dependent riding along was built against
// its own archive, which is exactly what its verdict is about. A run
// stamped from the submission's own flag would have the pull request
// body vouch for a build from source that never happened.
func TestFromSourceIsRecordedPerMember(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	fake := &verifytest.Fake{}
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}

	require.NoError(t, testState(t, repo, fake).submit(ctx, m, submission{
		Port:       "jq",
		Release:    fake.Capabilities().Platforms[0],
		FromSource: true,
		Members:    cohortMembers,
	}))

	require.Len(t, fake.Submitted, 1)
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].FromSource, "the argv names the headline alone")

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.True(t, runFor(n, "jq", "Testos").FromSource, "the member the guest was told to build from source")
	assert.False(t, runFor(n, "oniguruma", "Testos").FromSource,
		"and the one it was not, whose archive is what its own verdict is about")
}

// And a deferral after that refusal does not overwrite it. The runs a
// deferral writes are true of the members that were going to be built;
// a member that has already said it cannot be built here would be left
// reading as queued for a build nothing will ever start, and the change
// would read as still verifying until a drain re-evaluated it.
func TestADeferralDoesNotQueueADeclinedMember(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	repo, sha := decliningCohort(t)
	fake := &verifytest.Fake{SubmitErr: &verify.CapacityError{Busy: 2, Cap: 2}}

	_, err := testState(t, repo, fake).SubmitRelease(ctx, repo, "dockhand/jq-1.8", sha,
		cohortMembers, fake.Capabilities().Platforms[0], false)
	require.Error(t, err, "a full machine defers what was left to build")

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Queued, runFor(n, "jq", "Testos").State,
		"the member that was going to be built is what the deferral is about")
	assert.Equal(t, record.Unsupported, runFor(n, "oniguruma", "Testos").State,
		"the member that declined has its answer, and a deferral is not a newer one")
	assert.Equal(t, []string{"jq", "oniguruma"}, n.Ports())
}

// A run is keyed by release name, and the release is not resolved until
// submit runs — the caller's zero Release means "whatever the provider
// offers". A withheld member recorded before that resolution lands
// under the empty string, where every lookup by release misses it, and
// the subject the state exists to keep visible goes silent instead.
//
// Caught on live data: the note held "gegl-devel@" beside "gegl@Tahoe",
// so status would have shown eight lines for nine subjects with nothing
// saying why — the exact silent drop the state was added to prevent.
func TestAWithheldMemberIsKeyedByTheResolvedRelease(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	fake := &verifytest.Fake{}
	m := &Minted{Repo: repo, Branch: "dockhand/jq-1.8", Sha: sha, RelPort: "sysutils/jq"}

	// Release deliberately left zero: this is the road that resolves it.
	require.NoError(t, testState(t, repo, fake).submit(ctx, m, submission{
		Port:     "jq",
		Members:  cohortMembers,
		Withheld: []WithheldMember{{Port: "oniguruma-devel", Why: "it conflicts with oniguruma"}},
	}))

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)

	_, unkeyed := n.Runs[record.RunKey("oniguruma-devel", "")]
	assert.False(t, unkeyed, "an empty release is not a key: nothing reading by release finds it")

	rel := fake.Capabilities().Platforms[0].Name
	run, ok := n.Runs[record.RunKey("oniguruma-devel", rel)]
	require.True(t, ok, "the withheld run belongs under the release submit resolved")
	assert.Equal(t, record.Withheld, run.State)
	assert.Equal(t, rel, run.Platform, "and it names that release to a reader too")

	assert.NotContains(t, fake.Submitted[0].Ports, "oniguruma-devel",
		"withheld means withheld: the guest is never asked to build it")
}
