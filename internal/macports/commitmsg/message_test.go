package commitmsg_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/stretchr/testify/require"
)

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

// Dockhand's attribution is known in its current form and in the forms
// earlier builds wrote, so tidy and the pull-request body find it once.
// Another tool's trailer is the person's own.
func TestIsAttributionKnowsEveryFormDockhandWrote(t *testing.T) {
	t.Parallel()
	for _, line := range []string{commitmsg.GeneratedBy(), "  " + commitmsg.GeneratedBy(), "Assisted-By: Dockhand v0.1.0", "Generated-by: dockhand"} {
		require.True(t, commitmsg.IsAttribution(line), line)
	}
	for _, line := range []string{"Assisted-By: Claude Code", "Closes: https://trac.macports.org/ticket/74379", "fixture: update to 2"} {
		require.False(t, commitmsg.IsAttribution(line), line)
	}
}
