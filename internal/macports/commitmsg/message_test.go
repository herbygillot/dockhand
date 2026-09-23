package commitmsg_test

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestRewriteTouchesOnlyTheSubjectAndTheTrailers(t *testing.T) {
	t.Parallel()
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}
	generated := "fixture: update to 2\n\nDetails\n\n" + commitmsg.GeneratedBy() + "\n"
	require.Equal(t, "fixture: update to 2.1\n\nDetails\n\nCloses: https://trac.macports.org/ticket/74379\n"+commitmsg.GeneratedBy(), commitmsg.Rewrite(generated, "fixture: update to 2.1", []record.Reference{closes}))
	require.Equal(t, "fixture: update to 2\n\nDetails\n\n"+commitmsg.GeneratedBy(), commitmsg.Rewrite(generated, "", nil), "nothing asked, nothing changed")
	human := "fixture: fix build\n\nSigned-off-by: Someone <someone@example.invalid>\nCloses: https://trac.macports.org/ticket/74379"
	require.Equal(t, human+"\nSee: https://trac.macports.org/ticket/1", commitmsg.Rewrite(human, "", []record.Reference{closes, {Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/1"}}), "a cited ticket is not cited twice; a new one joins the final paragraph")
	require.Equal(t, "fixture: fix build\n\nCloses: https://trac.macports.org/ticket/74379", commitmsg.Rewrite("fixture: fix build", "", []record.Reference{closes}), "a subject-only message gains a trailer paragraph")
}

func TestSubjectSuppliesThePortNameOnce(t *testing.T) {
	t.Parallel()
	subject, err := commitmsg.Subject("py-foo", " revbump for simdutf update ")
	require.NoError(t, err)
	require.Equal(t, "py-foo: revbump for simdutf update", subject)
	_, err = commitmsg.Subject("py-foo", "py-foo: revbump")
	require.ErrorContains(t, err, "already begins with")
	_, err = commitmsg.Subject("py-foo", " ")
	require.ErrorContains(t, err, "one nonempty line")
	_, err = commitmsg.Subject("py-foo", "two\nlines")
	require.ErrorContains(t, err, "one nonempty line")
}

func TestComposeKeepsTheReasonAndOneAttribution(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "Rebuild dependents", "Rebuild dependents\n\n" + commitmsg.GeneratedBy() + "\n" + commitmsg.GeneratedBy()} {
		message := commitmsg.Compose("fixture: revbump", body, nil)
		require.Equal(t, 1, strings.Count(message, commitmsg.GeneratedBy()))
		require.True(t, strings.HasSuffix(message, "\n\n"+commitmsg.GeneratedBy()+"\n"))
		require.True(t, strings.HasPrefix(message, "fixture: revbump\n\n"))
		require.Equal(t, message, commitmsg.Compose("fixture: revbump", body, nil))
		if body != "" {
			require.Contains(t, message, "fixture: revbump\n\nRebuild dependents\n\n"+commitmsg.GeneratedBy())
		}
	}
}

func TestComposeCitesReferencesOnceAheadOfTheAttribution(t *testing.T) {
	t.Parallel()
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}
	see := record.Reference{Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/74422"}
	message := commitmsg.Compose("fixture: revbump for simdutf update", "Why it matters\n\nSee: https://trac.macports.org/ticket/74422\n"+commitmsg.GeneratedBy(), []record.Reference{closes, see, closes})
	require.Equal(t, "fixture: revbump for simdutf update\n\nWhy it matters\n\nSee: https://trac.macports.org/ticket/74422\nCloses: https://trac.macports.org/ticket/74379\n"+commitmsg.GeneratedBy()+"\n", message)
	require.Equal(t, "fixture: update to 2\n\nCloses: https://trac.macports.org/ticket/74379\n"+commitmsg.GeneratedBy()+"\n", commitmsg.Compose("fixture: update to 2", "", []record.Reference{closes}))
}
