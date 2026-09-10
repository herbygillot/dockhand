package staging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// THE PREFLIGHT IS AN EVALUATION AND NEEDS THE TREE IT EVALUATES IN.
// It reads known_fail and use_xcode out of the staged Portfile, and a
// Portfile may open a port group. Read before _resources is
// materialized, getportresourcepath falls back to the INSTALLATION'S
// default tree — or finds nothing and leaves the member unread, which
// run.Plan schedules as an ordinary build. Either way the commit under
// test does not get to answer the question that decides whether a VM is
// spent on it.
func TestPreflightReadsThePortGroupTheCommitCarries(t *testing.T) {
	prefix := testenv.MacPortsPrefix(t)
	repo := gittest.Init(t, tool.NewFinder(nil), "", map[string]string{
		"devel/gated/Portfile":                  "PortSystem 1.0\nPortGroup gate 1.0\nname gated\nversion 1.0\n",
		macports.PortGroupDir + "/gate-1.0.tcl": "known_fail yes\nuse_xcode yes\n",
	})
	sha, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)

	root, err := tempdir.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Remove() })

	s := New(repo, root, func(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error) {
		return eval.Start(ctx, prefix, opts...)
	})
	t.Cleanup(s.Cleanup)

	_, pre, err := s.Stage(context.Background(), sha,
		[]record.Subject{{Port: "gated", Portdir: "devel/gated"}},
		platform.Release{Darwin: 24, Name: "Sonoma"})
	require.NoError(t, err)

	pf := pre["gated"]
	require.NoError(t, pf.Err, "the port group the commit carries must be readable when the member is read")
	require.True(t, pf.Read, "an unread preflight is scheduled as an ordinary build")
	assert.True(t, pf.KnownFail, "known_fail comes from the commit's own port group")
	assert.True(t, pf.NeedsXcode, "and so does use_xcode")
}
