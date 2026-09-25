package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
)

const (
	oldSHA = "1f3a0000000000000000000000000000000000000000000000000000000c2d9"
	newSHA = "9b0c00000000000000000000000000000000000000000000000000000000077e1"
)

// rechecksummer stands in for MacPorts in a checksum refresh: upstream now
// serves jq-1.7.1.tar.gz with other contents.
type rechecksummer struct{ repo *git.Repository }

func (rechecksummer) ResolveRelease(context.Context, preparation.Request) (record.Release, error) {
	return record.Release{}, nil
}

func (r rechecksummer) Prepare(ctx context.Context, request preparation.Request) (preparation.Result, error) {
	name := "textproc/jq/Portfile"
	before, data, err := r.repo.File(ctx, string(request.Source.Tree), name)
	if err != nil {
		return preparation.Result{}, err
	}
	line := regexp.MustCompile(`(?m)^checksums .*$`)
	declared := line.FindString(string(data))
	version := regexp.MustCompile(`(?m)^version\s+(\S+)$`).FindStringSubmatch(string(data))[1]
	after := line.ReplaceAllString(string(data), "checksums           sha256 "+newSHA+" size 7114508")
	port := func(checksums string) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Name: "jq", Version: version, Options: map[string]string{"checksums": checksums}}}}
	}
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := r.repo.EditTree(ctx, string(request.Source.Tree), []git.FileEdit{edit})
	return preparation.Result{Target: record.Target{Name: "jq", Portfile: name}, PreparedTree: record.ObjectID(tree), Files: []git.FileEdit{edit},
		Fidelity:  []portedit.Fidelity{{Before: port(declared[len("checksums "):]), After: port("")}},
		Downloads: []archives.Download{{Checksum: portfile.Checksum{Name: "jq-" + version + ".tar.gz", SHA256: newSHA, Size: 7114508}}}}, err
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
		"Updated checksums (1 distfile) and dist_subdir jq/1.7.1_1, so mirrors keep both archives.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"Inspect the source change before deciding whether it needs a revision bump.\n")
	dir := regexp.MustCompile(`· (\S+)\n`).FindStringSubmatch(out)[1]
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, dir[2:], "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Contains(t, string(data), "size 7114508\ndist_subdir         ${name}/${version}_1\n")

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

func (refuser) ResolveRelease(context.Context, preparation.Request) (record.Release, error) {
	return record.Release{Version: "1.8.1"}, nil
}

func (refuser) Prepare(context.Context, preparation.Request) (preparation.Result, error) {
	return preparation.Result{}, fmt.Errorf("%w: its pre-fetch hook runs exec", preparation.ErrUnsupported)
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
