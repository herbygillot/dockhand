package branchstate

import (
	"slices"
	"testing"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

const (
	hereScope  = "2.12.7\x1f8.6.17\x1fdarwin 25 arm64\x1f-\x1f25.1.0\x1f26.1\x1f26.1.0\x1ffoo\x1f-"
	thereScope = "2.12.7\x1f8.6.17\x1fdarwin 24 arm64\x1f-\x1f24.6.0\x1f16.4\x1f16.4.0\x1ffoo\x1f-"
)

func updateEdit(port string, patches *model.PatchCheck) model.Edit {
	return model.Edit{Kind: model.EditUpdate, Port: port, Result: &model.EditResult{Patches: patches}}
}

func list(scope string, patches ...model.PatchResult) *model.PatchCheck {
	return &model.PatchCheck{Scope: scope, Declared: append([]model.PatchResult{}, patches...)}
}

func patch(name string, state model.PatchState) model.PatchResult {
	return model.PatchResult{Name: name, State: state}
}

// The verdict's answers in order, each with every reason, and whether the
// edits are committed (#194, P1 to P4, P9, P10).
func TestPreparation(t *testing.T) {
	t.Parallel()
	rejected := updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchRejected)))
	clean := updateEdit("bar", list(hereScope))
	request := &model.ChangeRequest{ID: "r1", Origin: "serve", Targets: []model.RequestTarget{
		{Port: "foo", Action: "update"}, {Port: "bar", Action: "update"}, {Port: "baz", Action: "update", Current: "3.0"}}}
	none := func(string) []string { return nil }

	// D: three ports, one with a patch rejected, in every order: held,
	// by its own port's edit, the edits committed.
	for _, edits := range [][]model.Edit{{rejected, clean}, {clean, rejected}} {
		verdict := PreparationOf(Readying{Edits: edits, Request: request, Allowed: none})
		require.Equal(t, Readied{Disposition: HeldBeforeCheck, Reasons: []string{"foo: patch fix.diff doesn't apply"}, Committed: true}, verdict)
	}
	// AB: a revision bump, a move followed alone, or a port found current
	// after the held update leaves it held.
	revbump := model.Edit{Kind: model.EditRevbump, Port: "foo", Result: &model.EditResult{}}
	followed := updateEdit("foo", nil)
	verdict := PreparationOf(Readying{Edits: []model.Edit{rejected, clean, revbump, followed}, Request: request, Allowed: none})
	require.Equal(t, HeldBeforeCheck, verdict.Disposition)

	for _, test := range []struct {
		name string
		in   Readying
		want Readied
	}{
		{"a person attends", Readying{Edits: []model.Edit{rejected}, Attended: true, Allowed: none}, Readied{Disposition: ReadyToCheck, Committed: true}},
		{"holds.allow names it now", Readying{Edits: []model.Edit{rejected}, Allowed: func(port string) []string {
			return map[string][]string{"foo": {"patch-rejected"}}[port]
		}}, Readied{Disposition: ReadyToCheck, Committed: true}},
		{"and taken away again", Readying{Edits: []model.Edit{rejected}, Allowed: none}, Readied{Disposition: HeldBeforeCheck, Reasons: []string{"foo: patch fix.diff doesn't apply"}, Committed: true}},
		{"dockhand's edits not committed come before held", Readying{Edits: []model.Edit{rejected}, Explained: []string{"devel/foo/Portfile"}, Allowed: none},
			Readied{Disposition: UnfinishedPreparation, Reasons: []string{"dockhand's edits of devel/foo/Portfile aren't committed"}}},
		{"a person's change comes first", Readying{Edits: []model.Edit{rejected}, Explained: []string{"devel/foo/Portfile"}, Others: []string{"devel/foo/files/mine.diff"}, Allowed: none},
			Readied{Disposition: HandEditsToCarry, Reasons: []string{"devel/foo/files/mine.diff"}}},
		{"an unwritten edit with a file it can't settle over", Readying{Unwritten: []UnwrittenEdit{{Edit: model.Edit{Port: "foo"}, Changed: []string{"devel/foo/Portfile"}}}},
			Readied{Disposition: HandEditsToCarry, Reasons: []string{"devel/foo/Portfile changed since foo's update was prepared"}, Committed: true, Owed: []string{"an update of foo wasn't finished"}}},
		{"a person's change keeps what's owed beside it (#335)", Readying{Edits: []model.Edit{rejected}, Request: request, Others: []string{"devel/foo/files/mine.diff"}, Allowed: none},
			Readied{Disposition: HandEditsToCarry, Reasons: []string{"devel/foo/files/mine.diff"}, Committed: true, Owed: []string{"bar has no update yet"}}},
		{"an unwritten edit", Readying{Unwritten: []UnwrittenEdit{{Edit: model.Edit{Port: "foo"}}}},
			Readied{Disposition: UnfinishedPreparation, Reasons: []string{"an update of foo wasn't finished"}, Committed: true, Pending: true}},
		{"a target whose update failed is tried again (E)", Readying{Edits: []model.Edit{rejected}, Request: request, Allowed: none},
			Readied{Disposition: UnfinishedPreparation, Reasons: []string{"bar has no update yet"}, Committed: true, Pending: true}},
		{"a refusal blocks (AX)", Readying{Edits: []model.Edit{clean}, Request: &model.ChangeRequest{ID: "r2", Targets: []model.RequestTarget{
			{Port: "bar", Action: "update", Outcomes: []model.ActionOutcome{{Action: "version", Result: model.ActionDone}, {Action: "source", Result: model.ActionRefused, Why: "--source tarball: bar has no tarball"}}}}}},
			Readied{Disposition: UnfinishedPreparation, Reasons: []string{"source of bar was refused: --source tarball: bar has no tarball"}, Committed: true, Pending: true}},
		{"a port found current and a clean sibling are ready", Readying{Edits: []model.Edit{clean}, Request: &model.ChangeRequest{ID: "r3", Targets: []model.RequestTarget{
			{Port: "bar", Action: "update"}, {Port: "baz", Action: "update", Current: "3.0"}}}}, Readied{Disposition: ReadyToCheck, Committed: true}},
	} {
		require.Equal(t, test.want, PreparationOf(test.in), test.name)
	}
}

// A rejection stands from the edit that recorded it until a later update
// of the port, with a list in the same scope, records the patch applied
// or doesn't declare it; a nil list, an unchecked patch, or another scope
// changes nothing; the last patch removed ends it, an unreadable list
// doesn't; and one from before stands (#194, P5, P7, P8).
func TestARejectionStandsUntilTheSameScopeSaysOtherwise(t *testing.T) {
	t.Parallel()
	recorded := updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchRejected), patch("b.diff", model.PatchApplied)))
	standing := []Rejection{{Patch: "fix.diff", Scope: hereScope}}
	for _, test := range []struct {
		name  string
		later model.Edit
		want  []Rejection
	}{
		{"applied in the same scope", updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchApplied))), nil},
		{"no longer declared there", updateEdit("foo", list(hereScope, patch("b.diff", model.PatchApplied))), nil},
		{"the last patch removed", updateEdit("foo", list(hereScope)), nil},
		{"rejected again", updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchRejected))), standing},
		{"left unchecked", updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchUnchecked))), standing},
		{"no list read", updateEdit("foo", nil), standing},
		{"applied on another system", updateEdit("foo", list(thereScope, patch("fix.diff", model.PatchApplied))), standing},
		{"read with a part unknown", updateEdit("foo", list(model.PatchScope("2.12.7", ""), patch("fix.diff", model.PatchApplied))), standing},
		{"another port's edit", updateEdit("bar", list(hereScope)), standing},
	} {
		require.Equal(t, test.want, StandingRejections("foo", []model.Edit{recorded, test.later}), test.name)
	}
	unknown := updateEdit("foo", list(model.PatchScope("2.12.7", ""), patch("fix.diff", model.PatchRejected)))
	require.Equal(t, []Rejection{{Patch: "fix.diff", Scope: unknown.Result.Patches.Scope}},
		StandingRejections("foo", []model.Edit{unknown, updateEdit("foo", list(unknown.Result.Patches.Scope))}), "a scope with an unknown part compares with none, itself included")

	before := model.Edit{Kind: model.EditUpdate, Port: "foo", Result: &model.EditResult{Findings: []model.Finding{{Kind: model.FindingPatchRejected, Detail: []string{"fix.diff: Hunk #1 FAILED"}}}}}
	require.Equal(t, []Rejection{{}}, StandingRejections("foo", []model.Edit{before, updateEdit("foo", list(hereScope))}), "a rejection from before stands until the person ends it")
}

// dockhand's own drop of a merged patch ends its rejection in the drop's
// own scope alone (#194, P6, AP).
func TestADroppedPatchEndsItsRejectionInItsOwnScope(t *testing.T) {
	t.Parallel()
	recorded := updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchRejected)))
	dropped := func(scope string) model.Edit {
		edit := updateEdit("foo", list(scope))
		edit.Result.Findings = []model.Finding{{Kind: model.FindingPatchDropped, Subject: "fix.diff", Fixed: true}}
		return edit
	}
	require.Empty(t, StandingRejections("foo", []model.Edit{recorded, dropped(hereScope)}))
	require.Equal(t, []Rejection{{Patch: "fix.diff", Scope: hereScope}}, StandingRejections("foo", []model.Edit{recorded, dropped(thereScope)}))
	// A conditional declaration of the file left standing is declared in
	// the final evaluation's list where its condition holds, and so ends
	// nothing there.
	conditional := updateEdit("foo", list(hereScope, patch("fix.diff", model.PatchRejected)))
	require.True(t, slices.Equal([]Rejection{{Patch: "fix.diff", Scope: hereScope}}, StandingRejections("foo", []model.Edit{recorded, conditional})))
}

// The developer tools are recorded with a scope and not compared (amended
// 2026-11-22): a rejection recorded under one Xcode is ended by a later
// update under the same Darwin and another Xcode that applies the patch,
// as it is where the earlier evaluation had read no tools at all.
func TestARejectionEndsUnderAnotherXcodeOnTheSameDarwin(t *testing.T) {
	t.Parallel()
	scope := func(darwin string, tools string) string {
		return model.PatchScope("2.12.7", "8.6.17", "darwin 25 arm64", "-", darwin, tools, "foo", "-")
	}
	for name, before := range map[string]string{
		"Xcode 26.1":     model.ScopeTools("26.1", "26.1.0.0.1"),
		"tools not read": model.ScopeTools("(not read)", "(not read)"),
	} {
		recorded := updateEdit("foo", list(scope("25.1.0", before), patch("fix.diff", model.PatchRejected)))
		fixed := updateEdit("foo", list(scope("25.1.0", model.ScopeTools("26.2", "26.2.0.0.1")), patch("fix.diff", model.PatchApplied)))
		require.Empty(t, StandingRejections("foo", []model.Edit{recorded, fixed}), name)
		elsewhere := updateEdit("foo", list(scope("25.2.0", model.ScopeTools("26.2", "26.2.0.0.1")), patch("fix.diff", model.PatchApplied)))
		require.NotEmpty(t, StandingRejections("foo", []model.Edit{recorded, elsewhere}), "%s: another Darwin still ends nothing", name)
	}
}
