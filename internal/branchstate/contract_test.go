package branchstate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
)

// contractCases are the facts and actions the Next contract is held to,
// one for each refusal Evaluate makes; each step of part 4 adds those its
// judgment brings.
var contractCases = func() []struct {
	name   string
	facts  Facts
	action Action
	mode   Mode
} {
	open := model.Branch{Name: "dockhand/jq-1.8.2", State: model.BranchOpen}
	passed := &model.Run{Number: 3, State: model.RunPassed}
	failed := &model.Run{Number: 3, State: model.RunFailed}
	branch := func(commits int, edited ...string) Branch {
		return Branch{Record: open, Commits: commits, Edited: edited}
	}
	stands := &evidence.Evidence{}
	type c = struct {
		name   string
		facts  Facts
		action Action
		mode   Mode
	}
	return []c{
		{"submit, gone", Facts{Branch: Branch{Record: open, Missing: true, Commits: 1}, Check: Check{Latest: passed, Current: true}}, ActionSubmit, Mode{Answered: model.AnsweredAsked}},
		{"submit, pushed", Facts{Branch: Branch{Record: open, Commits: 1, Pushed: true}, Check: Check{Latest: passed, Current: true}}, ActionSubmit, Mode{Answered: model.AnsweredAsked}},
		{"submit, nothing committed", Facts{Branch: branch(0, "Portfile"), Check: Check{Latest: passed, Current: true}}, ActionSubmit, Mode{Answered: model.AnsweredAsked}},
		{"submit, edited", Facts{Branch: branch(1, "Portfile"), Check: Check{Latest: passed, Current: true, Evidence: stands}}, ActionSubmit, Mode{Answered: model.AnsweredAsked}},
		{"submit, checking", Facts{Branch: branch(1), Check: Check{Latest: passed, Active: []model.Run{{Number: 4, State: model.RunRunning}}}}, ActionSubmit, Mode{Answered: model.AnsweredNobody}},
		{"submit, stopped", Facts{Branch: branch(1), Check: Check{Latest: passed, Stopped: &model.Run{Number: 4}}}, ActionSubmit, Mode{Answered: model.AnsweredNobody}},
		{"submit, failed", Facts{Branch: branch(1), Check: Check{Latest: failed, Current: true}}, ActionSubmit, Mode{Answered: model.AnsweredAsked}},
		{"submit, other files", Facts{Branch: branch(1), Check: Check{Latest: passed}}, ActionSubmit, Mode{Answered: model.AnsweredAsked}},
	}
}()

// A refusal's Next moves the branch on: another verb, or the same once
// something named has changed, never the refused action asked again with
// nothing to wait for (architecture, 2026-10-22).
func TestARefusalsNextAdvances(t *testing.T) {
	for _, c := range contractCases {
		verdict := Evaluate(c.facts, c.action, c.mode)
		require.True(t, verdict.Refused, "%s: each case is a refusal", c.name)
		require.NotNil(t, verdict.Next, "%s: a refusal says what moves it on", c.name)
		require.True(t, verdict.Next.Advances(c.action), "%s: %+v asks %s again with nothing to wait for", c.name, *verdict.Next, c.action)
	}
	require.True(t, Next{Verb: "check"}.Advances("submit"))
	require.True(t, Next{Verb: "submit", After: AfterLook}.Advances("submit"))
	require.False(t, Next{Verb: "submit"}.Advances("submit"))
}
