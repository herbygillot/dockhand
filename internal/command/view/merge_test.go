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

// A request GitHub acknowledged that it no longer shows pending is said
// so, as the design's reviewer table has it: they were asked, and the
// request is no longer pending (#191; Codex's review of group D).
func TestTheReviewReceiptSaysARequestNoLongerPending(t *testing.T) {
	t.Parallel()
	pr := &model.PullRequest{Rerequested: &model.ReviewReceipt{Reviewers: []model.Reviewer{
		{Login: "ana", Sent: model.SentRequest},
		{Login: "bo", Sent: model.SentRequest, Lapsed: true},
	}}}
	require.Equal(t, "asked @ana to review again; asked @bo to review again, and the request is no longer pending", ReviewReceiptWords(pr))
}
