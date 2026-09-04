package cmd

// The build-time constant, and the layer that sits under every path to
// the forge.
//
// Every assertion here is that something is REFUSED or that the
// permission is absent. Nothing here proves an unattended publication
// works, because on this build there is no such thing.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// THE RULING: the machine may not publish on this build.
//
// One assertion, and it is the point of the whole step. When the trust
// ladder is ruled on and the constant flips, this line is what says so
// out loud — the flip cannot be a silent one-character diff nobody
// reviewed.
func TestThisBuildDoesNotLetAMachinePublish(t *testing.T) {
	assert.False(t, machinePublishEnabled,
		"ring 3 is unspendable by any machine on this build; flipping this is the trust ladder's ruling to make")
}

// The constant reaches the engine through the run, and the run is the
// only thing that grants it. A Context nobody granted anything hands the
// engine false, which is the refusal.
func TestThePermissionTravelsAsAValueAndDefaultsToRefusing(t *testing.T) {
	ungranted := &runstate.Context{}
	assert.False(t, ungranted.MachinePublish, "a run that granted nothing grants nothing")
	require.Error(t, engine.GateRing3(record.Machine, ungranted.MachinePublish))

	// And the composition root spends the constant into it. Built
	// through newRoot so the wiring under test is the wiring that ships.
	_, rc := newRoot("test")
	assert.Equal(t, machinePublishEnabled, rc.MachinePublish,
		"the composition root spends the constant, and spends it here only")
	require.Error(t, engine.GateRing3(record.Machine, rc.MachinePublish),
		"which means the shipped wiring refuses")
}

// THE RULING: the gh seam refuses a machine's WRITE underneath every
// path that could assemble one.
//
// This is the layer that holds when an engine author writes a new
// publisher without reading the gate. It is asserted over the runner the
// composition root actually wires, with the inner runner failing the
// test if a refused call reaches it.
func TestTheGhSeamRefusesAMachinesWrites(t *testing.T) {
	ctx := context.Background()
	reached := false
	inner := func(context.Context, ...string) (string, error) {
		reached = true
		return "ok", nil
	}
	rc := &runstate.Context{Invoker: record.Machine, MachinePublish: machinePublishEnabled}
	guarded := guardForgeWrites(rc, inner)

	writes := [][]string{
		{"pr", "create", "--repo", "macports/macports-ports", "--head", "someone:dockhand/jq-1.8"},
		{"pr", "edit", "7", "--repo", "macports/macports-ports"},
		{"pr", "comment", "7", "--body", "ping"},
		{"pr", "merge", "7"},
		{"pr", "close", "7"},
		{"issue", "comment", "7", "--body", "ping"},
		{"api", "-X", "POST", "repos/macports/macports-ports/pulls"},
		{"api", "--method", "PATCH", "repos/macports/macports-ports/pulls/7"},
		{"api", "--method=DELETE", "repos/macports/macports-ports/pulls/7"},
		{"api", "-XPUT", "repos/macports/macports-ports/pulls/7/merge"},
	}
	for _, args := range writes {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			reached = false
			out, err := guarded(ctx, args...)
			require.Error(t, err)
			assert.Empty(t, out)
			var refusal *engine.MachineDisabledError
			require.ErrorAs(t, err, &refusal)
			assert.Equal(t, exitcode.MachineGate, refusal.DockhandExit())
			assert.False(t, reached, "the refusal must come before gh is executed")
		})
	}
}

// Reading spends nothing of ring 3, so the guard must not touch it. A
// guard that refused reads would stop `status` and `clean` working
// unattended, which is the sweep's whole job.
func TestTheGhSeamLetsAMachineREAD(t *testing.T) {
	ctx := context.Background()
	inner := func(context.Context, ...string) (string, error) { return "ok", nil }
	rc := &runstate.Context{Invoker: record.Machine, MachinePublish: machinePublishEnabled}
	guarded := guardForgeWrites(rc, inner)

	reads := [][]string{
		{"api", "-X", "GET", "search/issues"},
		{"api", "repos/macports/macports-ports/pulls?state=open"},
		{"api", "user"},
		{"pr", "view", "7"},
		{"pr", "list"},
	}
	for _, args := range reads {
		out, err := guarded(ctx, args...)
		require.NoError(t, err, "%v is a read and spends nothing", args)
		assert.Equal(t, "ok", out)
	}
}

// A person passes the guard for everything. The gate is about the
// machine, and a wrapper that refused a maintainer's own promote would
// have broken the verb it exists to protect.
func TestTheGhSeamNeverRefusesAPerson(t *testing.T) {
	ctx := context.Background()
	inner := func(context.Context, ...string) (string, error) { return "ok", nil }
	for _, by := range []record.Driver{record.Human, ""} {
		rc := &runstate.Context{Invoker: by, MachinePublish: machinePublishEnabled}
		out, err := guardForgeWrites(rc, inner)(ctx, "pr", "create", "--repo", "macports/macports-ports")
		require.NoError(t, err, "invoker %q must not be refused", by)
		assert.Equal(t, "ok", out)
	}
}

// The invoker is read at CALL time, not captured. It is resolved in
// PersistentPreRunE, after the composition root builds the runner, so a
// snapshot taken at wiring time would be the empty string forever — and
// the empty string is a person.
func TestTheGuardReadsTheInvokerWhenItIsAsked(t *testing.T) {
	ctx := context.Background()
	inner := func(context.Context, ...string) (string, error) { return "ok", nil }
	rc := &runstate.Context{MachinePublish: machinePublishEnabled}
	guarded := guardForgeWrites(rc, inner)

	_, err := guarded(ctx, "pr", "create", "--repo", "x/y")
	require.NoError(t, err, "unset at wiring time, which is a person")

	rc.Invoker = record.Machine
	_, err = guarded(ctx, "pr", "create", "--repo", "x/y")
	var refusal *engine.MachineDisabledError
	require.ErrorAs(t, err, &refusal, "the declaration made later is the one that counts")
}

// The guard's own reading of an argv, stated apart from the run so the
// table is readable. The `gh api` forms are where a hole would hide: gh
// takes the method three ways and a guard that knew two of them would be
// a guard with a spelling for a bypass.
func TestTheGuardRecognizesEveryWayToSpellAWrite(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "create"},
		{"pr", "edit", "7"},
		{"pr", "lock", "7"},
		{"pr", "unlock", "7"},
		{"repo", "edit", "--default-branch", "main"},
		{"api", "-X", "post", "repos/x/y/pulls"},
		{"api", "--method", "Patch", "repos/x/y/pulls/7"},
		{"api", "--method=put", "repos/x/y/pulls/7"},
		{"api", "-XDELETE", "repos/x/y/pulls/7"},
		// THE HOLE THIS TABLE WAS MISSING. gh defaults to GET and switches
		// to POST the moment any parameter is present, so this argv opens a
		// pull request with no method anywhere on it.
		{"api", "repos/x/y/pulls", "-f", "title=jq: update", "-f", "head=someone:dockhand/jq-1.8", "-f", "base=master"},
		{"api", "repos/x/y/pulls", "-F", "draft=false"},
		{"api", "repos/x/y/pulls", "--field", "title=jq"},
		{"api", "repos/x/y/pulls", "--raw-field", "title=jq"},
		{"api", "repos/x/y/pulls", "--input", "-"},
		{"api", "repos/x/y/pulls", "--field=title=jq"},
		{"api", "repos/x/y/pulls", "-ftitle=jq"},
		// graphql is a write whatever it says: a query and a mutation are
		// the same argv shape to a guard reading flags.
		{"api", "graphql", "-f", "query=mutation{addComment(input:{}){clientMutationId}}"},
		{"api", "graphql", "-f", "query=query{viewer{login}}"},
	} {
		assert.True(t, isForgeWrite(args), "%v writes", args)
	}
	for _, args := range [][]string{
		nil,
		{"api"},
		{"pr"},
		{"api", "-X", "GET", "search/issues"},
		{"api", "--method=HEAD", "repos/x/y"},
		// A parameterized READ: gh's own way to spell one, and the only
		// shape that can prove a body flag is not a write.
		{"api", "-X", "GET", "search/issues", "-f", "q=repo:x/y"},
		{"api", "--method=get", "repos/x/y/pulls", "--field", "state=open"},
		// The reads this tree actually makes.
		{"api", "repos/x/y/pulls?state=open&per_page=100&page=1"},
		{"api", "repos/x/y/pulls?head=someone:dockhand/jq-1.8&state=all"},
		{"api", "user", "-q", ".login"},
		{"api", "repos/x/y/releases?per_page=100", "--include"},
		{"pr", "view", "7"},
		{"repo", "view"},
	} {
		assert.False(t, isForgeWrite(args), "%v does not write", args)
	}
}

// Every gh READ this tree assembles passes the guard, asserted over the
// argvs the shipped code builds rather than over invented ones.
//
// The guard was widened to read `gh api` as a write whenever a parameter
// is present, which is the correct default and is also the change most
// likely to refuse a read by accident: a pass whose duplicate check or
// fork lookup was refused would stop reporting, unattended, with a
// machine-gate error about a question.
func TestTheGuardLetsEveryReadThisTreeMakesThrough(t *testing.T) {
	ctx := context.Background()
	inner := func(context.Context, ...string) (string, error) { return "ok", nil }
	rc := &runstate.Context{Invoker: record.Machine, MachinePublish: machinePublishEnabled}
	guarded := guardForgeWrites(rc, inner)

	for _, args := range [][]string{
		// gh.ForkRemote, and the sweep's maintainer:me half.
		{"api", "user", "-q", ".login"},
		// gh.QueryPR.
		{"api", "repos/macports/macports-ports/pulls?head=someone:dockhand/jq-1.8&state=all"},
		// gh.OpenPortPRs, the REST walk that replaced the search.
		{"api", "repos/macports/macports-ports/pulls?state=open&per_page=100&page=1"},
		// upstream's release check.
		{"api", "repos/x/y/releases?per_page=100", "--include"},
	} {
		out, err := guarded(ctx, args...)
		require.NoError(t, err, "%v is a read the tree makes on every pass", args)
		assert.Equal(t, "ok", out)
	}
}

// A refused write does not become a gh failure the caller has to read
// prose to classify: it comes back as the machine gate's own typed
// refusal, wrapped by nothing.
func TestARefusedWriteIsTypedAndNotAGhFailure(t *testing.T) {
	rc := &runstate.Context{Invoker: record.Machine, MachinePublish: machinePublishEnabled}
	inner := func(context.Context, ...string) (string, error) {
		return "", errors.New("gh pr: some other failure")
	}
	_, err := guardForgeWrites(rc, inner)(context.Background(), "pr", "create")
	require.Error(t, err)
	assert.Equal(t, exitcode.MachineGate, ExitCode(err),
		"the exit code is the gate's, not the child's")
}
