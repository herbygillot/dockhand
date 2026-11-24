package branchstate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// One gate for every submission: what blocks it stops it; a branch that
// moved from the commit it was bound to stops it; what holds it stops it
// only where nobody looks, and a person's own goes on, having shown it
// (R3-10).
func TestOneGateForEverySubmission(t *testing.T) {
	held := model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "license-changed", Detail: "upstream's COPYING changed"}
	p := Publication{Branch: "jq-1.8.2", Holds: []model.Concern{held}, Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	asked, nobody := Mode{Answered: model.AnsweredAsked}, Mode{Answered: model.AnsweredNobody}
	require.Nil(t, Publish(p, asked, words{}))
	waits := Publish(p, nobody, words{})
	require.Equal(t, refusal.Attention, waits.Kind())
	require.Equal(t, "jq-1.8.2 needs review before submission: upstream's COPYING changed", waits.Error())
	require.True(t, waits.Advances(string(VerbSubmit)), "submit again, after a look")

	p.Bound = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.Contains(t, Publish(p, asked, words{}).Error(), "moved to aaaaaaa while it was being checked")
	p.Blocks = []Block{{Kind: BlockNoTitle}}
	blocked := Publish(p, asked, words{})
	require.Equal(t, refusal.Failed, blocked.Kind())
	require.Contains(t, blocked.Error(), "can't submit yet: the pull request needs a title")
}

func TestUncheckedBuildHoldUsesDecisionAsItsNext(t *testing.T) {
	hold := model.Concern{Origin: model.FromUpstream, Port: "jq", Rule: "patch-rejected", Detail: "patch.diff doesn't apply"}
	p := Publication{Branch: "jq-1.8.2", Blocks: []Block{{Kind: BlockNoCheckBuildHold, Holds: []model.Concern{hold}}}}
	refused := Publish(p, Mode{Answered: model.AnsweredAsked}, words{})
	require.Contains(t, refused.Error(), "the build fails")
	require.Contains(t, refused.Error(), "patch-rejected")
	require.Len(t, refused.Next, 2, "decide and holds.allow are the two ways on")
	require.Equal(t, Next{Verb: VerbDecide, Branch: "jq-1.8.2", Args: []string{"patch-rejected", "jq", "--because", `"…"`}}, refused.Next[0])
	require.Contains(t, refused.Next[1].External, "holds.allow")
	require.NotEqual(t, AfterEdit, refused.Next[0].After)
}
