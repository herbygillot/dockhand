package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The name GitHub answered a listing as is read from the URLs it gave, a
// tag's zipball or a release's page; one that names the repository asked
// is no answer of another's.
func TestTheNameAListingAnsweredAsIsRead(t *testing.T) {
	t.Parallel()
	r := &repository{name: "acorn-io/acorn"}
	require.Empty(t, r.AnswersAs())
	r.answer("https://api.github.com/repos/obot-platform/obot/zipball/refs/tags/v0.27.0")
	require.Equal(t, "obot-platform/obot", r.AnswersAs())
	r.answer("https://github.com/someone/else/releases/tag/v1")
	require.Equal(t, "obot-platform/obot", r.AnswersAs(), "the first answer stands")

	same := &repository{name: "jqlang/jq"}
	same.answer("https://github.com/JQLang/jq/releases/tag/jq-1.8.1")
	require.Empty(t, same.AnswersAs(), "the same name, however cased")
	same.answer("not a url")
	require.Empty(t, same.AnswersAs())
}
