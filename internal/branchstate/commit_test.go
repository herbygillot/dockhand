package branchstate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A plan of dockhand's own edits commits on the way to a check where
// nobody looks; one with a person's needs their look, the way on tidy,
// whatever --yes said, and a person asked answered for it (R3-9's one
// reading of consent).
func TestAPlanCommitsUnaskedOnlyAsItsOwn(t *testing.T) {
	own := Plan{Branch: "jq-1.8.2", Groups: []Group{{Directory: "textproc/jq", FromEdits: true}}}
	hand := Plan{Branch: "jq-1.8.2", Groups: []Group{{Directory: "textproc/jq"}}}
	for _, answered := range []model.Answered{model.AnsweredNobody, model.AnsweredYes} {
		mode := Mode{Answered: answered}
		require.True(t, mode.Unattended())
		require.NoError(t, own.CommitUnasked(mode))
		require.EqualError(t, hand.CommitUnasked(mode), "review the commit plan before committing: run dockhand tidy --branch jq-1.8.2")
	}
	asked := Mode{Answered: model.AnsweredAsked}
	require.False(t, asked.Unattended())
	require.Error(t, hand.CommitUnasked(asked), "asked, it still isn't one to commit unasked")
	require.NoError(t, hand.AppliesUnasked(Mode{Answered: model.AnsweredYes}, false), "tidy --yes applies the plan it showed")
}
