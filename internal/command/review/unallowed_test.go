package review

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// countingReader is a port reader that counts the reads it's asked for,
// and those that came with no owner.
type countingReader struct {
	checks.PortReader
	reads, unmarked int
}

func (r *countingReader) Directory(context.Context, model.Source, string) (string, error) {
	return "textproc/jq", nil
}
func (r *countingReader) Ports(ctx context.Context, _ model.Source, _ string, _ model.Environment, _ map[string]bool) ([]macports.PortInfo, error) {
	r.reads++
	if _, ok := workspace.OwnerOf(ctx); !ok {
		r.unmarked++
	}
	return []macports.PortInfo{{Name: "jq", Version: "1", Options: map[string]string{"categories": "textproc"}}}, nil
}

// errReached is a preparation that got past the door.
var errReached = errors.New("the preparation was reached")

// doorPreparer stages the source through the real door, then stops.
type doorPreparer struct{ e *engine.Engine }

func (p *doorPreparer) ResolveRelease(ctx context.Context, r editprep.Request) (model.Release, error) {
	files, err := workspace.Open(ctx, p.e.Repo, r.Source)
	if err != nil {
		return model.Release{}, err
	}
	_ = files.Close()
	return model.Release{}, errReached
}
func (p *doorPreparer) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	files, err := workspace.Open(ctx, p.e.Repo, r.Source)
	if err != nil {
		return editprep.Result{}, err
	}
	_ = files.Close()
	return editprep.Result{}, errReached
}

// An adopted pull request's Portfiles the person hasn't allowed are never
// evaluated by the authoring commands: checksums and revbump in a script
// and after a No on a terminal, and rename, each stop at the door, which
// fails closed, and a command that asked and got No stops there (Codex's
// confirmation of #162's fixes).
func TestUnallowedAuthoringDoesntEvaluate(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	testsupport.WriteFiles(t, w.Upstream, map[string]string{"textproc/jq/Portfile": "PortSystem 1.0\nname jq\nversion 1\n"})
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905}, HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}
	_, said, err := cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err, said)
	cmdtest.Stands(t).Ports.Reader = &countingReader{}
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer { return &doorPreparer{e: e} }
	for _, verb := range []string{"revbump", "checksums"} {
		for _, terminal := range []bool{false, true} {
			name := verb + map[bool]string{false: " in a script", true: " answered No"}[terminal]
			var out, errs bytes.Buffer
			streams := term.Streams{In: strings.NewReader("n\n"), Out: &out, Err: &errs}
			if terminal {
				streams = streams.AsTerminal()
			}
			args := []string{verb, "jq", "-b", "pr-34905", "--plan"}
			if verb == "revbump" {
				args = append(args, "--subject", "rebuild for test")
			}
			err := cmdtest.RunFor(t, t.Context(), args, streams)
			require.ErrorContains(t, err, "dockhand adopt --pr 34905 --evaluate allows it", name)
			require.NotErrorIs(t, err, errReached, "%s: the preparation was never reached", name)
		}
	}
	spy := &countingReader{}
	cmdtest.Stands(t).Ports.Reader = spy
	_, _, err = cmdtest.Dockhand(t, "rename", "jq", "jq2", "-b", "pr-34905", "--plan")
	require.ErrorContains(t, err, "dockhand adopt --pr 34905 --evaluate allows it")
	require.Zero(t, spy.reads, "an unallowed adopted branch never reaches the reader")
}
