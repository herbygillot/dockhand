package engine

import (
	"errors"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
)

// ReviewerAction is what a publication's reviewer step does for one login
// once the pull request is read (docs/designs/publication-record.md, "The
// reviewer request"). Its promise is that each reviewer named has a
// request pending, or dockhand says who doesn't and why; never that a
// notification was delivered.
type ReviewerAction string

const (
	// ReviewerUnread is a read that failed or came back cut short:
	// nothing is sent, and the step stays owed.
	ReviewerUnread ReviewerAction = "unread"
	// ReviewedSince is a login whose own latest review is newer than the
	// one the answer was for: dropped, and said.
	ReviewedSince ReviewerAction = "reviewed-since"
	// ReviewerPending is a request already pending, whoever made it: the
	// step's promise is kept, and nothing is sent.
	ReviewerPending ReviewerAction = "pending"
	// ReviewerSend is no request pending and none sent: the mark "may have
	// been sent" is recorded, then the request is sent.
	ReviewerSend ReviewerAction = "send"
	// ReviewerMaySent is no request pending after a request that may have
	// been sent: never sent again.
	ReviewerMaySent ReviewerAction = "may-have-been-sent"
	// ReviewerNoLongerPending is a request GitHub acknowledged that is no
	// longer pending: nothing is sent, and the mark stays.
	ReviewerNoLongerPending ReviewerAction = "no-longer-pending"
	// ReviewerRefused is a request GitHub refused: not asked again by this
	// publication, its words kept.
	ReviewerRefused ReviewerAction = "refused"
)

// ReviewerStep decides one login's request from the pull request's status
// and the login's mark. Three histories look the same once a mark is
// recorded, the process dying before the call, the call never arriving,
// and the call landing and its request then being removed, so a request
// that may have been sent is never sent again; only a login with no mark
// is sent to.
func ReviewerStep(reviewer model.Reviewer, status forge.PullRequestStatus) ReviewerAction {
	if !status.RequestedRead {
		return ReviewerUnread
	}
	if latest, ok := status.LatestReview[reviewer.Login]; ok && latest.After(reviewer.Reviewed) {
		return ReviewedSince
	}
	for _, login := range status.Requested {
		if login == reviewer.Login {
			return ReviewerPending
		}
	}
	switch reviewer.Sent {
	case model.MaySent:
		return ReviewerMaySent
	case model.SentRequest:
		return ReviewerNoLongerPending
	case model.RefusedToAsk:
		return ReviewerRefused
	}
	return ReviewerSend
}

// ReviewerAnswered is the mark a request's own outcome leaves: sent where
// GitHub acknowledged it; not sent where a rate limit or a missing login
// stopped it, for the next run; refused, with GitHub's words, where GitHub
// refused it otherwise; and "may have been sent" where no answer came, or
// a server error did.
func ReviewerAnswered(err error) (model.Sent, string) {
	var limited *forge.RateLimitError
	switch {
	case err == nil:
		return model.SentRequest, ""
	case errors.As(err, &limited), errors.Is(err, forge.ErrAuthentication):
		return model.NotSent, ""
	case errors.Is(err, forge.ErrRejected):
		return model.RefusedToAsk, err.Error()
	}
	return model.MaySent, ""
}
