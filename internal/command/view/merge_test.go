package view

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The receipt says what became of each reviewer: asked, refused with
// GitHub's own words, and not asked for having reviewed since the
// question was put (#191, group D).
func TestTheReviewReceiptSaysEachReviewersOutcome(t *testing.T) {
	t.Parallel()
	pr := &model.PullRequest{Rerequested: &model.ReviewReceipt{Reviewers: []model.Reviewer{
		{Login: "ana", Sent: model.SentRequest},
		{Login: "bo", Sent: model.RefusedToAsk, Why: "not a collaborator"},
		{Login: "cy", Sent: model.ReviewedAgain},
		{Login: "di", Sent: model.ReviewedAgain},
	}}}
	require.Equal(t, "asked @ana to review again; GitHub refused to ask @bo (not a collaborator); @cy and @di reviewed since, so weren't asked again", ReviewReceiptWords(pr))
}
