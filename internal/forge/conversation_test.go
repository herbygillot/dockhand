package forge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAwaitingResponse(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	reviewer := Comment{Login: "reviewer", Kind: CommentUser, At: at}
	tests := []struct {
		name, account string
		comments      []Comment
		complete      bool
		by            string
		at            time.Time
		settled       bool
	}{
		{"bot last", "ada", []Comment{reviewer, {Login: "bot", Kind: CommentBot, At: at.Add(time.Minute)}}, true, "reviewer", at, true},
		{"own comment last", "Ada", []Comment{reviewer, {Login: "ada", Kind: CommentUser, At: at.Add(time.Minute)}}, true, "", time.Time{}, true},
		{"unknown author newest", "ada", []Comment{reviewer, {Kind: CommentUnknown, At: at.Add(time.Minute)}}, true, "", time.Time{}, false},
		{"older page not read", "ada", []Comment{{Kind: CommentBot, At: at}}, false, "", time.Time{}, false},
		{"no account", "", []Comment{reviewer}, true, "", time.Time{}, false},
		{"own reply at same second", "ada", []Comment{reviewer, {Login: "ada", Kind: CommentUser, At: at}}, true, "", time.Time{}, true},
		{"decisive human with older page missing", "ada", []Comment{reviewer}, false, "reviewer", at, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			by, when, settled := AwaitingResponse(tt.comments, tt.account, tt.complete)
			require.Equal(t, tt.by, by)
			require.Equal(t, tt.at, when)
			require.Equal(t, tt.settled, settled)
		})
	}
}
