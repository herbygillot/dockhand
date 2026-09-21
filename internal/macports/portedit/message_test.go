package portedit_test

import (
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/record"
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

func TestComposeCitesReferencesOnceAheadOfTheAttribution(t *testing.T) {
	t.Parallel()
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}
	see := record.Reference{Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/74422"}
	intent := portedit.CommitIntent{Subject: "fixture: revbump for simdutf update", Body: "Why it matters\n\nSee: https://trac.macports.org/ticket/74422\n" + portedit.GeneratedBy(), References: []record.Reference{closes, see, closes}}
	require.Equal(t, "fixture: revbump for simdutf update\n\nWhy it matters\n\nSee: https://trac.macports.org/ticket/74422\nCloses: https://trac.macports.org/ticket/74379\n"+portedit.GeneratedBy()+"\n", intent.Message())
	bare := portedit.CommitIntent{Subject: "fixture: update to 2", References: []record.Reference{closes}}
	require.Equal(t, "fixture: update to 2\n\nCloses: https://trac.macports.org/ticket/74379\n"+portedit.GeneratedBy()+"\n", bare.Message())
}

func TestRewriteTouchesOnlyTheSubjectAndTheTrailers(t *testing.T) {
	t.Parallel()
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}
	generated := "fixture: update to 2\n\nDetails\n\n" + portedit.GeneratedBy() + "\n"
	require.Equal(t, "fixture: update to 2.1\n\nDetails\n\nCloses: https://trac.macports.org/ticket/74379\n"+portedit.GeneratedBy(), portedit.Rewrite(generated, "fixture: update to 2.1", []record.Reference{closes}))
	require.Equal(t, "fixture: update to 2\n\nDetails\n\n"+portedit.GeneratedBy(), portedit.Rewrite(generated, "", nil), "nothing asked, nothing changed")
	human := "fixture: fix build\n\nSigned-off-by: Someone <someone@example.invalid>\nCloses: https://trac.macports.org/ticket/74379"
	require.Equal(t, human+"\nSee: https://trac.macports.org/ticket/1", portedit.Rewrite(human, "", []record.Reference{closes, {Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/1"}}), "a cited ticket is not cited twice; a new one joins the final paragraph")
	require.Equal(t, "fixture: fix build\n\nCloses: https://trac.macports.org/ticket/74379", portedit.Rewrite("fixture: fix build", "", []record.Reference{closes}), "a subject-only message gains a trailer paragraph")
}

func TestSubjectSuppliesThePortNameOnce(t *testing.T) {
	t.Parallel()
	subject, err := portedit.Subject("py-foo", " revbump for simdutf update ")
	require.NoError(t, err)
	require.Equal(t, "py-foo: revbump for simdutf update", subject)
	_, err = portedit.Subject("py-foo", "py-foo: revbump")
	require.ErrorContains(t, err, "already begins with")
	_, err = portedit.Subject("py-foo", " ")
	require.ErrorContains(t, err, "one nonempty line")
	_, err = portedit.Subject("py-foo", "two\nlines")
	require.ErrorContains(t, err, "one nonempty line")
}
