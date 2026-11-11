package engine

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
)

// The reviewer request, one table over the design's two: each reading and
// mark, and each answer. AC: a request that landed, whose answer was lost
// and which someone removed, is not sent again. AD: Alice approving since,
// while Bob's review still sets the verdict, drops Alice and keeps Bob
// (docs/designs/publication-record.md, "The reviewer request").
func TestAReviewerRequestIsSentOnceAtMost(t *testing.T) {
	t.Parallel()
	question := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	status := func(requested ...string) forge.PullRequestStatus {
		return forge.PullRequestStatus{RequestedRead: true, Requested: requested, LatestReview: map[string]time.Time{"bob": question, "alice": question.Add(time.Hour)}}
	}
	reviewer := func(login string, sent model.Sent) model.Reviewer {
		return model.Reviewer{Login: login, Reviewed: question, Sent: sent}
	}
	for _, c := range []struct {
		name     string
		reviewer model.Reviewer
		status   forge.PullRequestStatus
		want     ReviewerAction
	}{
		{"the read failed or was cut short", reviewer("bob", model.NotSent), forge.PullRequestStatus{}, ReviewerUnread},
		{"AD. Alice reviewed since the question", reviewer("alice", model.NotSent), status(), ReviewedSince},
		{"AD. Bob stands, none pending, not sent: sent now", reviewer("bob", model.NotSent), status(), ReviewerSend},
		{"a request is pending, whoever made it", reviewer("bob", model.NotSent), status("bob"), ReviewerPending},
		{"AC. may have been sent, none pending: not sent again", reviewer("bob", model.MaySent), status(), ReviewerMaySent},
		{"may have been sent, and pending", reviewer("bob", model.MaySent), status("bob"), ReviewerPending},
		{"sent, no longer pending", reviewer("bob", model.SentRequest), status(), ReviewerNoLongerPending},
		{"refused", reviewer("bob", model.RefusedToAsk), status(), ReviewerRefused},
	} {
		require.Equal(t, c.want, ReviewerStep(c.reviewer, c.status), c.name)
	}

	for _, c := range []struct {
		name string
		err  error
		want model.Sent
	}{
		{"acknowledged", nil, model.SentRequest},
		{"a rate limit", &forge.RateLimitError{Err: errors.New("limited")}, model.NotSent},
		{"the login is wanting", fmt.Errorf("asking: %w", forge.ErrAuthentication), model.NotSent},
		{"refused otherwise", fmt.Errorf("%w: reviewers must be collaborators", forge.ErrRejected), model.RefusedToAsk},
		{"no answer, or a server error", errors.New("connection reset"), model.MaySent},
	} {
		sent, why := ReviewerAnswered(c.err)
		require.Equal(t, c.want, sent, c.name)
		require.Equal(t, c.want == model.RefusedToAsk, why != "", c.name)
	}
}
