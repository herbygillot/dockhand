package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// jqIsOutdated stands in for upstream discovery: jq has 1.8.1, and lost
// can't be checked. With uncertain, yq may have 5.0, which was set aside.
type jqIsOutdated struct {
	asked     []engine.OutdatedRequest
	uncertain bool
}

// yqMayBeOutdated is a port whose newest release is uncertain: v5.0
// compares newer, but was tagged on a commit older than v4.44.1's.
var yqMayBeOutdated = engine.OutdatedPort{Port: "yq", Current: "4.44.1", Newest: "5.0", Uncertain: []engine.SetAside{{Tag: "v5.0", Version: "5.0", Source: "5.0", Predates: "v4.44.1"}}}

func (j *jqIsOutdated) Outdated(_ context.Context, _ model.ObjectID, request engine.OutdatedRequest) ([]engine.OutdatedPort, error) {
	j.asked = append(j.asked, request)
	if request.Progress != nil {
		for done := range 3 {
			request.Progress(done, 2)
		}
	}
	ports := []engine.OutdatedPort{
		{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true},
		{Port: "lost", Problem: "no forge could be found for it"},
	}
	if j.uncertain {
		ports = append(ports, yqMayBeOutdated)
	}
	return ports, nil
}

func withOutdated(t *testing.T) *jqIsOutdated {
	reader := &jqIsOutdated{}
	testOutdatedReader = reader
	t.Cleanup(func() { testOutdatedReader = nil })
	return reader
}

// A report where nothing is newer says so in words that fit its count: a
// port named alone by its name, and several as none of them.
func TestOutdatedSaysWhenNothingIsNewer(t *testing.T) {
	current := func(name string) engine.OutdatedPort {
		return engine.OutdatedPort{Port: name, Current: "1.8.2", Newest: "1.8.2"}
	}
	said := func(ports ...engine.OutdatedPort) string {
		var out bytes.Buffer
		require.NoError(t, writeOutdated(t.Context(), nil, &out, engine.OutdatedReport{Master: "1bb30d5aaaaa", Ports: ports}, false))
		return out.String()
	}
	require.Equal(t, "jq has no newer release, at master 1bb30d5\n", said(current("jq")))
	require.Equal(t, "None of 2 ports has a newer release, at master 1bb30d5\n", said(current("jq"), current("fd")))
	require.Equal(t, "None of 2 ports has a newer release, at master 1bb30d5 · 1 couldn't be checked (--all says why)\n",
		said(engine.OutdatedPort{Port: "jq", Problem: "no forge"}, current("fd")), "a port that couldn't be checked isn't said to have none")

	// Where none could be checked, as offline, each says why, and the
	// command fails rather than guess (the M1's quick stage, D-N1).
	var offline bytes.Buffer
	err := writeOutdated(t.Context(), nil, &offline, engine.OutdatedReport{Master: "1bb30d5aaaaa", Ports: []engine.OutdatedPort{{Port: "jq", Current: "1.8.2", Problem: "dial tcp: connection refused\ngit ls-remote: exit status 128"}}}, false)
	require.EqualError(t, err, "no port could be checked, at master 1bb30d5: dial tcp: connection refused")
	require.Equal(t, "  PORT   NOW     NEWEST   DOCKHAND CAN\n  jq     1.8.2   ?        couldn't check: dial tcp: connection refused; git ls-remote: exit status 128\n", offline.String())

	// Nor is one whose newest release is uncertain, which is listed, with
	// why and the update that takes it (the update-workflow review's
	// finding 6).
	row := "  PORT   NOW      NEWEST   DOCKHAND CAN\n  yq     4.44.1   5.0?     update yq 5.0 after a look: v5.0 compares newer, but its commit is older than v4.44.1's\n"
	settle := "Where a tag set aside is an old one spelled oddly rather than a release, a livecheck.regex that skips it keeps it out of later looks.\n"
	require.Equal(t, row+"yq may have a newer release, at master 1bb30d5\n"+settle, said(yqMayBeOutdated))
	require.Equal(t, row+"None of 2 ports has a newer release, at master 1bb30d5 · 1 may have one, for a look\n"+settle, said(yqMayBeOutdated, current("jq")))

	// Subports checked with a sibling that's listed are said on its row
	// (field testing, 2026-10-02).
	follow := func(name string) engine.OutdatedPort {
		return engine.OutdatedPort{Port: name, Current: "25.9.23", Newest: "25.9.23", With: "py-flatbuffers"}
	}
	var folded bytes.Buffer
	require.NoError(t, writeOutdated(t.Context(), nil, &folded, engine.OutdatedReport{Master: "1bb30d5aaaaa", Ports: []engine.OutdatedPort{
		{Port: "py-flatbuffers", Current: "25.9.23", Newest: "25.9.23"}, follow("py313-flatbuffers"), follow("py314-flatbuffers")}}, true))
	require.Equal(t, "  PORT             NOW       NEWEST    DOCKHAND CAN\n  py-flatbuffers   25.9.23   25.9.23   nothing; it is current (2 subports with it)\nNone of 3 ports has a newer release, at master 1bb30d5\n", folded.String())

	// A port with no release to look for, as a _select port, is covered,
	// not a port that couldn't be checked (batch 37).
	own := func(name string) engine.OutdatedPort {
		return engine.OutdatedPort{Port: name, Current: "0.1", OwnVersion: true}
	}
	require.Equal(t, "kubectl_select has no release to look for, at master 1bb30d5: it fetches nothing here, and no livecheck reads its version\n", said(own("kubectl_select")))
	require.Equal(t, "None of 2 ports has a newer release, at master 1bb30d5 · 1 has no release to look for\n", said(own("kubectl_select"), current("jq")))
	var out bytes.Buffer
	require.NoError(t, writeOutdated(t.Context(), nil, &out, engine.OutdatedReport{Master: "1bb30d5aaaaa", Ports: []engine.OutdatedPort{own("helm_select"), own("kubectl_select")}}, true))
	require.Equal(t, "  PORT             NOW   NEWEST   DOCKHAND CAN\n  helm_select      0.1   —        nothing; it fetches nothing here, and no livecheck reads its version\n  kubectl_select   0.1   —        nothing; it fetches nothing here, and no livecheck reads its version\nNone of 2 ports has a newer release, at master 1bb30d5 · 2 have no release to look for\n", out.String())
}

func TestOutdatedThenUpdateOutdated(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	reader := withOutdated(t)

	_, _, err := dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `--mine needs to know who you are, as your ports' maintainers lines name you: set maintainer = "{@you example.org:you}" in `+filepath.Join(w.home, ".dockhand", "config.toml"))
	t.Setenv("DOCKHAND_CONFIG", filepath.Join(w.home, "elsewhere.toml"))
	_, _, err = dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `in `+filepath.Join(w.home, "elsewhere.toml"), "the file read, not the default (the dogfood run with 251a1264)")
	t.Setenv("DOCKHAND_CONFIG", "")
	config := filepath.Join(w.home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n"), data...), 0o644))

	out, _, err := dockhand(t, "outdated", "--mine")
	require.NoError(t, err)
	require.Equal(t, []string{"@ada", "example.org:ada"}, reader.asked[0].Maintainers)
	require.Contains(t, out, "  PORT   NOW     NEWEST   DOCKHAND CAN\n  jq     1.7.1   1.8.1    update\n")
	require.Contains(t, out, "1 of 2 ports has a newer release, at master ")
	require.Contains(t, out, " · 1 couldn't be checked (--all says why)\n")
	out, _, err = dockhand(t, "outdated", "--mine", "--all")
	require.NoError(t, err)
	require.Contains(t, out, "couldn't check: no forge could be found for it")

	// --outdated takes any number of ports, and --plan starts none.
	out, _, err = dockhand(t, "update", "--outdated", "--plan", "jq", "lost", "gawk")
	require.NoError(t, err)
	require.Equal(t, []string{"jq", "lost", "gawk"}, reader.asked[len(reader.asked)-1].Ports)
	require.Contains(t, out, "Will start 1 branch, one per port (unrelated ports go in separate PRs):\n  dockhand/jq-1.8.1  jq 1.7.1 → 1.8.1\n", "a row a port, with the name the run uses")
	require.Contains(t, out, "Nothing was started (--plan).\n")

	out, _, err = dockhand(t, "update", "--outdated", "--mine", "--check")
	require.ErrorContains(t, err, "without a terminal, --yes starts what is shown")
	require.Contains(t, out, "Will start 1 branch, one per port (unrelated ports go in separate PRs):\n  dockhand/jq-")
	require.Contains(t, out, "Skipped: lost (no forge could be found for it)\n")

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"update", "--outdated", "--mine", "--check"}, Streams{In: strings.NewReader("y\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? go ahead? [y/N] ")
	require.Regexp(t, `  ✓ jq-1\.8\.1: 1\.7\.1 → 1\.8\.1, one commit, check-1 queued\n`, stdout.String())
	require.Contains(t, stdout.String(), "1 branch updated and tidied into one commit each; 1 check queued\nserve isn't running: dockhand serve, or dockhand wait to run them here\n")

	out, _, err = dockhand(t, "outdated", "jq")
	require.NoError(t, err)
	require.Regexp(t, `jq     1\.7\.1   1\.8\.1    already in jq-1\.8\.1\n`, out)
	out, _, err = dockhand(t, "update", "--outdated", "jq", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Nothing to update: none of them has a newer release that isn't already in a branch.\nSkipped: jq (already in jq-")

	_, _, err = dockhand(t, "update", "--mine")
	require.ErrorContains(t, err, "--mine and --check go with --outdated")
	_, _, err = dockhand(t, "update")
	require.ErrorContains(t, err, "name the port to update, or update your outdated ports with --outdated --mine")
}

// update --outdated starts nothing for a port whose newest release is
// uncertain, and says why and what takes it; outdated's JSON lists what was
// set aside (the update-workflow review's finding 6).
func TestUpdateOutdatedLeavesAnUncertainPortForALook(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	reader := withOutdated(t)
	reader.uncertain = true

	out, _, err := dockhand(t, "update", "--outdated", "--plan", "jq", "lost", "yq")
	require.NoError(t, err)
	require.Contains(t, out, "Will start 1 branch, one per port (unrelated ports go in separate PRs):\n  dockhand/jq-")
	require.Contains(t, out, "Skipped: yq (v5.0 compares newer, but its commit is older than v4.44.1's; after a look, dockhand update yq 5.0)\n")

	result, err := jsonOf(t, "outdated", "jq", "lost", "yq")
	require.NoError(t, err)
	yq := dig(t, result.Result, "ports", 2)
	require.Equal(t, "yq", dig(t, yq, "port"))
	require.Equal(t, false, dig(t, yq, "outdated"))
	require.Equal(t, "5.0", dig(t, yq, "newest"))
	require.Equal(t, []any{map[string]any{"tag": "v5.0", "version": "5.0", "source": "5.0", "predates": "v4.44.1"}}, dig(t, yq, "uncertain"))
	require.Nil(t, dig(t, result.Result, "ports", 0, "uncertain"), "absent for a port outdated for sure")
}

// At a terminal, outdated shows how many ports are looked up on a line it
// redraws, and clears it once they all are; elsewhere it shows nothing.
func TestOutdatedShowsItsProgressAtATerminal(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	reader := withOutdated(t)
	var out, errs bytes.Buffer
	err := Run(t.Context(), []string{"outdated", "jq", "lost"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Equal(t, "\r\033[KLooking up each port's newest release: 0 of 2\r\033[KLooking up each port's newest release: 1 of 2\r\033[K", errs.String())
	require.NotContains(t, out.String(), "Looking up")

	_, errOut, err := dockhand(t, "outdated", "jq", "lost")
	require.NoError(t, err)
	require.NotContains(t, errOut, "Looking up", "no terminal, no count drawn")
	require.NotNil(t, reader.asked[1].Progress, "the count is still followed, for a look cut short")
}

// cutShort stands in for a look interrupted after jq was found, and before
// lost was: it cancels the command, as Ctrl-C does.
type cutShort struct{ cancel context.CancelFunc }

func (c cutShort) Outdated(_ context.Context, _ model.ObjectID, request engine.OutdatedRequest) ([]engine.OutdatedPort, error) {
	if request.Progress != nil {
		request.Progress(1, 2)
	}
	c.cancel()
	return []engine.OutdatedPort{{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true}}, context.Canceled
}

// A look interrupted part way prints what it found, which is still true,
// and says how far it got; update --outdated then starts nothing.
func TestAnInterruptedLookPrintsWhatItFound(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	for _, args := range [][]string{{"outdated", "jq", "lost"}, {"update", "--outdated", "jq", "lost"}} {
		ctx, cancel := context.WithCancel(t.Context())
		testOutdatedReader = cutShort{cancel: cancel}
		var out, errs bytes.Buffer
		err := Run(ctx, args, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
		require.ErrorIs(t, err, context.Canceled, args)
		require.Contains(t, out.String(), "Interrupted after looking up 1 of 2 ports", args)
		require.Contains(t, out.String(), "  jq     1.7.1   1.8.1    update\n", args)
	}
	testOutdatedReader = nil
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "update --outdated started nothing")
}

// A line printed while the count shows clears it, and draws it again after.
func TestAReportPrintsAroundTheCount(t *testing.T) {
	var err bytes.Buffer
	line := &statusLine{w: &err}
	line.show("Looking up each port's newest release: 1 of 2")
	line.say("Building the PortIndex")
	line.clear()
	require.Equal(t, "\r\033[KLooking up each port's newest release: 1 of 2\r\033[KBuilding the PortIndex\nLooking up each port's newest release: 1 of 2\r\033[K", err.String())
}

// Each port's line in a batch says what a single update says of it beyond
// its version: termusic's dropped pin and its archive not compared went
// unsaid (field testing's ninth report, 2026-10-02).
func TestABatchSaysWhatASingleUpdateWould(t *testing.T) {
	update := engine.Update{Port: "termusic", CrossesMajor: true, PatchesDropped: []string{"fix.diff"},
		Regenerated: []editprep.Regenerated{{Option: "cargo.crates", Dropped: []editprep.Override{{Name: "soundtouch", Pinned: "0.4.1", Was: "0.4.0", Locked: "0.5.4"}}}},
		Upstream:    &model.UpstreamComparison{Problem: "termusic-0.9.1.tar.gz couldn't be fetched from upstream or MacPorts' mirror"}}
	require.Equal(t, []string{
		"A new major version: what depends on termusic may need to follow.",
		"The Portfile pinned soundtouch 0.4.1 over the lock's 0.4.0; the new lock has 0.5.4, so the pin is dropped.",
		"Dropped patch fix.diff, which the new source already holds; its file goes too.",
		"! Upstream archives not compared: termusic-0.9.1.tar.gz couldn't be fetched from upstream or MacPorts' mirror",
	}, batchNotes(update))
	require.Empty(t, batchNotes(engine.Update{Port: "jq"}))
}

// Every port update --outdated is given has a line: pomo and tokei, given
// by name and already at their newest, said nothing (field testing's
// eighth report, 2026-10-02).
func TestUpdateOutdatedSaysEveryPortItWasGiven(t *testing.T) {
	var out bytes.Buffer
	plan := engine.OutdatedPlan{Skipped: []engine.SkippedUpdate{{Port: "pgdog", Reason: "already in pgdog-a1b2"}}}
	report := engine.OutdatedReport{Ports: []engine.OutdatedPort{
		{Port: "pgdog", Current: "0.1.53", Newest: "0.1.60", Outdated: true},
		{Port: "pomo", Current: "0.8.1", Newest: "0.8.1"},
		{Port: "tokei", Current: "14.0.0", Newest: "14.0.0"},
		{Port: "lost", Problem: "no forge could be found for it"},
	}}
	writeSkipped(&out, plan, report)
	require.Equal(t, "Skipped: pgdog (already in pgdog-a1b2)\nSkipped: lost (couldn't check: no forge could be found for it)\nCurrent: pomo 0.8.1, tokei 14.0.0\n", out.String())
}

// A batch's exit says whether anything worked: 1 where none could be
// done, 3 where some need a look, and 0 where all were.
func TestABatchsExitSaysWhetherAnythingWorked(t *testing.T) {
	failed := []engine.PreparedUpdate{{Planned: engine.PlannedUpdate{Name: "jq-1.8.1"}, Problem: "can't update jq by itself"}}
	done := engine.PreparedUpdate{Planned: engine.PlannedUpdate{Name: "fd-10.3.0"}, Update: engine.Update{Before: engine.PortVersion{Version: "10.2.0"}, After: engine.PortVersion{Version: "10.3.0"}}}
	var out bytes.Buffer
	require.Equal(t, 1, ExitCode(writePrepared(t.Context(), nil, &out, failed, false)), "none could be done")
	require.Equal(t, 3, ExitCode(writePrepared(t.Context(), nil, &out, append(failed, done), false)), "some need a look")
	require.NoError(t, writePrepared(t.Context(), nil, &out, []engine.PreparedUpdate{done}, false), "all were done")
}

// Every port of a batch says what its comparison did, as a single update
// does, including one that found nothing, and one that compared nothing
// (field testing's batch 10, finding 3).
func TestEveryBatchPortSaysItsComparison(t *testing.T) {
	compared := engine.Update{Upstream: &model.UpstreamComparison{Changes: []model.UpstreamChange{}}}
	require.Equal(t, []string{"Upstream source compared: no license, build file, or dependency changes."}, batchNotes(compared))
	nothing := engine.Update{Upstream: &model.UpstreamComparison{Changes: []model.UpstreamChange{}, Coverage: []model.Coverage{{Path: "x", Relevance: "unknown", Treatment: "inspected", Policy: "not-compared", Reason: "op ships a binary package"}}}}
	require.Len(t, batchNotes(nothing), 1)
	require.Contains(t, batchNotes(nothing)[0], "Upstream not compared: ")
	require.Empty(t, batchNotes(engine.Update{}), "nothing compared at all, as with --plan")
}

// A look over hundreds of ports says once what it spends of GitHub's
// hourly allowance; a small one says nothing (the M1's run at 1da4fdbf:
// 835 ports cost 2,166 requests).
func TestALargeLookSaysItsCost(t *testing.T) {
	var said []string
	l := &lookups{say: func(line string) { said = append(said, line) }}
	l.progress(0, 2)
	require.Empty(t, said)
	l.progress(1, 835)
	l.progress(2, 835)
	require.Equal(t, []string{"Looking up 835 ports asks GitHub about 2,200 times, of the 5,000 an hour a login has."}, said)
}
