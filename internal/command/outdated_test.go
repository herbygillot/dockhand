package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	// What couldn't be checked leads, the result is the rest's, and the
	// command exits 3 (the rc6 full stage, D-N4).
	var partial bytes.Buffer
	err := writeOutdated(t.Context(), nil, &partial, engine.OutdatedReport{Master: "1bb30d5aaaaa", Ports: []engine.OutdatedPort{{Port: "jq", Problem: "GitHub's rate limit for your login resets in 18 minutes, at 23:24 EDT"}, current("fd")}}, false)
	require.Equal(t, 3, ExitCode(err))
	require.Equal(t, "1 of 2 ports couldn't be checked: GitHub's rate limit for your login resets in 18 minutes, at 23:24 EDT\nOf the 1 port checked, none has a newer release, at master 1bb30d5 · --current lists those it couldn't check\n",
		partial.String(), "a port that couldn't be checked isn't said to have none")

	// Where none could be checked, as offline, each says why, and the
	// command fails rather than guess (the M1's quick stage, D-N1).
	var offline bytes.Buffer
	err = writeOutdated(t.Context(), nil, &offline, engine.OutdatedReport{Master: "1bb30d5aaaaa", Ports: []engine.OutdatedPort{{Port: "jq", Current: "1.8.2", Problem: "dial tcp: connection refused\ngit ls-remote: exit status 128"}}}, false)
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

	// A newer release whose files aren't published yet is said as not
	// ready, as update would refuse it, and isn't counted as newer (field
	// testing at 43949b89, browsh 1.8.3).
	unready := engine.OutdatedPort{Port: "browsh", Current: "1.8.2", Newest: "1.8.3", Outdated: true, Unpublished: "the release of v1.8.3 has no files yet, which update fetches; it's not ready"}
	notReady := "  PORT     NOW     NEWEST   DOCKHAND CAN\n  browsh   1.8.2   1.8.3    nothing yet: the release of v1.8.3 has no files yet, which update fetches; it's not ready\n"
	require.Equal(t, notReady+"browsh has a newer release, not ready yet, at master 1bb30d5\n", said(unready))
	require.Equal(t, notReady+"None of 2 ports has a newer release, at master 1bb30d5 · 1 has a release not ready yet\n", said(unready, current("jq")))
}

func TestOutdatedThenUpdateMine(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	reader := withOutdated(t)

	_, _, err := dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `--mine needs to know who you are, as your ports' maintainers lines name you: set maintainer to you as a Portfile's maintainers line writes you, such as "@you" for your GitHub login, or "{example.org:you @you}" with your email too, in `+filepath.Join(w.home, ".dockhand", "config.toml"))
	t.Setenv("DOCKHAND_CONFIG", filepath.Join(w.home, "elsewhere.toml"))
	_, _, err = dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `in `+filepath.Join(w.home, "elsewhere.toml"), "the file read, not the default (the dogfood run with 251a1264)")
	t.Setenv("DOCKHAND_CONFIG", "")
	config := filepath.Join(w.home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n"), data...), 0o644))

	out, _, err := dockhand(t, "outdated", "--mine")
	require.Equal(t, 3, ExitCode(err), "one couldn't be checked")
	require.Equal(t, []string{"@ada", "example.org:ada"}, reader.asked[0].Maintainers)
	require.Contains(t, out, "  PORT   NOW     NEWEST   DOCKHAND CAN\n  jq     1.7.1   1.8.1    update\n")
	require.Contains(t, out, "1 of 2 ports couldn't be checked: no forge could be found for it\nOf the 1 port checked, 1 has a newer release, at master ")
	require.Contains(t, out, " · --current lists those it couldn't check\n")
	out, _, err = dockhand(t, "outdated", "--mine", "--current")
	require.Equal(t, 3, ExitCode(err))
	require.Contains(t, out, "couldn't check: no forge could be found for it")

	// --mine updates each in a branch of its own, nothing committed, and
	// --plan starts none.
	out, _, err = dockhand(t, "update", "--mine", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "  lost: left as it is: no forge could be found for it\n")
	require.Contains(t, out, "Will start 1 branch, one a port:\n  dockhand/jq-1.8.1  jq 1.7.1 → 1.8.1\n", "a row a port, with the name the run uses")
	require.Contains(t, out, "Nothing was started (--plan).\n")

	_, _, err = dockhand(t, "update", "--mine")
	require.ErrorContains(t, err, "without a terminal, --yes starts what is shown")

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"update", "--mine"}, Streams{In: strings.NewReader("y\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.Contains(t, errs.String(), "? Start them? [y/N] ")
	require.Contains(t, stdout.String(), "jq: 1.7.1 → 1.8.1")
	require.NotContains(t, stdout.String(), "Proposed commit", "nothing is committed")

	out, _, err = dockhand(t, "outdated", "jq")
	require.Equal(t, 3, ExitCode(err), "the fixture's lost couldn't be checked")
	require.Regexp(t, `jq     1\.7\.1   1\.8\.1    already in jq-1\.8\.1\n`, out)
	out, _, err = dockhand(t, "update", "--mine", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  jq: left as it is: already in jq-")
	require.Contains(t, out, "Nothing to update: none of them has a newer release that isn't already in a branch.\n")

	_, _, err = dockhand(t, "update", "--mine", "jq")
	require.EqualError(t, err, "--mine starts a branch for each of your ports; it takes no port, --branch, or --new")
	_, _, err = dockhand(t, "update")
	require.ErrorContains(t, err, "name the port to update, or update your outdated ports with --mine")
}

// update --mine starts nothing for a port whose newest release is
// uncertain, and counts it for a look; outdated's JSON lists what was set
// aside (the update-workflow review's finding 6).
func TestUpdateMineLeavesAnUncertainPortForALook(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	reader := withOutdated(t)
	reader.uncertain = true
	asMaintainer(t, w)

	out, _, err := dockhand(t, "update", "--mine", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Will start 1 branch, one a port:\n  dockhand/jq-")
	require.Contains(t, out, "1 that may have one, for your look")

	result, err := jsonOf(t, "outdated", "jq", "lost", "yq")
	require.Equal(t, 3, ExitCode(err), "lost couldn't be checked")
	require.EqualValues(t, 1, dig(t, result.Result, "unchecked"))
	yq := dig(t, result.Result, "ports", 2)
	require.Equal(t, "yq", dig(t, yq, "port"))
	require.Equal(t, false, dig(t, yq, "outdated"))
	require.Equal(t, "5.0", dig(t, yq, "newest"))
	require.Equal(t, []any{map[string]any{"tag": "v5.0", "version": "5.0", "source": "5.0", "predates": "v4.44.1"}}, dig(t, yq, "uncertain"))
	require.Nil(t, dig(t, result.Result, "ports", 0, "uncertain"), "absent for a port outdated for sure")
	require.Equal(t, "uncertain", dig(t, yq, "class"))
	require.Equal(t, "newer", dig(t, result.Result, "ports", 0, "class"))
	require.Equal(t, "unchecked", dig(t, result.Result, "ports", 1, "class"))
}

// At a terminal, outdated shows how many ports are looked up on a line it
// redraws, and clears it once they all are; elsewhere it shows nothing.
func TestOutdatedShowsItsProgressAtATerminal(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	reader := withOutdated(t)
	var out, errs bytes.Buffer
	err := Run(t.Context(), []string{"outdated", "jq", "lost"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs, interactive: true})
	require.Equal(t, 3, ExitCode(err), "lost couldn't be checked")
	require.Equal(t, "\r\033[KLooking up each port's newest release: 0 of 2\r\033[KLooking up each port's newest release: 1 of 2\r\033[K", errs.String())
	require.NotContains(t, out.String(), "Looking up")

	_, errOut, err := dockhand(t, "outdated", "jq", "lost")
	require.Equal(t, 3, ExitCode(err))
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
// and says how far it got; update --mine and ship --mine then start
// nothing.
func TestAnInterruptedLookPrintsWhatItFound(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	asMaintainer(t, w)
	for _, args := range [][]string{{"outdated", "jq", "lost"}, {"update", "--mine"}, {"ship", "--mine"}} {
		ctx, cancel := context.WithCancel(t.Context())
		testOutdatedReader = cutShort{cancel: cancel}
		var out, errs bytes.Buffer
		err := Run(ctx, args, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
		require.ErrorIs(t, err, context.Canceled, args)
		require.Contains(t, out.String(), "Interrupted after looking up 1 of 2 ports", args)
		require.Contains(t, out.String(), "  jq     1.7.1   1.8.1    update\n", args)
	}
	testOutdatedReader = nil
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "update --mine and ship --mine started nothing")
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

// What a batch leaves is counted by what the look found of each, naming
// outdated, which says each.
func TestABatchCountsWhatItLeaves(t *testing.T) {
	report := engine.OutdatedReport{Ports: []engine.OutdatedPort{
		{Port: "pgdog", Current: "0.1.53", Newest: "0.1.60", Outdated: true},
		{Port: "pomo", Current: "0.8.1", Newest: "0.8.1"},
		{Port: "tokei", Current: "14.0.0", Newest: "14.0.0"},
		{Port: "lost", Problem: "no forge could be found for it"},
	}}
	require.Equal(t, "Of the rest, 2 current, 1 that couldn't be checked (dockhand outdated --mine --current says each).", restWords(report, engine.OutdatedPlan{}))
	skipped := engine.OutdatedPlan{Skipped: []engine.SkippedUpdate{{Port: "lost", Reason: "no forge could be found for it"}}}
	require.Equal(t, "Of the rest, 2 current (dockhand outdated --mine --current says each).", restWords(report, skipped), "what the plan skipped is said by name, and not counted again")
	require.Empty(t, restWords(engine.OutdatedReport{}, engine.OutdatedPlan{}))
}

// A batch's exit says whether anything worked: 1 where none could be
// done, 3 where some need a look, and 0 where all were.
func TestABatchsExitSaysWhetherAnythingWorked(t *testing.T) {
	require.Equal(t, 1, ExitCode(batchExit([]string{"jq"}, 1, "shipped")), "none could be done")
	require.Equal(t, 3, ExitCode(batchExit([]string{"jq"}, 2, "shipped")), "some need a look")
	require.NoError(t, batchExit(nil, 2, "shipped"), "all were done")
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

// Off a terminal, a look says its count once it has run a while, and
// again now and then, not each port, and not once every port is looked up
// (field testing at c9ad2112: two ports, 13 minutes, nothing said).
func TestALookOffATerminalSaysItsCountAsItGoes(t *testing.T) {
	var said []string
	l := &lookups{say: func(line string) { said = append(said, line) }, first: time.Minute, every: time.Hour, started: time.Now()}
	l.progress(1, 3)
	require.Empty(t, said, "not before it has run a while")
	l.started = time.Now().Add(-2 * time.Minute)
	l.progress(1, 3)
	l.progress(2, 3)
	l.progress(3, 3)
	require.Equal(t, []string{"Looked up 1 of 3 ports' newest releases"}, said)
}
