package prepare_test

import (
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestGeneratedCommitMessageKeepsReasonAndOneTrailer(t *testing.T) {
	for _, body := range []string{"", "Rebuild dependents", "Rebuild dependents\n\n" + prepare.GeneratedBy + "\n" + prepare.GeneratedBy} {
		intent := prepare.CommitIntent{Subject: "fixture: revbump", Body: body}
		message := intent.Message()
		require.Equal(t, 1, strings.Count(message, prepare.GeneratedBy))
		require.True(t, strings.HasSuffix(message, "\n\n"+prepare.GeneratedBy+"\n"))
		require.True(t, strings.HasPrefix(message, "fixture: revbump\n\n"))
		require.Equal(t, message, intent.Message())
		if body != "" {
			require.Contains(t, message, "fixture: revbump\n\nRebuild dependents\n\n"+prepare.GeneratedBy)
		}
	}
}
