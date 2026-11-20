package forge

import (
	"slices"
	"strings"
	"time"
)

// CommentKind distinguishes a human, a bot, and an unidentifiable author.
type CommentKind string

const (
	CommentUser    CommentKind = "user"
	CommentBot     CommentKind = "bot"
	CommentUnknown CommentKind = "unknown"
)

// Comment is the author and creation time of a conversation comment.
type Comment struct {
	Login string
	Kind  CommentKind
	At    time.Time
}

// AwaitingResponse finds the newest human word, ignoring bots. complete says
// whether older comments were read; an unseen older page cannot change a
// decision already made by a newer human (#248).
func AwaitingResponse(comments []Comment, account string, complete bool) (by string, at time.Time, settled bool) {
	if account == "" {
		return "", time.Time{}, false
	}
	comments = slices.Clone(comments)
	for _, c := range comments {
		if c.Kind != CommentBot && c.At.IsZero() {
			return "", time.Time{}, false
		}
	}
	// Readers supply comments oldest first; later entries win timestamp ties.
	slices.Reverse(comments)
	slices.SortStableFunc(comments, func(a, b Comment) int { return b.At.Compare(a.At) })
	for _, c := range comments {
		switch c.Kind {
		case CommentBot:
			continue
		case CommentUser:
			if c.Login == "" {
				return "", time.Time{}, false
			}
			if strings.EqualFold(c.Login, account) {
				return "", time.Time{}, true
			}
			return c.Login, c.At.UTC(), true
		default:
			return "", time.Time{}, false
		}
	}
	return "", time.Time{}, complete
}
