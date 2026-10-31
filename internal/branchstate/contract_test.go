package branchstate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A refusal's Next moves the branch on: another verb, or the same once
// something named has changed, never the refused action asked again with
// nothing to wait for (architecture, 2026-10-22). Publish's refusals of
// what blocks a submission, of a branch that moved while it was checked,
// whose way on is a check of where it moved, and of what holds it where
// nobody looks are each held to it, and so is the commit plan's gate on the way to a
// submission, whose way on is tidy.
func TestARefusalsNextAdvances(t *testing.T) {
	held := model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "license-changed", Detail: "upstream's COPYING changed"}
	commit := model.ObjectID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	asked, nobody := Mode{Answered: model.AnsweredAsked}, Mode{Answered: model.AnsweredNobody}
	cases := []struct {
		name        string
		publication Publication
		mode        Mode
	}{
		{"blocked", Publication{Branch: "jq-1.8.2", Blocks: []Block{{Kind: BlockNoTitle}}, Commit: commit}, asked},
		{"moved", Publication{Branch: "jq-1.8.2", Commit: commit, Bound: model.ObjectID("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}, asked},
		{"held", Publication{Branch: "jq-1.8.2", Holds: []model.Concern{held}, Commit: commit}, nobody},
	}
	for _, c := range cases {
		refused := Publish(c.publication, c.mode, words{})
		require.NotNil(t, refused, "%s: each case is a refusal", c.name)
		require.NotEmpty(t, refused.Next, "%s: a refusal says what moves it on", c.name)
		require.True(t, refused.Advances(string(VerbSubmit)), "%s: %+v asks submit again with nothing to wait for", c.name, refused.Next)
		require.True(t, refused.Advances(string(VerbTidy)), "%s: %+v", c.name, refused.Next)
	}

	// The commit plan's gate says its way on in words, tidy, where it
	// isn't dockhand's own edits and nobody looks.
	plan := Plan{Branch: "jq-1.8.2", Groups: []Group{{Directory: "textproc/jq", FromEdits: true, Notes: []string{"a hand edit"}}}}
	require.ErrorContains(t, plan.CommitUnasked(nobody), "run dockhand tidy --branch jq-1.8.2")
	plan.Groups[0].Notes = nil
	require.NoError(t, plan.CommitUnasked(nobody), "dockhand's own edits are committed unasked")

	require.True(t, Next{Verb: "check"}.Advances("submit"))
	require.True(t, Next{Verb: "submit", After: AfterLook}.Advances("submit"))
	require.False(t, Next{Verb: "submit"}.Advances("submit"))
}
