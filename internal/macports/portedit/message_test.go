package portedit_test

import (
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestGeneratedCommitMessageKeepsReasonAndOneTrailer(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "Rebuild dependents", "Rebuild dependents\n\n" + commitmsg.GeneratedBy() + "\n" + commitmsg.GeneratedBy()} {
		intent := portedit.CommitIntent{Subject: "fixture: revbump", Body: body}
		message := intent.Message()
		require.Equal(t, 1, strings.Count(message, commitmsg.GeneratedBy()))
		require.True(t, strings.HasSuffix(message, "\n\n"+commitmsg.GeneratedBy()+"\n"))
		require.True(t, strings.HasPrefix(message, "fixture: revbump\n\n"))
		require.Equal(t, message, intent.Message())
		if body != "" {
			require.Contains(t, message, "fixture: revbump\n\nRebuild dependents\n\n"+commitmsg.GeneratedBy())
		}
	}
}

func TestComposeCitesReferencesOnceAheadOfTheAttribution(t *testing.T) {
	t.Parallel()
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}
	see := record.Reference{Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/74422"}
	intent := portedit.CommitIntent{Subject: "fixture: revbump for simdutf update", Body: "Why it matters\n\nSee: https://trac.macports.org/ticket/74422\n" + commitmsg.GeneratedBy(), References: []record.Reference{closes, see, closes}}
	require.Equal(t, "fixture: revbump for simdutf update\n\nWhy it matters\n\nSee: https://trac.macports.org/ticket/74422\nCloses: https://trac.macports.org/ticket/74379\n"+commitmsg.GeneratedBy()+"\n", intent.Message())
	bare := portedit.CommitIntent{Subject: "fixture: update to 2", References: []record.Reference{closes}}
	require.Equal(t, "fixture: update to 2\n\nCloses: https://trac.macports.org/ticket/74379\n"+commitmsg.GeneratedBy()+"\n", bare.Message())
}
