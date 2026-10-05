package command

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

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// bumper stands in for MacPorts: a bump rewrites the version line, and a
// checksum refresh adds a checksums line.
type bumper struct{ repo *git.Repository }

func (b bumper) ResolveRelease(ctx context.Context, r editprep.Request) (model.Release, error) {
	version := r.Version
	if version == "" {
		version = "1.8.1"
	}
	// As discovery does, it says a release the port is at already is no
	// update.
	_, data, err := b.repo.File(ctx, string(r.Source.Tree), "textproc/"+r.Selection.Selector+"/Portfile")
	if err != nil {
		return model.Release{}, err
	}
	current := regexp.MustCompile(`(?m)^version (\S+)$`).FindSubmatch(data)
	noUpdate := current != nil && string(current[1]) == version
	return model.Release{ReleaseSelection: model.ReleaseSelection{NoUpdate: noUpdate}, Version: version, Forge: "github", Repository: "jqlang/jq", Tag: "jq-" + version, Commit: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"}, nil
}

func (b bumper) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	name := "textproc/" + r.Selection.Selector + "/Portfile"
	before, data, err := b.repo.File(ctx, string(r.Source.Tree), name)
	if err != nil {
		return editprep.Result{}, err
	}
	line := regexp.MustCompile(`(?m)^version (\S+)$`)
	old := "1.7.1"
	if m := line.FindSubmatch(data); m != nil {
		old = string(m[1])
	}
	next, after := old, string(data)
	revision := 0
	removed := false
	switch {
	case r.Action == model.EditUpdate:
		next = r.Release.Version
		after = line.ReplaceAllString(after, "version "+next)
		// As the editor does where every archive of the new version has a
		// name of its own: a stealth update's dist_subdir goes.
		if without, ok, err := portfile.RemoveStealthDistSubdir([]byte(after)); err == nil && ok {
			after, removed = string(without), true
		}
	case r.Action == model.EditRevbump:
		revision = 1
		after += "revision 1\n"
	case !strings.Contains(after, "checksums"):
		after += "checksums sha256 0000\n"
	}
	port := func(v string, revision int) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Version: v, Revision: revision}}}
	}
	result := editprep.Result{Target: model.Target{Name: "jq"}, Release: r.Release, PreparedTree: r.Source.Tree, Fidelity: []portedit.Fidelity{{Before: port(old, 0), After: port(next, revision)}}}
	result.DistSubdirRemoved = removed
	if after == string(data) {
		return result, nil
	}
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := b.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	result.Files, result.PreparedTree = []git.FileEdit{edit}, model.ObjectID(tree)
	if r.Action == model.EditRevbump {
		result.Commits = []editprep.CommitIntent{{Subject: "jq: " + r.Subject}}
	}
	return result, err
}

func withBumper(t *testing.T) {
	testPreparer = func(e *engine.Engine) engine.Preparer { return bumper{repo: e.Repo} }
	t.Cleanup(func() { testPreparer = nil })
}

func versioned(t *testing.T, w world) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
}

func TestUpdateInTheBranchCheckedOutHere(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)

	out, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n")
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)\nPlan, nothing changed:\n\n")
	require.Contains(t, out, "+version 1.8.1")
	require.NoFileExists(t, filepath.Join(dir, "textproc/jq/Portfile"))

	out, _, err = dockhand(t, "update", "jq")
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

	out, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq is already at 1.8.1; nothing to change.")

	out, _, err = dockhand(t, "checksums", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.8.1\nUpdated checksums.\n")
}

func TestUpdateWithoutABranchStartsOneOrUsesTheOne(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)

	// A plan changes nothing, so with no branch to plan in it plans on
	// master, as --new --plan does, and asks nothing on a terminal.
	none := testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*")
	planned, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\); --new without --plan starts the branch\n`, planned)
	require.Contains(t, planned, "+version 1.8.1")
	var asked, told bytes.Buffer
	err = Run(t.Context(), []string{"update", "jq", "--plan"}, Streams{In: strings.NewReader("y\n"), Out: &told, Err: &asked, interactive: true})
	require.NoError(t, err)
	require.NotContains(t, asked.String(), "? ", "a plan asks nothing")
	require.Contains(t, told.String(), "Planned on master ")
	require.Equal(t, none, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")

	// With no open branch changing the port, the update starts one, in
	// a script too, and says so first (the command-line UX review, §2).
	var out, errs bytes.Buffer
	err = Run(t.Context(), []string{"update", "jq"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	require.Regexp(t, `^jq is in no open branch, so this starts one for it, named for the version it moves to\.\n`, errs.String())
	started := regexp.MustCompile(`Started dockhand/(jq-1\.8\.1) from master `).FindStringSubmatch(out.String())
	require.NotNil(t, started, out.String())
	name := started[1]
	require.Contains(t, out.String(), name+" · ~/Source/macports-branches/"+name+"\n")
	testsupport.Git(t, filepath.Join(w.home, "Source", "macports-branches", name), "commit", "-q", "-am", "jq: update to 1.8.1")

	// With exactly one, the work goes there, a plan's too, and says so.
	_, said, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, said, "Working in "+name+", the one open branch changing jq; --new starts another.\n")

	out.Reset()
	errs.Reset()
	err = Run(t.Context(), []string{"checksums", "jq"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "Working in "+name+", the one open branch changing jq")
	require.NotContains(t, errs.String(), "? ", "nothing else could be meant, so nothing is asked")
	require.Contains(t, out.String(), name+" · ")
	require.Contains(t, out.String(), "Updated checksums.")

	out.Reset()
	err = Run(t.Context(), []string{"update", "jq", "1.9", "--new"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	another := regexp.MustCompile(`Started dockhand/(jq-1\.9) from master `).FindStringSubmatch(out.String())
	require.NotNil(t, another, out.String())
	require.NotEqual(t, name, another[1])
	require.Contains(t, out.String(), "jq: 1.7.1 → 1.9")
	require.Contains(t, out.String(), `Next: cd "$(dockhand path `+another[1]+`)", then dockhand check`+"\n",
		"a branch not checked out here is gone to first, since a check from elsewhere takes its committed head")

	out.Reset()
	err = Run(t.Context(), []string{"update", "jq", "2.0", "--branch", another[1]}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	require.Contains(t, out.String(), "jq: 1.9 → 2.0")

	// --new with --plan is a look before starting a branch, on master as
	// fetched now; it starts none.
	before := testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*")
	planned, _, err = dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\); --new without --plan starts the branch\n`, planned)
	require.Contains(t, planned, "Plan, nothing changed:")
	require.NotContains(t, planned, "Started ")
	require.Equal(t, before, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")
	_, _, err = dockhand(t, "checksums", "jq", "--new", "--plan")
	require.ErrorContains(t, err, "starts no branch")
	_, _, err = dockhand(t, "update", "jq", "--new", "--branch", name)
	require.ErrorContains(t, err, "none of the others can be")
}

// A plan in a branch named with --branch is planned there, though the
// branch doesn't change the port yet.
func TestAPlanInANamedBranchIsPlannedThere(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "plain")
	require.NoError(t, err)
	out, _, err := dockhand(t, "update", "jq", "--plan", "--branch", "plain")
	require.NoError(t, err)
	require.Contains(t, out, "plain · ~/Source/macports-branches/plain\n")
	require.NotContains(t, out, "Planned on master")
}

// An authoring command's --branch naming no branch starts it from master,
// as usage.md's revbump example does (the person, 2026-10-05); a plan
// starts nothing, planning an update on master; and a Git branch of the
// name that dockhand doesn't track is refused, pointing at adopt.
func TestABranchNamedForTheFirstTimeIsStarted(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	out, _, err := dockhand(t, "update", "jq", "--plan", "--branch", "jq-new")
	require.NoError(t, err, "a plan starts nothing")
	require.Contains(t, out, "Planned on master")
	_, _, err = dockhand(t, "revbump", "jq", "--subject", "rebuild for oniguruma 6.9.10", "--branch", "jq-rebuild", "--plan")
	require.ErrorContains(t, err, "--plan changes nothing, so it starts no branch, and no branch is named jq-rebuild yet; without --plan, this starts it from master")

	out, _, err = dockhand(t, "revbump", "jq", "--subject", "rebuild for oniguruma 6.9.10", "--branch", "jq-rebuild")
	require.NoError(t, err)
	require.Regexp(t, `^Started dockhand/jq-rebuild from master [0-9a-f]+ \(fetched just now\)\n`, out)
	out, _, err = dockhand(t, "edit", "jq", "--branch", "jq-rebuild", "--no-open")
	require.NoError(t, err, "named again, it's the same branch")
	require.NotContains(t, out, "Started dockhand")

	testsupport.Git(t, w.clone, "branch", "dockhand/theirs")
	_, _, err = dockhand(t, "revbump", "jq", "--subject", "rebuild", "--branch", "theirs")
	require.ErrorContains(t, err, "dockhand adopt dockhand/theirs")
}

// A branch someone made with Git, which dockhand doesn't track, is theirs
// to adopt where it changes the port, in commits, edits, or files it adds,
// which work on master would leave out. Where it doesn't, it's no context,
// as master isn't: a plan is made on master, saying why, and an update
// says how to start a branch, as on master (D7, decided 2026-09-29).
func TestAnUntrackedBranchHereIsTheirsToAdopt(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	testsupport.Git(t, w.clone, "switch", "-q", "-c", "mine")
	planned, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\), since mine doesn't change jq; --new without --plan starts the branch\n`, planned)
	_, said, err := dockhand(t, "update", "jq", "1.8.1")
	require.NoError(t, err)
	require.Contains(t, said, "jq is in no open branch, so this starts one for it, named for the version it moves to.", "mine is no context, as master isn't")

	refused := func(why string) {
		t.Helper()
		_, _, err := dockhand(t, "update", "jq", "--plan")
		require.ErrorContains(t, err, "mine changes jq, which a plan on master would leave out; dockhand adopt tracks it, so the plan reads its changes, or --new --plan plans on master without them", why)
		_, _, err = dockhand(t, "update", "jq")
		require.ErrorContains(t, err, "mine changes jq and isn't tracked; dockhand adopt tracks it, so the update is made there, or --new starts a branch from master without its changes", why)
	}
	patch := filepath.Join(w.clone, "textproc/jq/files/patch-fix.diff")
	require.NoError(t, os.MkdirAll(filepath.Dir(patch), 0o755))
	require.NoError(t, os.WriteFile(patch, []byte("--- a\n+++ b\n"), 0o644))
	refused("a file it adds")
	require.NoError(t, os.RemoveAll(filepath.Dir(patch)))
	portfile := filepath.Join(w.clone, "textproc/jq/Portfile")
	require.NoError(t, os.WriteFile(portfile, []byte("name jq\n# mine\n"), 0o644))
	refused("an edit not committed")
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "jq: mine")
	refused("a commit since it left master")
	planned, _, err = dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `Planned on master [0-9a-f]+ \(fetched just now\); --new without --plan starts the branch\n`, planned)
}

// Master's own edits to a port, not committed, would be left out of a plan
// on master too, so it refuses them, and --new --plan plans without them
// (D7).
func TestAPlanOnMasterDoesntLeaveEditsOut(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# mine\n"), 0o644))
	_, _, err := dockhand(t, "update", "jq", "--plan")
	require.ErrorContains(t, err, "what's checked out here changes jq, which a plan on master would leave out; --new --plan plans on master without it")
	planned, _, err := dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "Planned on master ")
}

func TestUpdateRevbumpsTheLibraryDependents(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	for _, port := range []string{"yq", "jo"} {
		require.NoError(t, os.MkdirAll(filepath.Join(w.upstream, "textproc", port), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc", port, "Portfile"), []byte("name "+port+"\nversion 1\n"), 0o644))
	}
	testsupport.Git(t, w.upstream, "add", "-A")
	testsupport.Git(t, w.upstream, "commit", "-q", "-m", "yq and jo")
	withBumper(t)
	testDependentReader = jqDependents{}
	t.Cleanup(func() { testDependentReader = nil })
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)

	_, _, err = dockhand(t, "update", "jq", "--except", "yq")
	require.ErrorContains(t, err, "add --revbump-dependents")
	_, _, err = dockhand(t, "update", "jq", "--revbump-dependents", "--except", "jo", "--plan")
	require.ErrorContains(t, err, "--except jo: it is not a library dependent of jq")

	out, _, err := dockhand(t, "update", "jq", "--revbump-dependents", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "Direct library dependents, from the index at ")
	require.Contains(t, out, ":\n  yq\n", "jo only builds with jq")
	require.NoFileExists(t, filepath.Join(dir, "textproc/yq/Portfile"), "a plan bumps nothing")

	// One that links it only under a variant is listed apart.
	testDependentReader = jqDependents{under: true}
	out, _, err = dockhand(t, "update", "jq", "--revbump-dependents", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, ":\n  yq\n  · under a variant, found in the Portfile, which the index doesn't record: jo (+jq)\n")
	testDependentReader = jqDependents{}

	out, _, err = dockhand(t, "update", "jq", "--revbump-dependents")
	require.NoError(t, err)
	require.Contains(t, out, "Revision bumped 1 port; subject \"<port>: rebuild for jq 1.8.1\" recorded for tidy.\n")
	yq, err := os.ReadFile(filepath.Join(dir, "textproc/yq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name yq\nversion 1\nrevision 1\n", string(yq))
	require.NoFileExists(t, filepath.Join(dir, "textproc/jo/Portfile"))

	out, _, err = dockhand(t, "update", "jq", "2.0", "--revbump-dependents")
	require.NoError(t, err)
	require.Contains(t, out, "  · yq: the branch already changes it, so it is left as it is\n")
}

// Where an update found its version outlives the update's process: each
// command reads it back from the store, status before tidy and after, and
// its JSON too. From the architecture review of 2026-09-27, which found the
// chosen release's tag and upstream commit gone once update returned.
func TestAnUpdatesReleaseIsKept(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	const line = "  Release  jq 1.8.1, GitHub tag jq-1.8.1 of jqlang/jq at 1a2b3c4\n"
	out, _, err := dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, line)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	out, _, err = dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, line, "committing the edit keeps where it came from")

	status, err := jsonOf(t, "status")
	require.NoError(t, err)
	release := dig(t, status.Result, "branches", 0, "releases", 0)
	require.Equal(t, "jq", dig(t, release, "port"))
	require.Equal(t, "jq-1.8.1", dig(t, release, "tag"))
	require.Equal(t, "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", dig(t, release, "commit"))
	require.Equal(t, "jqlang/jq", dig(t, release, "repository"))
}

// chatty is a bumper that reports as it works, as MacPorts' editor does.
type chatty struct{ bumper }

func (c chatty) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	progress.Report(ctx, "Building the PortIndex; this may take several minutes")
	progress.VerboseReport(ctx, "Generating full PortIndex for source abc123")
	progress.VerboseReport(progress.Within(ctx, "macOS 15 (Tart)"), "Using the cached PortIndex for base abc123")
	return c.bumper.Prepare(ctx, r)
}

// What the work reports goes to standard error as it goes (Design v3 §12):
// what a person needs to follow it, and with -v, the work behind the
// scenes too.
func TestProgressGoesToStandardError(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return chatty{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })

	out, errs, err := dockhand(t, "update", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, errs, "Building the PortIndex; this may take several minutes\n")
	require.NotContains(t, errs, "Generating full PortIndex", "the work behind the scenes waits for -v")
	require.NotContains(t, out, "PortIndex", "progress never mixes with the result")

	_, errs, err = dockhand(t, "update", "jq", "--new", "-v")
	require.NoError(t, err)
	require.Contains(t, errs, "Building the PortIndex; this may take several minutes\nGenerating full PortIndex for source abc123\n")
	// A report about a part of the work is indented under the command's
	// own, as a check's run's lines are (the hugo exercise's check-65).
	require.Contains(t, errs, "\n  macOS 15 (Tart): Using the cached PortIndex for base abc123\n")
}

// What dockhand can't write, it says in its own words, with advice only
// where the advice works: git's refusal said "baseline {…}: portfile:
// unsupported source edit: calculated checksum algorithm", advised
// checksums, which refused the same way, and said a plan kept a branch it
// never started (the git run's findings 1 to 4).
func TestAnEditDockhandCantMakeSaysWhyAndWhatWorks(t *testing.T) {
	unlocated := fmt.Errorf("baseline {OS:darwin Version:25 Architecture:arm64}: %w", &engine.Unlocated{Name: "git-htmldocs-2.56.0.tar.xz", Reason: fmt.Errorf("%w: no command written in the Portfile makes it, as an eval'd one isn't", engine.ErrUnsupported)})
	branch := model.Branch{Name: "dockhand/git-ab12"}

	err := byHand(unlocated, engine.UpdateRequest{Action: model.EditUpdate, Port: "git"}, branch, false)
	require.EqualError(t, err, `can't update git by itself: the checksums for git-htmldocs-2.56.0.tar.xz can't be found in the Portfile to edit: no command written in the Portfile makes it, as an eval'd one isn't
Kept: the branch, unchanged.
Edit the version yourself; dockhand checksums git then prints the checksums to write:
  dockhand edit git`)

	err = byHand(&engine.ChecksumsToWrite{Err: unlocated, Checksums: []portfile.Checksum{{Name: "git-2.56.0.tar.xz", RMD160: "aaaa", SHA256: "bbbb", Size: 8}}}, engine.UpdateRequest{Action: model.EditChecksums, Port: "git"}, branch, false)
	require.EqualError(t, err, `can't refresh git's checksums by itself: the checksums for git-htmldocs-2.56.0.tar.xz can't be found in the Portfile to edit: no command written in the Portfile makes it, as an eval'd one isn't
Kept: the branch, unchanged.
Write them yourself; its archives have these now:
    checksums           git-2.56.0.tar.xz \
                        rmd160  aaaa \
                        sha256  bbbb \
                        size    8
  dockhand edit git`)

	err = byHand(fmt.Errorf("%w: a version it can't find", engine.ErrUnsupported), engine.UpdateRequest{Action: model.EditUpdate, Port: "git", Plan: true, FromMaster: true}, model.Branch{}, false)
	require.EqualError(t, err, "can't update git by itself: a version it can't find\nEdit the version and its checksums yourself; port checksum git, after the version's edit, says what its archives have:\n  dockhand edit git", "a plan keeps nothing, having changed nothing")

	// An edit whose evaluation isn't the change intended is refused as one
	// dockhand can't make, with the way on, where rust's came bare (the
	// rust and cargo run).
	err = byHand(fmt.Errorf("%w: rust-src, another port of the same Portfile, fetches its own source (its distfiles differ from rust's), which updating rust doesn't move", engine.ErrFidelity), engine.UpdateRequest{Action: model.EditUpdate, Port: "rust", Plan: true}, model.Branch{}, false)
	require.EqualError(t, err, "can't update rust by itself: rust-src, another port of the same Portfile, fetches its own source (its distfiles differ from rust's), which updating rust doesn't move\nEdit the version and its checksums yourself; port checksum rust, after the version's edit, says what its archives have:\n  dockhand edit rust")
}

// An update names the port's other open pull requests, planned or made,
// and stops for none: libuv 1.53.0 was planned without a word of #34620
// (the libuv run's finding 1). What couldn't be looked for is said.
func TestAnUpdateNamesOtherOpenPullRequests(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := &forgetest.GitHub{Others: []forge.PullRequestSummary{{Number: 34620, Title: "jq: update to 1.8.0", URL: "https://github.com/macports/macports-ports/pull/34620"}}}
	testForge = func(*engine.Engine) engine.Forge { return g }

	planned, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "Also open for jq: #34620 jq: update to 1.8.0\n")
	result, err := jsonOf(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"number": float64(34620), "title": "jq: update to 1.8.0", "url": "https://github.com/macports/macports-ports/pull/34620"}}, dig(t, result.Result, "others"))

	made, _, err := dockhand(t, "update", "jq", "--new")
	require.NoError(t, err, "it stops for none")
	require.Contains(t, made, "Also open for jq: #34620 jq: update to 1.8.0\n")

	g.SearchErr = errors.New("rate limited")
	dir := filepath.Join(w.home, "Source", "macports-branches")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(dir, entries[0].Name()))
	require.NoError(t, os.WriteFile(filepath.Join(dir, entries[0].Name(), "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"), 0o644))
	again, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, again, "Couldn't look for other open pull requests for jq: rate limited\n")
}

// uncertainBumper is a bumper whose discovery set jq-1.9.0 aside, tagged
// on a commit older than jq-1.7.1's, and found nothing newer.
type uncertainBumper struct{ bumper }

func (b uncertainBumper) ResolveRelease(ctx context.Context, r editprep.Request) (model.Release, error) {
	if r.Version == "" {
		return model.Release{}, &engine.UncertainRelease{Port: "jq", SetAside: []engine.SetAside{{Tag: "jq-1.9.0", Version: "1.9.0", Source: "1.9.0", Predates: "jq-1.7.1"}}}
	}
	return b.bumper.ResolveRelease(ctx, r)
}

// An update to the newest release where discovery can't say which that is
// chooses none: it changes nothing, needs a look, and names the update that
// takes what was set aside, which then goes ahead (the update-workflow
// review's finding 6).
func TestAnUncertainNewestReleaseIsNamedNotChosen(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return uncertainBumper{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })
	withScript(t, w, "passed")
	withGitHub(t, w)

	_, _, err := dockhand(t, "update", "jq", "--new")
	require.Equal(t, 3, ExitCode(err), "it needs a look")
	require.EqualError(t, err, "can't tell whether jq is current, so nothing was changed: jq-1.9.0 compares newer, but its commit is older than jq-1.7.1's\nIf jq-1.9.0 is a release: dockhand update jq 1.9.0")
	_, _, err = bumpOn(t, "jq")
	require.Equal(t, 3, ExitCode(err))
	require.ErrorContains(t, err, "\nIf jq-1.9.0 is a release: dockhand bump jq 1.9.0")
	result, err := jsonOf(t, "update", "jq", "--plan")
	require.Equal(t, 3, ExitCode(err))
	require.Nil(t, result.Result, "a refusal before anything is done reports only its error")
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")

	out, _, err := dockhand(t, "update", "jq", "1.9.0", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "jq: 1.7.1 → 1.9.0")
}

// regenerating is a preparer whose update wrote a dependency block again.
type regenerating struct{ bumper }

func (b regenerating) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	result, err := b.bumper.Prepare(ctx, r)
	result.Regenerated = []editprep.Regenerated{{Option: "cargo.crates", Count: 352, Changed: 160, Dropped: []editprep.Override{{Name: "soundtouch", Pinned: "0.4.1", Was: "0.4.0", Locked: "0.5.4"}}}, {Option: "cargo.crates_github", Count: 0, Changed: 0}}
	return result, err
}

// An update says what it wrote of the dependency blocks, not only the
// distfiles: hk's said "1 distfile" of 280 lines of crates (the gh, usql,
// hk, and pgdog run's finding 4).
func TestAnUpdateCountsTheCratesItWrote(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return regenerating{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })
	result, err := jsonOf(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"option": "cargo.crates", "count": float64(352), "changed": float64(160),
		"dropped_pins": []any{map[string]any{"name": "soundtouch", "pinned": "0.4.1", "lock_had": "0.4.0", "lock_has": "0.5.4"}}}, dig(t, result.Result, "regenerated", 0))
	out, _, err := dockhand(t, "update", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, " and 352 crates (160 changed).\n", "an empty block says nothing: no \"and 0 Git crates (0 changed)\" (the txt run's finding 6)")
	require.Contains(t, out, "The Portfile pinned soundtouch 0.4.1 over the lock's 0.4.0; the new lock has 0.5.4, so the pin is dropped.\n")
}
