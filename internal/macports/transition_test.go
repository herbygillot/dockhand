package macports_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// A port's transition across a change: kept, added, removed, moved with
// its directory, renamed by its stub's replaced_by or by the base's
// directory having one port the revision's no longer has, and a base
// directory of two such ports left as an addition and removals, which
// can't be told apart (the roadmap's "A port's identity across a move or
// rename").
func TestAPortsTransitionAcrossAChange(t *testing.T) {
	t.Parallel()
	sources := [2]model.Source{{Tree: "base"}, {Tree: "revision"}}
	side := func(i int, directory, name string) macports.PortSide {
		return macports.PortSide{Source: sources[i], Directory: directory, Name: name}
	}
	moved := macports.Scope{Ports: []string{"macports/dockhand", "textproc/jq2"}, Moved: []macports.Move{
		{From: "sysutils/dockhand", To: "macports/dockhand"},
		{From: "textproc/jq", To: "textproc/jq2", Stub: true, Port: "jq"},
	}}
	for _, test := range []struct {
		name          string
		scope         macports.Scope
		directory     string
		before, after []string
		want          []macports.PortTransition
	}{
		{name: "kept", directory: "textproc/jq", before: []string{"jq"}, after: []string{"jq"},
			want: []macports.PortTransition{{Before: side(0, "textproc/jq", "jq"), After: side(1, "textproc/jq", "jq"), Kind: macports.TransitionKept}}},
		{name: "added", directory: "devel/new", after: []string{"new"},
			want: []macports.PortTransition{{Before: side(0, "devel/new", ""), After: side(1, "devel/new", "new"), Kind: macports.TransitionAdded}}},
		{name: "removed", directory: "devel/gone", before: []string{"gone"},
			want: []macports.PortTransition{{Before: side(0, "devel/gone", "gone"), After: side(1, "devel/gone", ""), Kind: macports.TransitionRemoved}}},
		{name: "moved", scope: moved, directory: "macports/dockhand", before: []string{"dockhand"}, after: []string{"dockhand"},
			want: []macports.PortTransition{{Before: side(0, "sysutils/dockhand", "dockhand"), After: side(1, "macports/dockhand", "dockhand"), Kind: macports.TransitionMoved}}},
		{name: "renamed by replaced_by", scope: moved, directory: "textproc/jq2", before: []string{"jq", "jq-devel"}, after: []string{"jq2", "jq-devel"},
			want: []macports.PortTransition{
				{Before: side(0, "textproc/jq", "jq"), After: side(1, "textproc/jq2", "jq2"), Kind: macports.TransitionRenamed},
				{Before: side(0, "textproc/jq", "jq-devel"), After: side(1, "textproc/jq2", "jq-devel"), Kind: macports.TransitionMoved},
			}},
		{name: "renamed by the one-port rule", directory: "devel/goplus", before: []string{"goplus"}, after: []string{"xgo"},
			want: []macports.PortTransition{{Before: side(0, "devel/goplus", "goplus"), After: side(1, "devel/goplus", "xgo"), Kind: macports.TransitionRenamed}}},
		{name: "the one-port rule takes the directory's own name among several", directory: "devel/xgo", before: []string{"goplus"}, after: []string{"xgo", "xgo-devel"},
			want: []macports.PortTransition{
				{Before: side(0, "devel/xgo", "goplus"), After: side(1, "devel/xgo", "xgo"), Kind: macports.TransitionRenamed},
				{Before: side(0, "devel/xgo", ""), After: side(1, "devel/xgo", "xgo-devel"), Kind: macports.TransitionAdded},
			}},
		{name: "two base ports gone are ambiguous", directory: "devel/pair", before: []string{"one", "two"}, after: []string{"three"},
			want: []macports.PortTransition{
				{Before: side(0, "devel/pair", ""), After: side(1, "devel/pair", "three"), Kind: macports.TransitionAdded},
				{Before: side(0, "devel/pair", "one"), After: side(1, "devel/pair", ""), Kind: macports.TransitionRemoved},
				{Before: side(0, "devel/pair", "two"), After: side(1, "devel/pair", ""), Kind: macports.TransitionRemoved},
			}},
	} {
		require.Equal(t, test.want, test.scope.Transitions(sources, test.directory, test.before, test.after), test.name)
	}

	transitions := moved.Transitions(sources, "textproc/jq2", []string{"jq"}, []string{"jq2"})
	renamed, ok := macports.TransitionOf(transitions, "jq2")
	require.True(t, ok)
	require.Equal(t, "jq", renamed.Before.Name)
	_, ok = macports.TransitionOf(transitions, "jq")
	require.False(t, ok, "no port at the revision is named jq")
}
