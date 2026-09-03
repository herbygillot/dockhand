package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
	"github.com/herbygillot/dockhand/internal/plan"
)

// The same tail as finish_test.go, over a scripted oracle instead of a
// live one.
//
// What Finish decides is a function of two snapshots, and nothing about
// deciding it needs MacPorts: the edits are applied for real, the
// shadow is a real directory copy, and only the answer "here is what
// that Portfile evaluates to" is supplied. Holding the guard order
// behind port-tclsh means the machines that skip — every CI job whose
// promise is nothing but Go — verify none of it, and a reordered guard
// ships green.
//
// What is deliberately NOT proved here is that a real Portfile really
// evaluates to these values. That is eval's job and the intents'
// end-to-end tests', and scripting it would be the test agreeing with
// itself.

const oracleSrc = "PortSystem 1.0\nname foo\nversion 1.0\nrevision 0\n"

func scriptedKey(subport string) info.SubportKey { return info.SubportKey{Subport: subport} }

// scripted stages the Portfile above and returns a handle whose oracle
// answers the real portdir with before and the shadow with after.
func scripted(t *testing.T, before, after info.Snapshot) (port.Handle, []byte) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(oracleSrc), 0o644))
	return porttest.Handle(porttest.Shadowed(dir, before, after), dir), []byte(oracleSrc)
}

// versionEdit rewrites the literal 1.0 on the version line.
func versionEdit(src []byte, to string) edit.Edit {
	start := len("PortSystem 1.0\nname foo\nversion ")
	return edit.Edit{Start: start, End: start + len("1.0"), Old: "1.0", New: to, Reason: "version"}
}

func oracleOpts(before info.Snapshot) FinishOpts {
	return FinishOpts{
		Before:    before,
		Vals:      info.Values{Name: "foo", Version: "1.0"},
		MayChange: map[info.Field]bool{info.FieldVersion: true},
	}
}

func oracleIdentity() Identity {
	return Identity{Intent: "bump", Slug: "foo-2.0", Summary: "foo: update to 2.0"}
}

// The whole chain, with nothing installed: apply, shadow, snapshot,
// diff, guard, assemble.
func TestFinishRunsTheWholeTailOverAScriptedOracle(t *testing.T) {
	before := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "1.0"}}
	after := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "2.0"}}
	h, src := scripted(t, before, after)

	p, err := Finish(t.Context(), h, src, []edit.Edit{versionEdit(src, "2.0")},
		oracleIdentity(), oracleOpts(before))
	require.NoError(t, err)
	assert.Equal(t, "bump", p.Intent)
	assert.Equal(t, "foo", p.Port)
	assert.Equal(t, edit.FileSHA256(src), p.PortfileSHA256, "the hash pledges the source the edits were computed against")
	require.Len(t, p.Edits, 1)
	assert.Equal(t, "2.0", p.Edits[0].New)
	require.Len(t, p.Predicted, 1)
}

// A field the intent did not claim moved with the one it did. The
// version arriving does not excuse the revision moving: a plan states
// what will change, and applying it is held to that statement.
func TestScriptedFinishRefusesAFieldOutsideTheIntentsClaim(t *testing.T) {
	before := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "1.0", Revision: "0"}}
	after := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "2.0", Revision: "3"}}
	h, src := scripted(t, before, after)

	_, err := Finish(t.Context(), h, src, []edit.Edit{versionEdit(src, "2.0")},
		oracleIdentity(), oracleOpts(before))
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
	assert.Equal(t, "foo: revision", d.Detail, "the offender is named, and named the same way every run")
}

// A context appearing is asked about before anything else, because a
// Portfile whose structure moved makes every later question meaningless
// — including the intent's own.
func TestScriptedFinishRefusesAContextThatAppeared(t *testing.T) {
	before := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "1.0"}}
	after := info.Snapshot{
		scriptedKey("foo"):     {Name: "foo", Version: "2.0"},
		scriptedKey("foo-sub"): {Name: "foo-sub", Version: "2.0"},
	}
	h, src := scripted(t, before, after)

	opts := oracleOpts(before)
	opts.Accept = func(info.Delta) error {
		t.Error("the intent judged a delta whose contexts had moved")
		return nil
	}
	_, err := Finish(t.Context(), h, src, []edit.Edit{versionEdit(src, "2.0")}, oracleIdentity(), opts)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.SubportsChanged, d.Type)
	assert.Equal(t, "1 added, 0 removed", d.Detail)
}

// The isolation proof: a checksum edit that landed in a set variable is
// only trustworthy while no sibling moved with it, because the carrier
// is then ambiguous and the plan cannot say which context it is
// speaking for.
func TestScriptedFinishRefusesASetVariableThatCarriesASibling(t *testing.T) {
	before := info.Snapshot{
		scriptedKey("foo"):   {Name: "foo", Version: "1.0"},
		scriptedKey("other"): {Name: "other", Version: "1.0"},
	}
	after := info.Snapshot{
		scriptedKey("foo"):   {Name: "foo", Version: "2.0"},
		scriptedKey("other"): {Name: "other", Version: "2.0"},
	}
	h, src := scripted(t, before, after)

	opts := oracleOpts(before)
	opts.ViaSet = true
	opts.Accept = func(info.Delta) error {
		t.Error("the intent judged edits whose carrier was ambiguous")
		return nil
	}
	_, err := Finish(t.Context(), h, src, []edit.Edit{versionEdit(src, "2.0")}, oracleIdentity(), opts)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
	assert.Contains(t, d.Detail, "the carrier is ambiguous")
}

// Both structural guards violated at once, which is the other ordering
// that changed when the three tails became one: at the copies, the
// via-set sibling proof ran first and this delta was reported as an
// ambiguous carrier. Structure outranks it now — a Portfile whose
// contexts appeared or vanished is not a Portfile the set-variable
// question can be asked about — and this pins that, so the two cannot
// be swapped back unnoticed.
func TestScriptedFinishReportsTheStructuralFailureFirst(t *testing.T) {
	before := info.Snapshot{
		scriptedKey("foo"):   {Name: "foo", Version: "1.0"},
		scriptedKey("other"): {Name: "other", Version: "1.0"},
	}
	after := info.Snapshot{
		scriptedKey("foo"):     {Name: "foo", Version: "2.0"},
		scriptedKey("other"):   {Name: "other", Version: "2.0"},
		scriptedKey("foo-sub"): {Name: "foo-sub", Version: "2.0"},
	}
	h, src := scripted(t, before, after)

	opts := oracleOpts(before)
	opts.ViaSet = true
	opts.Accept = func(info.Delta) error {
		t.Error("the intent judged a delta whose contexts had moved")
		return nil
	}
	_, err := Finish(t.Context(), h, src, []edit.Edit{versionEdit(src, "2.0")}, oracleIdentity(), opts)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.SubportsChanged, d.Type,
		"a sibling also moved under a set-variable edit; the structural answer is the one that comes out")
	assert.Equal(t, "1 added, 0 removed", d.Detail)
}

// The intent's own reason outranks the generic sweep when both apply,
// which is the guard order that changed when the three tails became
// one. Scripted, so the ordering is checked wherever the suite runs.
func TestScriptedFinishPrefersTheIntentsReason(t *testing.T) {
	before := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "1.0", Revision: "0"}}
	after := info.Snapshot{scriptedKey("foo"): {Name: "foo", Version: "1.5", Revision: "3"}}
	h, src := scripted(t, before, after)

	opts := oracleOpts(before)
	opts.Accept = func(info.Delta) error {
		return &plan.Decline{Type: plan.TargetNotReached, Detail: "foo would not become 2.0"}
	}
	_, err := Finish(t.Context(), h, src, []edit.Edit{versionEdit(src, "2.0")}, oracleIdentity(), opts)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.TargetNotReached, d.Type, "the revision also moved; the intent's reason is the informative one")
}
