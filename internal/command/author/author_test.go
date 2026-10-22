package author

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestUpdateInTheBranchCheckedOutHere(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)

	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n")
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)\nPlan, nothing changed:\n\n")
	require.Contains(t, out, "+version 1.8.1")
	require.NoFileExists(t, filepath.Join(dir, "textproc/jq/Portfile"))

	out, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	require.Equal(t, "jq-update · ~/Source/macports-branches/jq-update\n"+
		"jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)\n"+
		"Updated version.\n"+ // the fixture's jq fetches nothing
		"Changed: textproc/jq/Portfile\n"+
		"Upstream not compared: jq fetches no upstream source, so there's nothing to compare.\n"+
		"Next: dockhand check\n", out)
	data, err := os.ReadFile(filepath.Join(dir, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\n", string(data))

	out, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq is already at 1.8.1; nothing to change.")

	out, _, err = cmdtest.Dockhand(t, "checksums", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.8.1\nUpdated checksums.\n")
}

func TestUpdateWithoutABranchStartsOneOrUsesTheOne(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)

	// A plan changes nothing, so with no branch to plan in it plans on
	// master, as --new --plan does, and asks nothing on a terminal.
	none := testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*")
	planned, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\); --new without --plan starts the branch\n`, planned)
	require.Contains(t, planned, "+version 1.8.1")
	var asked, told bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"update", "jq", "--plan"}, term.Streams{In: strings.NewReader("y\n"), Out: &told, Err: &asked}.AsTerminal())
	require.NoError(t, err)
	require.NotContains(t, asked.String(), "? ", "a plan asks nothing")
	require.Contains(t, told.String(), "Planned on master ")
	require.Equal(t, none, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")
	// A checksum refresh's plan too, which started one: "this starts
	// dockhand/jq-checksums for it" (batch a, 2026-10-15).
	planned, _, err = cmdtest.Dockhand(t, "checksums", "jq", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\)`, planned)
	require.Equal(t, none, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")

	// With no open branch changing the port, the update starts one, in
	// a script too, and says so first (the command-line UX review, §2).
	var out, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"update", "jq"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	require.Regexp(t, `^jq is in no open branch, so this starts one for it, named for the version it moves to\.\n`, errs.String())
	started := regexp.MustCompile(`Started dockhand/(jq-1\.8\.1) from master `).FindStringSubmatch(out.String())
	require.NotNil(t, started, out.String())
	name := started[1]
	require.Contains(t, out.String(), name+" · ~/Source/macports-branches/"+name+"\n")
	testsupport.Git(t, filepath.Join(w.Home, "Source", "macports-branches", name), "commit", "-q", "-am", "jq: update to 1.8.1")

	// With exactly one, the work goes there, a plan's too, and says so.
	_, said, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, said, "Working in "+name+", the one open branch changing jq; --new starts another.\n")

	out.Reset()
	errs.Reset()
	err = cmdtest.RunFor(t, t.Context(), []string{"checksums", "jq"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, errs.String(), "Working in "+name+", the one open branch changing jq")
	require.NotContains(t, errs.String(), "? ", "nothing else could be meant, so nothing is asked")
	require.Contains(t, out.String(), name+" · ")
	require.Contains(t, out.String(), "Updated checksums.")

	out.Reset()
	err = cmdtest.RunFor(t, t.Context(), []string{"update", "jq", "1.9", "--new"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	another := regexp.MustCompile(`Started dockhand/(jq-1\.9) from master `).FindStringSubmatch(out.String())
	require.NotNil(t, another, out.String())
	require.NotEqual(t, name, another[1])
	require.Contains(t, out.String(), "jq: 1.7.1 → 1.9")
	require.Contains(t, out.String(), `Next: cd "$(dockhand path `+another[1]+`)", then dockhand check`+"\n",
		"a branch not checked out here is gone to first, since a check from elsewhere takes its committed head")

	out.Reset()
	err = cmdtest.RunFor(t, t.Context(), []string{"update", "jq", "2.0", "--branch", another[1]}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	require.Contains(t, out.String(), "jq: 1.9 → 2.0")

	// --new with --plan is a look before starting a branch, on master as
	// fetched now; it starts none.
	before := testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*")
	planned, _, err = cmdtest.Dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\); --new without --plan starts the branch\n`, planned)
	require.Contains(t, planned, "Plan, nothing changed:")
	require.NotContains(t, planned, "Started ")
	require.Equal(t, before, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")
	_, _, err = cmdtest.Dockhand(t, "checksums", "jq", "--new", "--plan")
	require.ErrorContains(t, err, "starts no branch")
	_, _, err = cmdtest.Dockhand(t, "update", "jq", "--new", "--branch", name)
	require.ErrorContains(t, err, "none of the others can be")
}

// A plan in a branch named with --branch is planned there, though the
// branch doesn't change the port yet.
func TestAPlanInANamedBranchIsPlannedThere(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "plain")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan", "--branch", "plain")
	require.NoError(t, err)
	require.Contains(t, out, "plain · ~/Source/macports-branches/plain\n")
	require.NotContains(t, out, "Planned on master")
}

// -b names an existing branch, everywhere, and a name no branch has is
// refused, saying --new=<name> starts one: edit jq -b jq-typo said
// "Started dockhand/jq-typo" with no question (the command-line UX second
// look's §1 and §3). --new=<name> starts a branch of the name; a plan
// with it is made on master. A Git branch dockhand doesn't track is
// adopt's.
func TestBNamesAnExistingBranchAndNewNamesANewOne(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan", "--branch", "jq-new")
	require.ErrorContains(t, err, "no tracked branch named jq-new; --new=jq-new starts one")
	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan", "--new=jq-new")
	require.NoError(t, err, "a plan starts nothing")
	require.Contains(t, out, "Planned on master")
	_, _, err = cmdtest.Dockhand(t, "revbump", "jq", "--subject", "rebuild for oniguruma 6.9.10", "--branch", "jq-rebuild")
	require.ErrorContains(t, err, "no tracked branch named jq-rebuild; --new=jq-rebuild starts one")
	_, _, err = cmdtest.Dockhand(t, "edit", "jq", "--branch", "jq-typo", "--no-open")
	require.ErrorContains(t, err, "no tracked branch named jq-typo; --new=jq-typo starts one")

	out, _, err = cmdtest.Dockhand(t, "edit", "jq", "--new=jq-rebuild", "--no-open")
	require.NoError(t, err)
	require.Regexp(t, `(?m)^Started dockhand/jq-rebuild from master [0-9a-f]+ \(fetched just now\)$`, out)
	out, _, err = cmdtest.Dockhand(t, "revbump", "jq", "--subject", "rebuild for oniguruma 6.9.10", "--branch", "jq-rebuild")
	require.NoError(t, err, "named again, it's the same branch")
	require.NotContains(t, out, "Started dockhand")

	testsupport.Git(t, w.Clone, "branch", "dockhand/theirs")
	_, _, err = cmdtest.Dockhand(t, "revbump", "jq", "--subject", "rebuild", "--branch", "theirs")
	require.ErrorContains(t, err, "dockhand adopt dockhand/theirs")
}

// A branch someone made with Git, which dockhand doesn't track, is theirs
// to adopt where it changes the port, in commits, edits, or files it adds,
// which work on master would leave out. Where it doesn't, it's no context,
// as master isn't: a plan is made on master, saying why, and an update
// says how to start a branch, as on master (D7, decided 2026-09-29).
func TestAnUntrackedBranchHereIsTheirsToAdopt(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	testsupport.Git(t, w.Clone, "switch", "-q", "-c", "mine")
	planned, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\), since mine doesn't change jq; --new without --plan starts the branch\n`, planned)
	_, said, err := cmdtest.Dockhand(t, "update", "jq", "1.8.1")
	require.NoError(t, err)
	require.Contains(t, said, "jq is in no open branch, so this starts one for it, named for the version it moves to.", "mine is no context, as master isn't")

	refused := func(why string) {
		t.Helper()
		_, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
		require.ErrorContains(t, err, "mine changes jq, which a plan on master would leave out; dockhand adopt tracks it, so the plan reads its changes, or --new --plan plans on master without them", why)
		_, _, err = cmdtest.Dockhand(t, "update", "jq")
		require.ErrorContains(t, err, "mine changes jq and isn't tracked; dockhand adopt tracks it, so the update is made there, or --new starts a branch from master without its changes", why)
	}
	patch := filepath.Join(w.Clone, "textproc/jq/files/patch-fix.diff")
	require.NoError(t, os.MkdirAll(filepath.Dir(patch), 0o755))
	require.NoError(t, os.WriteFile(patch, []byte("--- a\n+++ b\n"), 0o644))
	refused("a file it adds")
	require.NoError(t, os.RemoveAll(filepath.Dir(patch)))
	portfile := filepath.Join(w.Clone, "textproc/jq/Portfile")
	require.NoError(t, os.WriteFile(portfile, []byte("name jq\n# mine\n"), 0o644))
	refused("an edit not committed")
	testsupport.Git(t, w.Clone, "commit", "-q", "-am", "jq: mine")
	refused("a commit since it left master")
	planned, _, err = cmdtest.Dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\); --new without --plan starts the branch\n`, planned)
}

// Master's own edits to a port, not committed, would be left out of a plan
// on master too, so it refuses them, and --new --plan plans without them
// (D7).
func TestAPlanOnMasterDoesntLeaveEditsOut(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Clone, "textproc/jq/Portfile"), []byte("name jq\n# mine\n"), 0o644))
	_, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.ErrorContains(t, err, "what's checked out here changes jq, which a plan on master would leave out; --new --plan plans on master without it")
	planned, _, err := cmdtest.Dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "Planned on master ")
}

func TestUpdateRevbumpsTheLibraryDependents(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	for _, port := range []string{"yq", "jo"} {
		require.NoError(t, os.MkdirAll(filepath.Join(w.Upstream, "textproc", port), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc", port, "Portfile"), []byte("name "+port+"\nversion 1\n"), 0o644))
	}
	testsupport.Git(t, w.Upstream, "add", "-A")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "yq and jo")
	cmdtest.WithBumper(t)
	cmdtest.UseDependents(cmdtest.Stands(t), cmdtest.JqDependents{})
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)

	_, _, err = cmdtest.Dockhand(t, "update", "jq", "--except", "yq")
	require.ErrorContains(t, err, "add --revbump-dependents")
	_, _, err = cmdtest.Dockhand(t, "update", "jq", "--revbump-dependents", "--except", "jo", "--plan")
	require.ErrorContains(t, err, "--except jo: it is not a library dependent of jq")

	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--revbump-dependents", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Direct library dependents, from the index at ")
	require.Contains(t, out, ":\n  yq\n", "jo only builds with jq")
	require.NoFileExists(t, filepath.Join(dir, "textproc/yq/Portfile"), "a plan bumps nothing")

	// One that links it only under a variant is listed apart.
	cmdtest.UseDependents(cmdtest.Stands(t), cmdtest.JqDependents{Under: true})
	out, _, err = cmdtest.Dockhand(t, "update", "jq", "--revbump-dependents", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, ":\n  yq\n    under a variant, found in the Portfile, which the index doesn't record: jo (+jq)\n")
	cmdtest.UseDependents(cmdtest.Stands(t), cmdtest.JqDependents{})

	out, _, err = cmdtest.Dockhand(t, "update", "jq", "--revbump-dependents")
	require.NoError(t, err)
	require.Contains(t, out, "Revision bumped 1 port; subject \"<port>: rebuild for jq 1.8.1\" recorded for tidy.\n")
	yq, err := os.ReadFile(filepath.Join(dir, "textproc/yq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name yq\nversion 1\nrevision 1\n", string(yq))
	require.NoFileExists(t, filepath.Join(dir, "textproc/jo/Portfile"))

	out, _, err = cmdtest.Dockhand(t, "update", "jq", "2.0", "--revbump-dependents")
	require.NoError(t, err)
	require.Contains(t, out, "    yq: the branch already changes it, so it is left as it is\n")
}

// Where an update found its version outlives the update's process: each
// command reads it back from the store, status before tidy and after, and
// its JSON too. From the architecture review of 2026-09-27, which found the
// chosen release's tag and upstream commit gone once update returned.
func TestAnUpdatesReleaseIsKept(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)

	const line = "  Release  jq 1.8.1, GitHub tag jq-1.8.1 of jqlang/jq at 1a2b3c4\n"
	out, _, err := cmdtest.Dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, line)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, line, "committing the edit keeps where it came from")

	status, err := cmdtest.JSONOf(t, "status")
	require.NoError(t, err)
	release := cmdtest.Dig(t, status.Result, "branches", 0, "releases", 0)
	require.Equal(t, "jq", cmdtest.Dig(t, release, "port"))
	require.Equal(t, "jq-1.8.1", cmdtest.Dig(t, release, "tag"))
	require.Equal(t, "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", cmdtest.Dig(t, release, "commit"))
	require.Equal(t, "jqlang/jq", cmdtest.Dig(t, release, "repository"))
}

// chatty is a bumper that reports as it works, as MacPorts' editor does.
type chatty struct{ cmdtest.Bumper }

func (c chatty) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	progress.Report(ctx, "Building the PortIndex; this may take several minutes")
	progress.VerboseReport(ctx, "Generating full PortIndex for source abc123")
	progress.VerboseReport(progress.Within(ctx, "macOS 15 (Tart)"), "Using the cached PortIndex for base abc123")
	return c.Bumper.Prepare(ctx, r)
}

// What the work reports goes to standard error as it goes (Design v3 §12):
// what a person needs to follow it, and with -v, the work behind the
// scenes too.
func TestProgressGoesToStandardError(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return chatty{cmdtest.Bumper{Repo: e.Repo}} }

	out, errs, err := cmdtest.Dockhand(t, "update", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, errs, "Building the PortIndex; this may take several minutes\n")
	require.NotContains(t, errs, "Generating full PortIndex", "the work behind the scenes waits for -v")
	require.NotContains(t, out, "PortIndex", "progress never mixes with the result")

	_, errs, err = cmdtest.Dockhand(t, "update", "jq", "--new", "-v")
	require.NoError(t, err)
	require.Contains(t, errs, "Building the PortIndex; this may take several minutes\nGenerating full PortIndex for source abc123\n")
	// A report about a part of the work is indented under the command's
	// own, as a check's run's lines are (the hugo exercise's check-65).
	require.Contains(t, errs, "\n  macOS 15 (Tart): Using the cached PortIndex for base abc123\n")
}

// An update names the port's other open pull requests, planned or made,
// and stops for none: libuv 1.53.0 was planned without a word of #34620
// (the libuv run's finding 1). What couldn't be looked for is said.
func TestAnUpdateNamesOtherOpenPullRequests(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := &forgetest.GitHub{Others: []forge.PullRequestSummary{{Number: 34620, Title: "jq: update to 1.8.0", URL: "https://github.com/macports/macports-ports/pull/34620"}}}
	cmdtest.Stands(t).Forge = func(*engine.Engine) engine.Forge { return g }

	planned, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "Also open for jq: #34620 jq: update to 1.8.0\n")
	result, err := cmdtest.JSONOf(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"number": float64(34620), "title": "jq: update to 1.8.0", "url": "https://github.com/macports/macports-ports/pull/34620"}}, cmdtest.Dig(t, result.Result, "others"))

	made, _, err := cmdtest.Dockhand(t, "update", "jq", "--new")
	require.NoError(t, err, "it stops for none")
	require.Contains(t, made, "Also open for jq: #34620 jq: update to 1.8.0\n")

	g.SearchErr = errors.New("rate limited")
	dir := filepath.Join(w.Home, "Source", "macports-branches")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(dir, entries[0].Name()))
	require.NoError(t, os.WriteFile(filepath.Join(dir, entries[0].Name(), "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"), 0o644))
	again, _, err := cmdtest.Dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, again, "Couldn't look for other open pull requests for jq: rate limited\n")
}

// uncertainBumper is a bumper whose discovery set jq-1.9.0 aside, tagged
// on a commit older than jq-1.7.1's, and found nothing newer.
type uncertainBumper struct{ cmdtest.Bumper }

func (b uncertainBumper) ResolveRelease(ctx context.Context, r editprep.Request) (model.Release, error) {
	if r.Version == "" {
		return model.Release{}, &engine.UncertainRelease{Port: "jq", SetAside: []engine.SetAside{{Tag: "jq-1.9.0", Version: "1.9.0", Source: "1.9.0", Predates: "jq-1.7.1"}}}
	}
	return b.Bumper.ResolveRelease(ctx, r)
}

// An update to the newest release where discovery can't say which that is
// chooses none: it changes nothing, needs a look, and names the update that
// takes what was set aside, which then goes ahead (the update-workflow
// review's finding 6).
func TestAnUncertainNewestReleaseIsNamedNotChosen(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return uncertainBumper{cmdtest.Bumper{Repo: e.Repo}} }
	cmdtest.WithScript(t, w, "passed")
	cmdtest.WithGitHub(t, w)

	_, _, err := cmdtest.Dockhand(t, "update", "jq", "--new")
	require.Equal(t, 3, term.ExitCode(err), "it needs a look")
	require.EqualError(t, err, "can't tell whether jq is current, so nothing was changed: jq-1.9.0 compares newer, but its commit is older than jq-1.7.1's\nNext: if jq-1.9.0 is a release: dockhand update jq 1.9.0\nNext: if it isn't, as an old tag spelled oddly: a livecheck.regex in the Portfile that leaves jq-1.9.0 out keeps it out of later looks")
	_, _, err = cmdtest.BumpOn(t, "jq")
	require.Equal(t, 3, term.ExitCode(err))
	require.ErrorContains(t, err, "\nNext: if jq-1.9.0 is a release: dockhand bump jq 1.9.0")
	result, err := cmdtest.JSONOf(t, "update", "jq", "--plan")
	require.Equal(t, 3, term.ExitCode(err))
	require.Nil(t, result.Result, "a refusal before anything is done reports only its error")
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")

	out, _, err := cmdtest.Dockhand(t, "update", "jq", "1.9.0", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "jq: 1.7.1 → 1.9.0")
}

// regenerating is a preparer whose update wrote a dependency block again.
type regenerating struct{ cmdtest.Bumper }

func (b regenerating) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	result, err := b.Bumper.Prepare(ctx, r)
	result.Regenerated = []editprep.Regenerated{{Option: "cargo.crates", Count: 352, Changed: 160, Dropped: []editprep.Override{{Name: "soundtouch", Pinned: "0.4.1", Was: "0.4.0", Locked: "0.5.4"}}}, {Option: "cargo.crates_github", Count: 0, Changed: 0}}
	return result, err
}

// An update says what it wrote of the dependency blocks, not only the
// distfiles: hk's said "1 distfile" of 280 lines of crates (the gh, usql,
// hk, and pgdog run's finding 4).
func TestAnUpdateCountsTheCratesItWrote(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return regenerating{cmdtest.Bumper{Repo: e.Repo}} }
	result, err := cmdtest.JSONOf(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"option": "cargo.crates", "count": float64(352), "changed": float64(160),
		"dropped_pins": []any{map[string]any{"name": "soundtouch", "pinned": "0.4.1", "lock_had": "0.4.0", "lock_has": "0.5.4"}}}, cmdtest.Dig(t, result.Result, "regenerated", 0))
	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, " and 352 crates (160 changed).\n", "an empty block says nothing: no \"and 0 Git crates (0 changed)\" (the txt run's finding 6)")
	require.Contains(t, out, "Removed the Portfile pin for soundtouch 0.4.1 (lockfile version: 0.4.0); the new lockfile now specifies 0.5.4.\n")
}

// limitedBumper is a bumper GitHub's rate limit stops before it finds the
// release, as a request for a tag's commit is.
type limitedBumper struct{ cmdtest.Bumper }

func (limitedBumper) ResolveRelease(context.Context, editprep.Request) (model.Release, error) {
	limited := &forge.RateLimitError{RetryAt: time.Now().Add(18 * time.Minute), Err: errors.New("GitHub's rate limit for your login resets in 18 minutes, at 23:24 EDT")}
	return model.Release{}, fmt.Errorf("Get \"https://api.github.com/repos/jqlang/jq/git/commits/abc\": %w", limited)
}

// An update GitHub's rate limit stops says the limit, when it lifts, and
// that nothing changed, not the request it stopped (the rc6 full stage,
// D-R4).
func TestARateLimitedUpdateSaysSo(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return limitedBumper{cmdtest.Bumper{Repo: e.Repo}} }
	_, _, err := cmdtest.Dockhand(t, "update", "jq", "--new", "--plan")
	require.EqualError(t, err, "GitHub's rate limit for your login resets in 18 minutes, at 23:24 EDT; nothing was changed")
}

// subportBumper is bumper where the edit evaluates a subport, as a python
// port's evaluates py314-<name>.
type subportBumper struct{ cmdtest.Bumper }

func (b subportBumper) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	result, err := b.Bumper.Prepare(ctx, r)
	result.Target.Name = "py314-" + result.Target.Name
	for i, report := range result.Fidelity {
		for _, snapshot := range []*macports.Snapshot{&report.Before, &report.After} {
			ports := map[string]macports.PortInfo{}
			for name, info := range snapshot.Ports {
				ports["py314-"+name] = info
			}
			snapshot.Ports = ports
		}
		result.Fidelity[i] = report
	}
	return result, err
}

// update's header names the port as the person named it, not the subport
// the edit evaluated: py-textual-fastdatatable's read
// "py314-textual-fastdatatable: 0.19.0 → 0.19.3" (field testing at
// 23c5e2aa).
func TestAnUpdateNamesThePortAsAsked(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return subportBumper{cmdtest.Bumper{Repo: e.Repo}} }
	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.NotContains(t, out, "py314-jq: ")
}

// checksums with no port takes the branch's one port, as every other
// branch command does (field testing at 23c5e2aa: "accepts 1 arg(s),
// received 0"), and asks for one where the branch is about several.
func TestChecksumsTakesTheBranchsPort(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-sums", "--port", "jq")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-sums"))
	out, _, err := cmdtest.Dockhand(t, "checksums", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq")
	require.Contains(t, out, "+checksums sha256 0000")

	_, _, err = cmdtest.Dockhand(t, "checksums", "--new", "--plan")
	require.Error(t, err)
}
