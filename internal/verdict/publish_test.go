package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
)

// blockedOn builds a verdict set with a blocked run carrying a detail.
func blockedOn(plat, detail string, rest map[string]record.RunState) record.Record {
	r := set(rest)
	runOn(r, plat, record.Run{State: record.Blocked, Detail: detail})
	return r
}

func TestDecidePublishPassesAVerifiedTip(t *testing.T) {
	d := DecidePublish(PublishAsk{Record: set(map[string]record.RunState{"Sequoia": record.Passed}),
		Promotable: true, Branch: "dockhand/jq", Tip: "abc1234"})
	require.NoError(t, d.Refusal)
	assert.False(t, d.SayUnverified, "a verified tip says nothing")
	assert.Empty(t, d.Blocked)
}

// The one refusal that remains is distinct NEGATIVE evidence: a
// completed failed build. Everything else promotes with a complaint,
// because invoking promote is already the publication choice.
func TestDecidePublishRefusesOnlyAFailedBuild(t *testing.T) {
	failed := set(map[string]record.RunState{"Sequoia": record.Failed})
	d := DecidePublish(PublishAsk{Record: failed, Branch: "dockhand/jq", Tip: "abc1234"})
	require.Error(t, d.Refusal)
	assert.Equal(t,
		"dockhand/jq: tip abc1234 has a failed verification — fix it, `dockhand discard` it, or --no-verify to promote anyway",
		d.Refusal.Error())
	assert.False(t, d.SayUnverified, "a refused promotion never reaches the complaint")

	// The refusal is the failing run's verdict being enforced, so it
	// exits with that verdict rather than among the ways promote itself
	// can break.
	var refusal *FailedVerificationError
	require.ErrorAs(t, d.Refusal, &refusal)
	assert.Equal(t, exitcode.VerifyFailed, refusal.DockhandExit())
	assert.Equal(t, "verdict", exitcode.Family(refusal.DockhandExit()))
	assert.Equal(t, "verification-failed", refusal.Code())

	// --no-verify overrides exactly this refusal and nothing else.
	d = DecidePublish(PublishAsk{Record: failed, Branch: "dockhand/jq", Tip: "abc1234", NoVerify: true})
	require.NoError(t, d.Refusal)
	assert.True(t, d.SayUnverified)
}

func TestDecidePublishComplainsWithoutRefusing(t *testing.T) {
	cases := []struct {
		name   string
		states map[string]record.RunState
	}{
		{"nothing recorded at all", nil},
		{"a run still going", map[string]record.RunState{"Sequoia": record.Running}},
		{"a queued run", map[string]record.RunState{"Sequoia": record.Queued}},
		{"a claimed run not yet started", map[string]record.RunState{"Sequoia": record.Submitting}},
		{"a platform the port declines", map[string]record.RunState{"Sequoia": record.Unsupported}},
		{"a canceled run", map[string]record.RunState{"Sequoia": record.Canceled}},
		{"an errored environment", map[string]record.RunState{"Sequoia": record.Errored}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DecidePublish(PublishAsk{Record: set(tc.states), Branch: "dockhand/jq", Tip: "abc1234"})
			require.NoError(t, d.Refusal)
			assert.True(t, d.SayUnverified)
			assert.Empty(t, d.Blocked)
		})
	}
}

// A blocked run is the one unverified shape with a story worth telling:
// the maintainer deciding to promote anyway deserves the name of the
// broken neighbour in front of them. The advisories come in the
// record's stable platform order, before the complaint.
func TestDecidePublishNamesTheBlockedNeighbours(t *testing.T) {
	r := blockedOn("Sonoma", "dependency olm fails to build; the change itself is untested",
		map[string]record.RunState{"Sequoia": record.Running})
	runOn(r, "Monterey", record.Run{State: record.Blocked,
		Detail: "dependency zlib (nomaintainer) fails to build; the change itself is untested"})

	d := DecidePublish(PublishAsk{Record: r, Branch: "dockhand/jq", Tip: "abc1234"})
	require.NoError(t, d.Refusal)
	assert.True(t, d.SayUnverified)
	assert.Equal(t, []string{
		"verification blocked on Monterey: dependency zlib (nomaintainer) fails to build; the change itself is untested",
		"verification blocked on Sonoma: dependency olm fails to build; the change itself is untested",
	}, d.Blocked)

	// A refused promotion reports nothing: it never gets that far.
	runOn(r, "Ventura", record.Run{State: record.Failed})
	assert.Empty(t, DecidePublish(PublishAsk{Record: r, Branch: "dockhand/jq", Tip: "abc1234"}).Blocked)
}

func TestMergedDeadEnd(t *testing.T) {
	require.NoError(t, MergedDeadEnd(PRFact{}, "dockhand/jq"), "no PR, no dead end")
	require.NoError(t, MergedDeadEnd(open, "dockhand/jq"))
	require.NoError(t, MergedDeadEnd(closed, "dockhand/jq"))

	err := MergedDeadEnd(merged, "dockhand/jq")
	require.Error(t, err)
	assert.Equal(t,
		"PR #42 for dockhand/jq already merged (https://example.invalid/pr/42) — `dockhand clean` retires the branch",
		err.Error())
	var dead *PRMergedError
	require.ErrorAs(t, err, &dead)
	assert.Equal(t, exitcode.PRMerged, dead.DockhandExit(), "a dead end is the destination refusing, not a failure")
	assert.Equal(t, "refused", exitcode.Family(dead.DockhandExit()))
	assert.Equal(t, 42, dead.Number, "the number is the answer; a caller should not parse it back out")
}

func TestPortName(t *testing.T) {
	assert.Equal(t, "jq", PortName("jq", "anything at all"), "the note's port wins")
	// A subport's own name is what the note carries, and the title's
	// prefix must not override it.
	assert.Equal(t, "pcre2", PortName("pcre2", "pcre: update to 8.46"))
	assert.Equal(t, "jq", PortName("", "jq: update to 1.8.1"), "the project's title convention")
	assert.Equal(t, "jq", PortName("", "  jq  : update"), "the prefix is trimmed")
	assert.Empty(t, PortName("", "update everything"), "no colon, no port")
	assert.Empty(t, PortName("", ""))
}

func TestCheckDuplicates(t *testing.T) {
	mine := PRFact{Found: true, Number: 7, Title: "jq: update to 1.8.1", URL: "https://example.invalid/pr/7"}
	theirs := PRFact{Found: true, Number: 9, Title: "jq: update to 1.8.1", URL: "https://example.invalid/pr/9"}
	other := PRFact{Found: true, Number: 11, Title: "jq: fix the manpage", URL: "https://example.invalid/pr/11"}

	t.Run("no open PRs", func(t *testing.T) {
		d := CheckDuplicates(nil, PRFact{}, "jq: update to 1.8.1")
		require.NoError(t, d.Refusal)
		assert.Empty(t, d.Notes)
	})

	t.Run("a duplicate is refused with a remedy", func(t *testing.T) {
		d := CheckDuplicates([]PRFact{theirs}, PRFact{}, "jq: update to 1.8.1")
		require.Error(t, d.Refusal)
		var dup *DuplicatePRError
		require.ErrorAs(t, d.Refusal, &dup)
		assert.Equal(t, exitcode.DuplicatePR, dup.DockhandExit(), "refusal is a feature, not a failure")
		assert.Equal(t, "refused", exitcode.Family(dup.DockhandExit()), "the destination will not take it")
		assert.Equal(t,
			`an open PR already proposes "jq: update to 1.8.1": https://example.invalid/pr/9 — join it, retitle with --title, or --no-pr-check to promote anyway`,
			d.Refusal.Error())
	})

	t.Run("the comparison ignores case and surrounding space", func(t *testing.T) {
		d := CheckDuplicates([]PRFact{theirs}, PRFact{}, "  JQ: Update To 1.8.1  ")
		require.Error(t, d.Refusal)
	})

	// Re-promoting updates this branch's own PR in place; matching
	// against it would refuse the branch for duplicating itself.
	t.Run("this branch's own PR is skipped by number", func(t *testing.T) {
		d := CheckDuplicates([]PRFact{mine}, mine, "jq: update to 1.8.1")
		require.NoError(t, d.Refusal)
		assert.Empty(t, d.Notes)
	})

	t.Run("same port, different change is an advisory", func(t *testing.T) {
		d := CheckDuplicates([]PRFact{other}, PRFact{}, "jq: update to 1.8.1")
		require.NoError(t, d.Refusal)
		assert.Equal(t, []string{
			`note: an open PR already touches this port: #11 "jq: fix the manpage" (https://example.invalid/pr/11)`,
		}, d.Notes)
	})

	// The search reports each PR as it walks past, so the advisories for
	// everything ahead of a duplicate are already said by the time it is
	// found. They come back with the refusal so the caller can say them.
	t.Run("advisories ahead of a duplicate survive it", func(t *testing.T) {
		d := CheckDuplicates([]PRFact{other, theirs}, PRFact{}, "jq: update to 1.8.1")
		require.Error(t, d.Refusal)
		assert.Len(t, d.Notes, 1)
		assert.Contains(t, d.Notes[0], "#11")
	})
}
