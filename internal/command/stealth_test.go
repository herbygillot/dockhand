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
	w := newWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name                jq\nversion             1.7.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	testsupport.Git(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
	stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return rechecksummer{repo: e.Repo} }

	out, _, err := dockhand(t, "checksums", "jq", "--new")
	require.NoError(t, err)
	require.Contains(t, out, "jq 1.7.1 · the distfile changed upstream without a new name (stealth update)\n"+
		"  was   sha256 1f3a…c2d9   size 7,114,391\n"+
		"  now   sha256 9b0c…77e1   size 7,114,508\n"+
		"Updated checksums (1 distfile), revision 0 → 1, and dist_subdir jq/1.7.1_1, so mirrors keep both archives.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"Bumped the revision because the source changed. Use --no-revbump only if the change needs no rebuild.\n")
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
	stands(t).Preparer = func(*engine.Engine) engine.Preparer { return refuser{} }

	_, _, err := dockhand(t, "update", "jq", "--new")
	require.Error(t, err)
	require.Regexp(t, `^Could not update jq automatically: its pre-fetch hook runs exec\nKept: dockhand/jq-[a-z0-9]{4}, with nothing changed\.\n`+
		`Edit the version, run port checksum jq, then copy the reported checksums into the Portfile\.\nEdit with: dockhand edit jq$`, err.Error())

	_, _, err = dockhand(t, "checksums", "jq", "--new")
	require.ErrorContains(t, err, "Could not update the checksums for jq automatically: its pre-fetch hook runs exec\n")

	refused, err := jsonOf(t, "update", "jq", "--new")
	require.Error(t, err)
	require.Contains(t, *refused.Error, "Could not update jq automatically")
	require.Equal(t, true, refused.Result["started"], "the result names the branch kept for the edit by hand")
	require.Regexp(t, `^dockhand/jq-[a-z0-9]{4}$`, dig(t, refused.Result, "branch", "git_branch"))
}

// ship settles where to check before it edits anything, and passes on
// what was tested.
func TestShipSettlesWhereBeforeTheEdit(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	_, _, err := dockhand(t, "update", "jq", "--new", "--on", "command")
	require.ErrorContains(t, err, "unknown flag: --on", "update is the edit alone")
	_, _, err = dockhand(t, "ship", "jq", "--yes", "--on", "nowhere")
	require.EqualError(t, err, `--on nowhere: no provider "nowhere" is set up; nothing was changed`)
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/*"), "no branch was started")

	var out, errs bytes.Buffer
	err = runFor(t, t.Context(), []string{"ship", "jq", "--yes", "--on", "command", "--tested-binaries"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.NotContains(t, errs.String(), "Apply [a]")
	require.NotContains(t, errs.String(), "Did you test", "the flag answered the template's questions")
	require.Contains(t, out.String(), "Opened #34901")
	require.Len(t, g.PRs, 1)
	require.Contains(t, g.PRs[34901].Body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, g.PRs[34901].Body, "- [ ] checked that the Portfile's most important [variants]")
}

func TestAStealthUpdateWithoutARevbumpNumbersTheDirectory(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name                jq\nversion             1.7.1\nchecksums           sha256 "+oldSHA+" size 7114391\n"), 0o644))
	testsupport.Git(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
	stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return rechecksummer{repo: e.Repo} }

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
	testsupport.Git(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
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
