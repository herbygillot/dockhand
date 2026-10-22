package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
		if after, err = portfile.StealthDistSubdir(after, stealth.Revbumped); err != nil {
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
	t.Parallel()
	w := cmdtest.NewWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name                jq\nversion             1.7.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.7.1")
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return rechecksummer{repo: e.Repo} }

	out, _, err := cmdtest.Dockhand(t, "checksums", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.7.1 · the distfile changed upstream without a new name (stealth update)\n"+
		"  was   sha256 1f3a…c2d9   size 7,114,391\n"+
		"  now   sha256 9b0c…77e1   size 7,114,508\n"+
		"Updated checksums (1 distfile), revision 0 → 1, and dist_subdir jq/1.7.1_1, so mirrors keep both\n  archives.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"Bumped the revision because the source changed. Use --no-revbump only if the change needs no\n  rebuild.\n")
	dir := regexp.MustCompile(` in (\S+)\n`).FindStringSubmatch(cmdtest.Flat(out))[1]
	home := cmdtest.Getenv(t, "HOME")
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name                jq\nversion             1.7.1\nrevision            1\nchecksums           sha256 "+newSHA+" size 7114508\ndist_subdir         ${name}/${version}_${revision}\n", string(data))

	// A version edited by hand first is not a stealth update.
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(home, dir[2:]))
	require.NoError(t, os.WriteFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"), []byte("name                jq\nversion             1.8.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	out, _, err = cmdtest.Dockhand(t, "checksums", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.8.1\nUpdated checksums (1 distfile).\n")
	require.NotContains(t, out, "stealth")
}

func TestWhatDockhandCantEditItSaysHowToDoByHand(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(*engine.Engine) engine.Preparer { return cmdtest.Refuser{} }

	_, _, err := cmdtest.Dockhand(t, "update", "jq", "--new")
	require.Error(t, err)
	require.Regexp(t, `^Could not update jq automatically: its pre-fetch hook runs exec\nKept: dockhand/jq-[a-z0-9]{4}, with nothing changed\.\n`+
		`Next: edit the version, run port checksum jq, then copy the reported checksums into the Portfile\nNext: dockhand edit jq$`, err.Error())

	_, _, err = cmdtest.Dockhand(t, "checksums", "jq", "--new")
	require.ErrorContains(t, err, "Could not update the checksums for jq automatically: its pre-fetch hook runs exec\n")

	refused, err := cmdtest.JSONOf(t, "update", "jq", "--new")
	require.Error(t, err)
	require.Contains(t, *refused.Error, "Could not update jq automatically")
	require.Equal(t, true, refused.Result["started"], "the result names the branch kept for the edit by hand")
	require.Regexp(t, `^dockhand/jq-[a-z0-9]{4}$`, cmdtest.Dig(t, refused.Result, "branch", "git_branch"))
}

// ship settles where to check before it edits anything, and passes on
// what was tested.
func TestShipSettlesWhereBeforeTheEdit(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil

	_, _, err := cmdtest.Dockhand(t, "update", "jq", "--new", "--on", "command")
	require.ErrorContains(t, err, "unknown flag: --on", "update is the edit alone")
	_, _, err = cmdtest.Dockhand(t, "ship", "jq", "--yes", "--on", "nowhere")
	require.EqualError(t, err, `--on nowhere: no provider "nowhere" is set up; nothing was changed`)
	require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "no branch was started")

	var out, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"ship", "jq", "--yes", "--on", "command", "--tested-binaries"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs}.AsTerminal())
	require.NoError(t, err, errs.String())
	require.NotContains(t, errs.String(), "Apply [a]")
	require.NotContains(t, errs.String(), "Did you test", "the flag answered the template's questions")
	require.Contains(t, out.String(), "opened #34901")
	require.Len(t, g.PRs, 1)
	require.Contains(t, g.PRs[34901].Body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, g.PRs[34901].Body, "- [ ] checked that the Portfile's most important [variants]")
}

func TestAStealthUpdateWithoutARevbumpNumbersTheDirectory(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name                jq\nversion             1.7.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.7.1")
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return rechecksummer{repo: e.Repo} }

	out, _, err := cmdtest.Dockhand(t, "checksums", "jq", "--new", "--no-revbump")
	require.NoError(t, err)
	require.Contains(t, out, "Updated checksums (1 distfile) and dist_subdir jq/1.7.1_1, so mirrors keep both archives.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"Inspect the source change before deciding whether it needs a revision bump.\n")
	dir := regexp.MustCompile(` in (\S+)\n`).FindStringSubmatch(cmdtest.Flat(out))[1]
	home := cmdtest.Getenv(t, "HOME")
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "size 7114508\ndist_subdir         ${name}/${version}_1\n")
	require.NotContains(t, string(data), "revision")
}

func TestANewVersionRemovesTheStealthDistSubdir(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\ndist_subdir         ${name}/${version}_${revision}\nrevision 1\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.7.1")
	cmdtest.WithBumper(t)

	planned, _, err := cmdtest.Dockhand(t, "update", "jq", "--new", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "Removes dist_subdir: a stealth update set it for the old version, and every archive of the new\n  version has a name of its own.\n",
		"the plan says it, as the update does (the hugo exercise's yq run, finding 2)")
	out, _, err := cmdtest.Dockhand(t, "update", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "Removed dist_subdir: a stealth update set it for the old version, and every archive of the new\n  version has a name of its own.\n")
	dir := regexp.MustCompile(` in (\S+)\n`).FindStringSubmatch(cmdtest.Flat(out))[1]
	home := cmdtest.Getenv(t, "HOME")
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\nrevision 1\n", string(data))
}
