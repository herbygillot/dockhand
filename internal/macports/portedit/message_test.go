package portedit_test

import (
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestGeneratedCommitMessageKeepsReasonAndOneTrailer(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "Rebuild dependents", "Rebuild dependents\n\n" + portedit.GeneratedBy() + "\n" + portedit.GeneratedBy()} {
		intent := portedit.CommitIntent{Subject: "fixture: revbump", Body: body}
		message := intent.Message()
		require.Equal(t, 1, strings.Count(message, portedit.GeneratedBy()))
		require.True(t, strings.HasSuffix(message, "\n\n"+portedit.GeneratedBy()+"\n"))
		require.True(t, strings.HasPrefix(message, "fixture: revbump\n\n"))
		require.Equal(t, message, intent.Message())
		if body != "" {
			require.Contains(t, message, "fixture: revbump\n\nRebuild dependents\n\n"+portedit.GeneratedBy())
		}
	}
}
