package cmdtest

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// Bumper stands in for MacPorts: a bump rewrites the version line, and a
// checksum refresh adds a checksums line.
type Bumper struct{ Repo *git.Repository }

func (b Bumper) ResolveRelease(ctx context.Context, r editprep.Request) (model.Release, error) {
	version := r.Version
	if version == "" {
		version = "1.8.1"
	}
	// As discovery does, it says a release the port is at already is no
	// update.
	_, data, err := b.Repo.File(ctx, string(r.Source.Tree), "textproc/"+r.Selection.Selector+"/Portfile")
	if err != nil {
		return model.Release{}, err
	}
	current := regexp.MustCompile(`(?m)^version (\S+)$`).FindSubmatch(data)
	noUpdate := current != nil && string(current[1]) == version
	return model.Release{ReleaseSelection: model.ReleaseSelection{NoUpdate: noUpdate}, Version: version, Forge: "github", Repository: "jqlang/jq", Tag: "jq-" + version, Commit: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"}, nil
}

func (b Bumper) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	name := "textproc/" + r.Selection.Selector + "/Portfile"
	before, data, err := b.Repo.File(ctx, string(r.Source.Tree), name)
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
	tree, err := b.Repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	result.Files, result.PreparedTree = []git.FileEdit{edit}, model.ObjectID(tree)
	if r.Action == model.EditRevbump {
		result.Commits = []editprep.CommitIntent{{Subject: "jq: " + r.Subject}}
	}
	return result, err
}

func WithBumper(t *testing.T) {
	Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return Bumper{Repo: e.Repo} }
}

func Versioned(t *testing.T, w World) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.7.1")
}
