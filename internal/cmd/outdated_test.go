package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/sweep"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// The text report is what a person reads while a paced sweep runs, so
// it is one line per port with no trailing whitespace and no buffering
// to align columns nobody asked about.
func TestOutdatedTextRow(t *testing.T) {
	line := textRow(upstream.Row{
		Port: "jq", Outcome: upstream.OutcomeOutdated,
		Current: "1.7.1", Latest: "1.8.1",
		Sha:     "0123456789abcdef0123456789abcdef01234567",
		Verdict: "agreement", Detail: "livecheck 1.8.1, forge agrees",
	})
	assert.True(t, strings.HasPrefix(line, "outdated    jq "), line)
	assert.Contains(t, line, "1.7.1 -> 1.8.1")
	assert.Contains(t, line, "0123456789ab", "git's own abbreviation length, so the sha resolves")
	assert.NotContains(t, line, "0123456789abc", "a fuller sha than git abbreviates to")
	assert.Contains(t, line, "livecheck 1.8.1")
	assert.Equal(t, strings.TrimRight(line, " "), line, "trailing padding on every line of a long report")

	// A port that is where it should be shows one version, not an
	// arrow pointing at itself.
	line = textRow(upstream.Row{Port: "jq", Outcome: upstream.OutcomeCurrent, Current: "1.8.1", Latest: "1.8.1"})
	assert.Contains(t, line, "1.8.1")
	assert.NotContains(t, line, "->")
}

// An excluded port is a row like any other. A port left out of a
// report with no line saying so is a port a reader has to notice is
// missing, and the quote is what lets a human lane act on the ones
// that were pinned by a person.
func TestOutdatedExcludedIsARow(t *testing.T) {
	row := outdatedExcluded(sweep.Excluded{
		Target: tree.Target{Portdir: "/tree/perl/p5-boolean"},
		Reason: sweep.ReplacedBy,
		Detail: `the index says this port is replaced by "p5.34-boolean"`,
	})
	assert.Equal(t, upstream.OutcomeExcluded, row.Outcome)
	assert.Equal(t, "p5-boolean", row.Port, "a portdir names its port without an evaluation")
	assert.Equal(t, "/tree/perl/p5-boolean", row.Portdir)
	assert.Contains(t, row.Detail, "p5.34-boolean")
	assert.False(t, row.Outcome.Hard())
	// The twin is a real one. A zero Twin publishes code 0 — success,
	// on a port nothing examined — under a family outside the
	// vocabulary, which is what a consumer filtering on family reads.
	assert.Equal(t, exitcode.PlanDeclined, row.Code)
	assert.Equal(t, "declined", row.Family)
	assert.Equal(t, "excluded-replaced", row.Reason)

	// A subport names itself.
	row = outdatedExcluded(sweep.Excluded{
		Target: tree.Target{Portdir: "/tree/devel/libftdi", Subport: "libftdi"},
		Reason: sweep.DoNotUpgrade,
		Detail: "a comment against the version line asks that this port not be moved",
		Quote:  "# Note: do not update past 3.8.3",
	})
	assert.Equal(t, "libftdi", row.Port)
	assert.Contains(t, row.Detail, "# Note: do not update past 3.8.3")
	assert.Contains(t, textRow(row), "do not update past 3.8.3",
		"the maintainer's own words reach the reader who has to weigh them")
}

// A port the pool never reached carries the machine's band, and it
// carries it in the JSON as well. A row that published code 0 and an
// empty family would tell a script the port was examined and found
// well, which is the one thing nobody looked at.
func TestOutdatedAbandonedCarriesTheMachinesBand(t *testing.T) {
	row := outdatedAbandoned(tree.Target{Portdir: "/tree/devel/jq"}, errors.New("tclsh died"))
	assert.Equal(t, upstream.OutcomeAbandoned, row.Outcome)
	assert.Equal(t, "jq", row.Port)
	assert.True(t, row.Outcome.Hard(), "a port nothing reached is a hard row")
	assert.Equal(t, exitcode.EvalStartup, row.Code)
	assert.Equal(t, "environment", row.Family)
	assert.Equal(t, "sweep-abandoned", row.Reason)
	assert.Contains(t, row.Detail, "tclsh died")

	b, err := json.Marshal(row)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "environment", got["family"], "the family a filter reads is a member of the vocabulary")

	// A pool that died without any replacement attempted has no cause,
	// and the row still names the band.
	row = outdatedAbandoned(tree.Target{Portdir: "/tree/devel/jq"}, nil)
	assert.Equal(t, exitcode.EvalStartup, row.Code)
}

// The default report prints what it is named for. A thousand lines of
// "current" is a report nobody reads; the census tail keeps the
// arithmetic honest for the reader who wants the number.
func TestOutdatedShowsTheExceptionsByDefault(t *testing.T) {
	quiet := outdatedAction{}
	loud := outdatedAction{current: true}
	for _, o := range upstream.Outcomes {
		row := upstream.Row{Outcome: o}
		assert.Equal(t, o != upstream.OutcomeCurrent, quiet.show(row), string(o))
		assert.True(t, loud.show(row), string(o))
	}
}

// The JSON form is one object per port with the twin inside it, so a
// caller that captured stdout through a pipe and lost $? still knows
// how each port ended.
func TestOutdatedRowJSONShape(t *testing.T) {
	b, err := json.Marshal(upstream.Row{
		Port: "jq", Portdir: "/tree/textproc/jq", Outcome: upstream.OutcomeOutdated,
		Current: "1.7.1", Latest: "1.8.1", Sha: "abc", Verdict: "agreement",
		Repo:   "https://github.com/jqlang/jq",
		Stages: []upstream.Witnessed{{Witness: upstream.WitnessLsRemote, Source: "fetched"}},
	})
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	for _, k := range []string{"port", "portdir", "outcome", "current", "latest", "sha",
		"verdict", "repo", "stages", "code", "family", "reason"} {
		assert.Contains(t, got, k, k)
	}
	// Empty testimony is omitted rather than published as an empty
	// string a filter would have to special-case.
	assert.NotContains(t, got, "livecheck")
	assert.NotContains(t, got, "detail")
}

// The verb is registered, reachable, and carries the flags the
// politeness ruling is expressed through.
func TestOutdatedCommandIsWired(t *testing.T) {
	root := Root("test")
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "outdated" {
			found = true
			for _, f := range []string{"deep", "no-cache", "ttl", "pace", "json", "current", "all", "workers"} {
				assert.NotNil(t, c.Flags().Lookup(f), f)
			}
		}
	}
	assert.True(t, found, "outdated is not reachable from the root command")
}

// The two invocations that are fine for a handful of ports and are an
// abuse of somebody else's server for thousands. Both are refused at
// the boundary and neither is clamped: a run that quietly asked a
// narrower question than the one typed would answer something nobody
// asked.
func TestOutdatedRefusesTheExpensiveInvocationsAtScale(t *testing.T) {
	// --deep is a whole MacPorts livecheck phase per port, run one at a
	// time. A category is a sweep somebody chose; the tree is not.
	deep := outdatedAction{deep: true}
	require.NoError(t, deep.refuseAtScale(deepCeiling))
	err := deep.refuseAtScale(4764)
	require.Error(t, err)
	assert.Equal(t, exitcode.Usage, ExitCode(err))
	assert.Contains(t, err.Error(), "4764")
	assert.Contains(t, err.Error(), "no batching strategy anybody has ruled on")

	// The default report is staged and stays allowed at any scale.
	require.NoError(t, outdatedAction{}.refuseAtScale(4764))

	// --pace has a floor, and the floor is a selector's. One port may
	// be asked for as fast as the user likes.
	fast := outdatedAction{pace: time.Millisecond}
	require.NoError(t, fast.refuseAtScale(1))
	err = fast.refuseAtScale(4764)
	require.Error(t, err)
	assert.Equal(t, exitcode.Usage, ExitCode(err))
	assert.Contains(t, err.Error(), "floor")
	require.NoError(t, outdatedAction{pace: paceFloor}.refuseAtScale(4764))
	require.NoError(t, outdatedAction{}.refuseAtScale(4764), "the default pace is not a floor violation")
}
