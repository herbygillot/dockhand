package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/run"
)

// requiresLocal is run.Local answering only the forward lookup, which is
// the one question a build order asks.
type requiresLocal struct {
	quiet
	edges map[string][]string
	err   error
}

func (l requiresLocal) Requires(context.Context, []string) (map[string][]string, error) {
	return l.edges, l.err
}

func roster(ports ...string) []run.Member {
	out := make([]run.Member, 0, len(ports))
	for _, p := range ports {
		out = append(out, run.Member{Port: p, Names: []string{p}})
	}
	return out
}

// The shape the guest runner needs: every prerequisite at an EARLIER
// position, because it tests `[ -f state.$j ]` and a later member has no
// state file yet, so the check passes and the build runs anyway.
func TestBuildOrderPutsPrerequisitesFirst(t *testing.T) {
	// head is the headline; b needs a, and both need head.
	in := roster("head", "b", "a")
	local := requiresLocal{edges: map[string][]string{
		"head": nil,
		"b":    {"a", "head"},
		"a":    {"head"},
	}}
	seated, requires := buildOrder(context.Background(), local, in)

	assert.Equal(t, []string{"head", "a", "b"}, memberNames(seated))
	require.Len(t, requires, 3)
	assert.Empty(t, requires[0])
	assert.Equal(t, []string{"head"}, requires[1])
	assert.Equal(t, []string{"a", "head"}, requires[2])
}

// Members that do not constrain each other keep the order they arrived
// in, so a cohort does not shuffle between runs for no visible reason.
func TestBuildOrderIsStableAmongUnconstrainedMembers(t *testing.T) {
	in := roster("head", "z", "y", "x")
	local := requiresLocal{edges: map[string][]string{
		"z": {"head"}, "y": {"head"}, "x": {"head"},
	}}
	seated, _ := buildOrder(context.Background(), local, in)
	assert.Equal(t, []string{"head", "z", "y", "x"}, memberNames(seated))
}

// A dependency outside the roster is MacPorts' to build and constrains
// nothing here.
func TestBuildOrderIgnoresEdgesLeavingTheRoster(t *testing.T) {
	in := roster("head", "a")
	local := requiresLocal{edges: map[string][]string{
		"head": {"zlib", "ncurses"},
		"a":    {"openssl"},
	}}
	seated, requires := buildOrder(context.Background(), local, in)
	assert.Equal(t, []string{"head", "a"}, memberNames(seated))
	assert.Nil(t, requires, "no edge inside the roster is no graph at all")
}

// run.Finish reads Spec.Roster[0] as the port a cohort proposal is
// about, and run.Observe keys the headline's manifest off it. A sort
// that promoted a dependent would make both talk about the wrong port.
func TestBuildOrderNeverMovesTheHeadline(t *testing.T) {
	in := roster("head", "a")
	local := requiresLocal{edges: map[string][]string{"head": {"a"}}}
	seated, requires := buildOrder(context.Background(), local, in)
	assert.Equal(t, []string{"head", "a"}, memberNames(seated))
	assert.Nil(t, requires)
}

// A FORCED MEMBER IS SEATED LAST AND STAYS THERE. run.Roster puts it
// at the tail deliberately: it deactivates a sibling, so everything
// that might need that sibling has to be built first. A sort reading
// only the dependency graph moved it ahead of a member that needs it,
// which is the shape of the open question about a forced member that is
// itself a prerequisite — and resolving that by reordering would be
// answering it, quietly, in the wrong direction.
func TestBuildOrderKeepsAForcedMemberLast(t *testing.T) {
	in := []run.Member{
		{Port: "head"},
		{Port: "a"},
		{Port: "forced", Forced: "forced-devel"},
	}
	local := requiresLocal{edges: map[string][]string{
		"head": nil, "a": {"forced", "head"}, "forced": {"head"},
	}}
	seated, requires := buildOrder(context.Background(), local, in)
	assert.Equal(t, []string{"head", "a", "forced"}, memberNames(seated),
		"the deactivation must not be pulled ahead of members that need the sibling")
	// The edge is still stated, and the runner will find no state file
	// at a later position and build anyway — today's behaviour, said out
	// loud rather than reordered away.
	assert.Equal(t, []string{"forced", "head"}, requires[1])
}

// Every failure yields the roster as it stood and no edges, because an
// ordering is an optimization and may never refuse a verification.
func TestBuildOrderFallsBackRatherThanFailing(t *testing.T) {
	in := roster("head", "a", "b")
	for name, local := range map[string]run.Local{
		"no local at all":  nil,
		"the tree errored": requiresLocal{err: errors.New("no PortIndex")},
		"a cycle": requiresLocal{edges: map[string][]string{
			"head": nil, "a": {"b"}, "b": {"a"},
		}},
		"nothing known": requiresLocal{edges: map[string][]string{}},
	} {
		t.Run(name, func(t *testing.T) {
			seated, requires := buildOrder(context.Background(), local, in)
			assert.Equal(t, []string{"head", "a", "b"}, memberNames(seated))
			assert.Nil(t, requires)
		})
	}
}

// A single member has nobody to wait for and costs no lookup.
func TestBuildOrderAsksNothingForOneMember(t *testing.T) {
	seated, requires := buildOrder(context.Background(), requiresLocal{
		err: errors.New("must not be asked"),
	}, roster("only"))
	assert.Equal(t, []string{"only"}, memberNames(seated))
	assert.Nil(t, requires)
}
