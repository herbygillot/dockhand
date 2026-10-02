package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

const (
	oldSHA = "1f3a0000000000000000000000000000000000000000000000000000000c2d9"
	newSHA = "9b0c00000000000000000000000000000000000000000000000000000000077e1"
)

// rechecksummer stands in for MacPorts in a checksum refresh: upstream now
// serves jq-1.7.1.tar.gz with other contents.
type rechecksummer struct{ repo *git.Repository }

func (rechecksummer) ResolveRelease(context.Context, editprep.Request) (model.Release, error) {
	return model.Release{}, nil
}

func (r rechecksummer) Prepare(ctx context.Context, request editprep.Request) (editprep.Result, error) {
	name := "textproc/jq/Portfile"
	before, data, err := r.repo.File(ctx, string(request.Source.Tree), name)
	if err != nil {
		return editprep.Result{}, err
	}
	line := regexp.MustCompile(`(?m)^checksums .*$`)
	declared := line.FindString(string(data))[len("checksums "):]
	version := regexp.MustCompile(`(?m)^version\s+(\S+)$`).FindStringSubmatch(string(data))[1]
	now := portfile.Checksum{Name: "jq-" + version + ".tar.gz", SHA256: newSHA, Size: 7114508}
	after := []byte(line.ReplaceAllString(string(data), "checksums           sha256 "+newSHA+" size 7114508"))
	// As the editor does, for a Portfile the branch hasn't changed since
	// its base: the revision bumped unless asked not to, and dist_subdir set.
	revision := 0
	var stealth *editprep.Stealth
	if asked := request.Stealth; asked != nil && !slices.Contains(asked.Changed, name) {
		stealth = &editprep.Stealth{Distfiles: []editprep.StealthDistfile{{Name: now.Name, Was: distfetch.Declared(declared)[""], Now: now}}}
		if !asked.KeepRevision {
			if after, err = portfile.BumpRevision(after, "", 0); err != nil {
				return editprep.Result{}, err
			}
			stealth.Revbumped, revision = true, 1
		}
		if after, _, err = portfile.StealthDistSubdir(after, stealth.Revbumped); err != nil {
			return editprep.Result{}, err
		}
		stealth.DistSubdir = "jq/" + version + "_1"
	}
	port := func(checksums string, revision int) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Name: "jq", Version: version, Revision: revision, Options: map[string]string{"checksums": checksums}}}}
	}
	edit := git.FileEdit{Path: name, Before: before, After: after, Mode: before.Mode}
	tree, err := r.repo.EditTree(ctx, string(request.Source.Tree), []git.FileEdit{edit})
	result := editprep.Result{Target: model.Target{Name: "jq", Portfile: name}, PreparedTree: model.ObjectID(tree), Files: []git.FileEdit{edit},
		Fidelity:  []portedit.Fidelity{{Before: port(declared, 0), After: port("", revision)}},
		Downloads: []distfetch.Download{{Checksum: now}}}
	result.Stealth = stealth
	return result, err
}

func TestAStealthUpdateSaysSoAndKeepsBothArchives(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name                jq\nversion             1.7.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	gitRun(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
	testPreparer = func(e *engine.Engine) engine.Preparer { return rechecksummer{repo: e.Repo} }
	t.Cleanup(func() { testPreparer = nil })

	out, _, err := dockhand(t, "checksums", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.7.1 · the distfile changed upstream without a new name (stealth update)\n"+
		"  was   sha256 1f3a…c2d9   size 7,114,391\n"+
		"  now   sha256 9b0c…77e1   size 7,114,508\n"+
		"Updated checksums (1 distfile), revision 0 → 1, and dist_subdir jq/1.7.1_1, so mirrors keep both archives.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"The source changed, so the revision is bumped; --no-revbump leaves it, for a change that needs no rebuild.\n")
	dir := regexp.MustCompile(`· (\S+)\n`).FindStringSubmatch(out)[1]
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name                jq\nversion             1.7.1\nrevision            1\nchecksums           sha256 "+newSHA+" size 7114508\ndist_subdir         ${name}/${version}_${revision}\n", string(data))

	// A version edited by hand first is not a stealth update.
	t.Setenv("MACPORTS_TREE", filepath.Join(home, dir[2:]))
	require.NoError(t, os.WriteFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"), []byte("name                jq\nversion             1.8.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	out, _, err = dockhand(t, "checksums", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.8.1\nUpdated checksums (1 distfile).\n")
	require.NotContains(t, out, "stealth")
}

// refuser stands in for MacPorts with a Portfile dockhand won't edit.
type refuser struct{}

func (refuser) ResolveRelease(context.Context, editprep.Request) (model.Release, error) {
	return model.Release{Version: "1.8.1"}, nil
}

func (refuser) Prepare(context.Context, editprep.Request) (editprep.Result, error) {
	return editprep.Result{}, fmt.Errorf("%w: its pre-fetch hook runs exec", editprep.ErrUnsupported)
}

func TestWhatDockhandCantEditItSaysHowToDoByHand(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(*engine.Engine) engine.Preparer { return refuser{} }
	t.Cleanup(func() { testPreparer = nil })

	_, _, err := dockhand(t, "update", "jq", "--new")
	require.Error(t, err)
	require.Regexp(t, `^can't update jq by itself: its pre-fetch hook runs exec\nKept: dockhand/jq-[a-z0-9]{4}, with nothing changed\.\n`+
		`Edit the version yourself; dockhand checksums jq then fills in the rest:\n  dockhand edit jq$`, err.Error())

	_, _, err = dockhand(t, "checksums", "jq", "--new")
	require.ErrorContains(t, err, "can't refresh jq's checksums by itself: its pre-fetch hook runs exec\n")

	refused, err := jsonOf(t, "update", "jq", "--new")
	require.Error(t, err)
	require.Contains(t, *refused.Error, "can't update jq by itself")
	require.Equal(t, true, refused.Result["started"], "the result names the branch kept for the edit by hand")
	require.Regexp(t, `^dockhand/jq-[a-z0-9]{4}$`, dig(t, refused.Result, "branch", "git_branch"))
}

func TestUpdateSubmitTidiesChecksAndSubmits(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)

	_, _, err := dockhand(t, "update", "jq", "--plan", "--submit")
	require.ErrorContains(t, err, "--submit goes with one port's update")

	out, errs, err := dockhand(t, "update", "jq", "--new", "--submit")
	require.NoError(t, err, errs)
	require.NotContains(t, out, "Next: review it")
	require.Regexp(t, `jq: 1.7.1 → 1.8.1 .*\n(.*\n)*\njq-[a-z0-9]{4} · tidying edits not yet committed\n\nProposed commit\n`, out)
	require.Contains(t, out, "checking commit ")
	require.Contains(t, out, "Opened #34901")
	require.Len(t, g.prs, 1)
}

// update --submit passes on what submit --check takes, and settles where
// to check before it edits anything. On a terminal, --yes applies a tidy of
// dockhand's own edit without asking.
func TestUpdateSubmitPassesOnWhereAndWhatWasTested(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)

	_, _, err := dockhand(t, "update", "jq", "--new", "--on", "command")
	require.EqualError(t, err, "--on, --tested-binaries, and --tested-variants go with --submit")
	_, _, err = dockhand(t, "update", "jq", "--new", "--yes")
	require.EqualError(t, err, "--yes goes with --outdated or --submit")

	_, _, err = dockhand(t, "update", "jq", "--new", "--submit", "--on", "nowhere")
	require.EqualError(t, err, `--on nowhere: no provider "nowhere" is set up; nothing was changed`)
	require.Empty(t, gitRun(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")

	terminal := func(args ...string) (string, string) {
		var out, errs bytes.Buffer
		err := Run(t.Context(), args, Streams{In: strings.NewReader(""), Out: &out, Err: &errs, interactive: true})
		require.NoError(t, err, errs.String())
		return out.String(), errs.String()
	}
	out, prompts := terminal("update", "jq", "--new", "--submit", "--on", "command")
	require.Contains(t, prompts, "Apply [a]", "without --yes, a terminal reviews the tidy")
	require.Contains(t, out, "Nothing was checked or submitted.")

	out, prompts = terminal("update", "jq", "--new", "--submit", "--yes", "--on", "command", "--tested-binaries")
	require.NotContains(t, prompts, "Apply [a]")
	require.NotContains(t, prompts, "Did you test", "the flag answered the template's questions")
	require.Contains(t, out, "Opened #34901")
	require.Len(t, g.prs, 1)
	require.Contains(t, g.prs[0].Body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, g.prs[0].Body, "- [ ] checked that the Portfile's most important [variants]")
}

func TestAStealthUpdateWithoutARevbumpNumbersTheDirectory(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name                jq\nversion             1.7.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	gitRun(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
	testPreparer = func(e *engine.Engine) engine.Preparer { return rechecksummer{repo: e.Repo} }
	t.Cleanup(func() { testPreparer = nil })

	out, _, err := dockhand(t, "checksums", "jq", "--new", "--no-revbump")
	require.NoError(t, err)
	require.Contains(t, out, "Updated checksums (1 distfile) and dist_subdir jq/1.7.1_1, so mirrors keep both archives.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"Inspect the source change before deciding whether it needs a revision bump.\n")
	dir := regexp.MustCompile(`· (\S+)\n`).FindStringSubmatch(out)[1]
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "size 7114508\ndist_subdir         ${name}/${version}_1\n")
	require.NotContains(t, string(data), "revision")
}

func TestANewVersionRemovesTheStealthDistSubdir(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\ndist_subdir         ${name}/${version}_${revision}\nrevision 1\n"), 0o644))
	gitRun(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
	withBumper(t)

	planned, _, err := dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "Removes dist_subdir: a stealth update set it for the old version, and every archive of the new version has a name of its own.\n",
		"the plan says it, as the update does (the hugo exercise's yq run, finding 2)")
	out, _, err := dockhand(t, "update", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "Removed dist_subdir: a stealth update set it for the old version, and every archive of the new version has a name of its own.\n")
	dir := regexp.MustCompile(`· (\S+)\n`).FindStringSubmatch(out)[1]
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\nrevision 1\n", string(data))
}
