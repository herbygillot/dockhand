package comparison

import (
	"context"
	"slices"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/distributable"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// treeDistribution judges a port with its dependency's license at each
// tree, as the port index of that tree has it.
type treeDistribution struct {
	caller       distributable.Caller
	dependencies map[model.ObjectID]distributable.Info
	t            *testing.T
}

func (j treeDistribution) Distributable(ctx context.Context, source model.Source, port macports.PortInfo, _ []byte) assess.Verdict {
	top := distributable.Info{License: port.Options["license"], InstallsLibs: true}
	for _, dep := range port.Dependencies {
		top.Dependencies = append(top.Dependencies, dep.Port)
	}
	result, err := distributable.Judge(ctx, j.caller, port.Name, map[string]distributable.Info{port.Name: top, "dependency": j.dependencies[source.Tree]})
	require.NoError(j.t, err)
	return assess.Verdict{Known: result.Known, OK: result.OK, Reason: result.Reason}
}

// The candidate is judged distributable at its own tree, with its
// dependencies' licenses as it has them: it was judged with the base's,
// so a dependency the branch relicensed went unseen (Codex's sixth
// review, #72, its probe).
func TestTheCandidateIsJudgedAtItsOwnTree(t *testing.T) {
	t.Parallel()
	process, err := shell.Start(t.Context(), testsupport.MacPortsTclsh(t))
	require.NoError(t, err)
	session, err := rpc.New(t.Context(), process)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	_, err = session.Call(t.Context(), "eval", distributable.Script())
	require.NoError(t, err)
	judge := treeDistribution{caller: session, t: t, dependencies: map[model.ObjectID]distributable.Info{
		"base": {License: "MIT", InstallsLibs: true},
		"head": {License: "OpenSSL", InstallsLibs: true},
	}}
	sources := [2]model.Source{{Tree: "base"}, {Tree: "head"}}
	port := macports.PortInfo{Name: "harbor", Options: map[string]string{"license": "GPL-2"}, Dependencies: []macports.Dependency{{Phase: "lib", Port: "dependency"}}}
	atBase := judge.Distributable(t.Context(), sources[0], port, nil)
	atHead := judge.Distributable(t.Context(), sources[1], port, nil)
	require.True(t, atBase.Known && atBase.OK)
	require.True(t, atHead.Known && !atHead.OK, atHead.Reason)
	c := Collector{Judge: judge}
	comparison := c.collect(t.Context(), assess.Input{Base: port, Port: port}, sources, nil, false)
	require.True(t, slices.ContainsFunc(comparison.Changes, func(c model.UpstreamChange) bool { return c.Rule == assess.NotDistributable && c.Hold }), "a candidate dependency license conflict must reach the candidate assessment")
}
