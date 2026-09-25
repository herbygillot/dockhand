package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
)

// bumper stands in for MacPorts: a bump rewrites the version line, and a
// checksum refresh adds a checksums line.
type bumper struct{ repo *git.Repository }

func (b bumper) ResolveRelease(_ context.Context, r preparation.Request) (record.Release, error) {
	version := r.Version
	if version == "" {
		version = "1.8.1"
	}
	return record.Release{Version: version, Forge: "github", Tag: "jq-" + version}, nil
}

func (b bumper) Prepare(ctx context.Context, r preparation.Request) (preparation.Result, error) {
	name := "textproc/" + r.Selection.Selector + "/Portfile"
	before, data, err := b.repo.File(ctx, string(r.Source.Tree), name)
	if err != nil {
		return preparation.Result{}, err
	}
	line := regexp.MustCompile(`(?m)^version (\S+)$`)
	old := "1.7.1"
	if m := line.FindSubmatch(data); m != nil {
		old = string(m[1])
	}
	next, after := old, string(data)
	revision := 0
	switch {
	case r.Action == record.Bump:
		next = r.Release.Version
		after = line.ReplaceAllString(after, "version "+next)
	case r.Action == record.BumpRevision:
		revision = 1
		after += "revision 1\n"
	case !strings.Contains(after, "checksums"):
		after += "checksums sha256 0000\n"
	}
	port := func(v string, revision int) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{"jq": {Version: v, Revision: revision}}}
	}
	result := preparation.Result{Target: record.Target{Name: "jq"}, Release: r.Release, PreparedTree: r.Source.Tree, Fidelity: []portedit.Fidelity{{Before: port(old, 0), After: port(next, revision)}}}
	if after == string(data) {
		return result, nil
	}
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := b.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	result.Files, result.PreparedTree = []git.FileEdit{edit}, record.ObjectID(tree)
	if r.Action == record.BumpRevision {
		result.Commits = []preparation.CommitIntent{{Subject: "jq: " + r.Subject}}
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
	gitRun(t, w.upstream, "commit", "-q", "-am", "jq: 1.7.1")
}

func TestUpdateInTheBranchCheckedOutHere(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)

	out, _, err := dockhand(t, "update", "jq", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ~/src/macports-branches/jq-update\n")
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)\nPlan, nothing changed:\n\n")
	require.Contains(t, out, "+version 1.8.1")
	require.NoFileExists(t, filepath.Join(dir, "textproc/jq/Portfile"))

	out, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	require.Equal(t, "jq-update · ~/src/macports-branches/jq-update\n"+
		"jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)\n"+
		"Updated version and checksums.\n"+
		"Changed: textproc/jq/Portfile\n"+
		"Next: review it with git diff, then commit it\n", out)
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

func TestUpdateWithoutABranchAsksOrSaysHow(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)

	_, _, err := dockhand(t, "update", "jq")
	require.ErrorContains(t, err, "jq is in no open branch, and this checkout is on none; start one with --new, or name one with --branch <name>")

	var out, errs bytes.Buffer
	err = Run(t.Context(), []string{"update", "jq"}, Streams{In: strings.NewReader("\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Regexp(t, `^jq is in no open branch\.\n\? start dockhand/jq-[a-z0-9]{4} for it\? \[Y/n\] $`, errs.String())
	started := regexp.MustCompile(`Started dockhand/(jq-[a-z0-9]{4}) from master `).FindStringSubmatch(out.String())
	require.NotNil(t, started, out.String())
	name := started[1]
	require.Contains(t, out.String(), name+" · ~/src/macports-branches/"+name+"\n")
	gitRun(t, filepath.Join(w.home, "src", "macports-branches", name), "commit", "-q", "-am", "jq: update to 1.8.1")

	_, _, err = dockhand(t, "update", "jq")
	require.ErrorContains(t, err, "jq is changed in "+name+"; name it with --branch "+name+", or start another with --new")

	out.Reset()
	errs.Reset()
	err = Run(t.Context(), []string{"checksums", "jq"}, Streams{In: strings.NewReader("t\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "jq is changed in 1 open branch: dockhand/"+name+"\n? refresh jq there, or start a new branch? [t]here / [n]ew / [q]uit ")
	require.Contains(t, out.String(), name+" · ")
	require.Contains(t, out.String(), "Updated checksums.")

	out.Reset()
	err = Run(t.Context(), []string{"update", "jq", "1.9", "--new"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	another := regexp.MustCompile(`Started dockhand/(jq-[a-z0-9]{4}) from master `).FindStringSubmatch(out.String())
	require.NotNil(t, another, out.String())
	require.NotEqual(t, name, another[1])
	require.Contains(t, out.String(), "jq: 1.7.1 → 1.9")

	out.Reset()
	err = Run(t.Context(), []string{"update", "jq", "2.0", "--branch", another[1]}, Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	require.NoError(t, err)
	require.Contains(t, out.String(), "jq: 1.9 → 2.0")

	_, _, err = dockhand(t, "update", "jq", "--new", "--plan")
	require.ErrorContains(t, err, "starts no branch")
	_, _, err = dockhand(t, "update", "jq", "--new", "--branch", name)
	require.ErrorContains(t, err, "none of the others can be")
}

func TestAnUntrackedBranchHereIsTheirsToAdopt(t *testing.T) {
	w := newWorld(t)
	withBumper(t)
	gitRun(t, w.clone, "switch", "-q", "-c", "mine")
	_, _, err := dockhand(t, "update", "jq")
	require.ErrorContains(t, err, "mine is not tracked; dockhand adopt tracks it")
}
