package outdated

import (
	"errors"
	"regexp"

	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A report where nothing is newer says so in words that fit its count: a
// port named alone by its name, and several as none of them.
func TestOutdatedSaysWhenNothingIsNewer(t *testing.T) {
	t.Parallel()
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
	require.Equal(t, 3, term.ExitCode(err))
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
	require.Equal(t, row+"yq may have a newer release, at master 1bb30d5\n"+settle, said(cmdtest.YqMayBeOutdated))
	require.Equal(t, row+"None of 2 ports has a newer release, at master 1bb30d5 · 1 may have one, for a look\n"+settle, said(cmdtest.YqMayBeOutdated, current("jq")))

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
	unready := engine.OutdatedPort{Port: "browsh", Current: "1.8.2", Newest: "1.8.3", Outdated: true, Unpublished: "release v1.8.3 has no downloadable assets yet; the update is not ready"}
	notReady := "  PORT     NOW     NEWEST   DOCKHAND CAN\n  browsh   1.8.2   1.8.3    nothing yet: release v1.8.3 has no downloadable assets yet; the update is not ready\n"
	require.Equal(t, notReady+"browsh has a newer release, not ready yet, at master 1bb30d5\n", said(unready))
	require.Equal(t, notReady+"None of 2 ports has a newer release, at master 1bb30d5 · 1 has a release not ready yet\n", said(unready, current("jq")))
}

func TestOutdatedThenUpdateMine(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	reader := cmdtest.WithOutdated(t)

	_, _, err := cmdtest.Dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `--mine needs to know who you are, as your ports' maintainers lines name you: set maintainer to you as a Portfile's maintainers line writes you, such as "@you" for your GitHub login, or "{example.org:you @you}" with your email too, in `+filepath.Join(w.Home, ".dockhand", "config.toml"))
	cmdtest.Setenv(t, "DOCKHAND_CONFIG", filepath.Join(w.Home, "elsewhere.toml"))
	_, _, err = cmdtest.Dockhand(t, "outdated", "--mine")
	require.ErrorContains(t, err, `in `+filepath.Join(w.Home, "elsewhere.toml"), "the file read, not the default (the dogfood run with 251a1264)")
	cmdtest.Setenv(t, "DOCKHAND_CONFIG", "")
	config := filepath.Join(w.Home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n"), data...), 0o644))

	out, _, err := cmdtest.Dockhand(t, "outdated", "--mine")
	require.Equal(t, 3, term.ExitCode(err), "one couldn't be checked")
	require.Equal(t, []string{"@ada", "example.org:ada"}, reader.Asked[0].Maintainers)
	require.Contains(t, out, "  PORT   NOW     NEWEST   DOCKHAND CAN\n  jq     1.7.1   1.8.1    update\n")
	require.Contains(t, out, "1 of 2 ports couldn't be checked: no forge could be found for it\nOf the 1 port checked, 1 has a newer release, at master ")
	require.Contains(t, out, " · --current lists those it couldn't check\n")
	out, _, err = cmdtest.Dockhand(t, "outdated", "--mine", "--current")
	require.Equal(t, 3, term.ExitCode(err))
	require.Contains(t, out, "couldn't check: no forge could be found for it")

	// --mine updates each in a branch of its own, nothing committed, and
	// --plan starts none.
	out, _, err = cmdtest.Dockhand(t, "update", "--mine", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "  lost: left as it is: no forge could be found for it\n")
	require.Contains(t, out, "Will start 1 branch, one per port:\n  dockhand/jq-1.8.1  jq 1.7.1 → 1.8.1\n", "a row a port, with the name the run uses")
	require.Contains(t, out, "Nothing was started (--plan).\n")

	_, _, err = cmdtest.Dockhand(t, "update", "--mine")
	require.ErrorContains(t, err, "without a terminal, --yes starts what is shown")

	var stdout, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"update", "--mine"}, term.Streams{In: strings.NewReader("y\n"), Out: &stdout, Err: &errs}.AsTerminal())
	require.NoError(t, err, errs.String())
	require.Contains(t, errs.String(), "? Start them? [y/N] ")
	require.Contains(t, stdout.String(), "jq: 1.7.1 → 1.8.1")
	require.NotContains(t, stdout.String(), "Proposed commit", "nothing is committed")

	out, _, err = cmdtest.Dockhand(t, "outdated", "jq")
	require.Equal(t, 3, term.ExitCode(err), "the fixture's lost couldn't be checked")
	require.Regexp(t, `jq     1\.7\.1   1\.8\.1    already in jq-1\.8\.1\n`, out)
	out, _, err = cmdtest.Dockhand(t, "update", "--mine", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  jq: left as it is: already in jq-")
	require.Contains(t, out, "Nothing to update: no newer releases remain outside your existing branches.\n")

	_, _, err = cmdtest.Dockhand(t, "update", "--mine", "jq")
	require.EqualError(t, err, "--mine starts a branch for each of your ports; it takes no port, --branch, or --new")
	_, _, err = cmdtest.Dockhand(t, "update")
	require.ErrorContains(t, err, "name the port to update, or update your outdated ports with --mine")
}

// update --mine starts nothing for a port whose newest release is
// uncertain, and counts it for a look; outdated's JSON lists what was set
// aside (the update-workflow review's finding 6).
func TestUpdateMineLeavesAnUncertainPortForALook(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	reader := cmdtest.WithOutdated(t)
	reader.Uncertain = true
	cmdtest.AsMaintainer(t, w)

	out, _, err := cmdtest.Dockhand(t, "update", "--mine", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Will start 1 branch, one per port:\n  dockhand/jq-")
	require.Contains(t, out, "1 that may have one, for your look")

	result, err := cmdtest.JSONOf(t, "outdated", "jq", "lost", "yq")
	require.Equal(t, 3, term.ExitCode(err), "lost couldn't be checked")
	require.EqualValues(t, 1, cmdtest.Dig(t, result.Result, "unchecked"))
	yq := cmdtest.Dig(t, result.Result, "ports", 2)
	require.Equal(t, "yq", cmdtest.Dig(t, yq, "port"))
	require.Equal(t, false, cmdtest.Dig(t, yq, "outdated"))
	require.Equal(t, "5.0", cmdtest.Dig(t, yq, "newest"))
	require.Equal(t, []any{map[string]any{"tag": "v5.0", "version": "5.0", "source": "5.0", "predates": "v4.44.1"}}, cmdtest.Dig(t, yq, "uncertain"))
	require.Nil(t, cmdtest.Dig(t, result.Result, "ports", 0, "uncertain"), "absent for a port outdated for sure")
	require.Equal(t, "uncertain", cmdtest.Dig(t, yq, "class"))
	require.Equal(t, "newer", cmdtest.Dig(t, result.Result, "ports", 0, "class"))
	require.Equal(t, "unchecked", cmdtest.Dig(t, result.Result, "ports", 1, "class"))
}

// At a terminal, outdated shows how many ports are looked up on a line it
// redraws, and clears it once they all are; elsewhere it shows nothing.
func TestOutdatedShowsItsProgressAtATerminal(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	reader := cmdtest.WithOutdated(t)
	var out, errs bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), []string{"outdated", "jq", "lost"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs}.AsTerminal())
	require.Equal(t, 3, term.ExitCode(err), "lost couldn't be checked")
	// A fetch of master slower than two seconds says so first, as a loaded
	// machine's can be: the split's test binaries run many at once.
	said := regexp.MustCompile(`^Fetching master from \S+\n`).ReplaceAllString(errs.String(), "")
	require.Equal(t, "\r\033[KLooking up each port's newest release: 0 of 2\r\033[KLooking up each port's newest release: 1 of 2\r\033[K", said)
	require.NotContains(t, out.String(), "Looking up")

	_, errOut, err := cmdtest.Dockhand(t, "outdated", "jq", "lost")
	require.Equal(t, 3, term.ExitCode(err))
	require.NotContains(t, errOut, "Looking up", "no terminal, no count drawn")
	require.NotNil(t, reader.Asked[1].Progress, "the count is still followed, for a look cut short")
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
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.AsMaintainer(t, w)
	for _, args := range [][]string{{"outdated", "jq", "lost"}, {"update", "--mine"}, {"ship", "--mine"}} {
		ctx, cancel := context.WithCancel(t.Context())
		cmdtest.Stands(t).OutdatedReader = cutShort{cancel: cancel}
		var out, errs bytes.Buffer
		err := cmdtest.RunFor(t, ctx, args, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
		require.ErrorIs(t, err, context.Canceled, args)
		require.Contains(t, out.String(), "Interrupted after looking up 1 of 2 ports", args)
		require.Contains(t, out.String(), "  jq     1.7.1   1.8.1    update\n", args)
	}
	cmdtest.Stands(t).OutdatedReader = nil
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "update --mine and ship --mine started nothing")
}

// What a batch leaves is counted by what the look found of each, naming
// outdated, which says each.
func TestABatchCountsWhatItLeaves(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	require.Equal(t, 1, term.ExitCode(view.BatchExit([]string{"jq"}, 1, "shipped")), "none could be done")
	require.Equal(t, 3, term.ExitCode(view.BatchExit([]string{"jq"}, 2, "shipped")), "some need a look")
	require.NoError(t, view.BatchExit(nil, 2, "shipped"), "all were done")
}

// A look over hundreds of ports says once what it spends of GitHub's
// hourly allowance; a small one says nothing (the M1's run at 1da4fdbf:
// 835 ports cost 2,166 requests).
func TestALargeLookSaysItsCost(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// A look whose estimate passes what's left of the hour's GitHub allowance
// asks first on a terminal, and is refused without one unless
// --over-allowance says to go on; one within it, or where GitHub can't be
// asked, goes on as it did (field testing at 6dd7745c).
func TestALookPastTheAllowanceAsksFirst(t *testing.T) {
	t.Parallel()
	reset := time.Date(2026, 10, 21, 15, 4, 0, 0, time.Local)
	left := func(remaining int) func(context.Context) (github.Allowance, error) {
		return func(context.Context) (github.Allowance, error) {
			return github.Allowance{Remaining: remaining, Limit: 5000, Reset: reset}, nil
		}
	}
	script := term.Streams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	require.NoError(t, allowLook(left(5000), script, false)(t.Context(), 100), "within it")
	err := allowLook(left(1000), script, false)(t.Context(), 2133)
	require.EqualError(t, err, "Looking up 2,133 ports asks GitHub about 5,500 times, and the login has 1,000 of its 5,000 left this hour, until 15:04, which other sessions sharing it spend too; --over-allowance looks anyway")
	require.NoError(t, allowLook(left(1000), script, true)(t.Context(), 2133), "--over-allowance")
	require.NoError(t, allowLook(func(context.Context) (github.Allowance, error) { return github.Allowance{}, errors.New("offline") }, script, false)(t.Context(), 2133), "GitHub can't be asked")

	var asked bytes.Buffer
	yes := term.Streams{In: strings.NewReader("y\n"), Out: &bytes.Buffer{}, Err: &asked}.AsTerminal()
	require.NoError(t, allowLook(left(1000), yes, false)(t.Context(), 2133))
	require.Contains(t, asked.String(), "? Look anyway? [y/N]")
	no := term.Streams{In: strings.NewReader("\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}.AsTerminal()
	require.EqualError(t, allowLook(left(1000), no, false)(t.Context(), 2133), "nothing was looked up")
}
