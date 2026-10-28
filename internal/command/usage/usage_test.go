package usage

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	guide "github.com/herbygillot/dockhand/docs/usage"
	"github.com/herbygillot/dockhand/internal/term"
)

// Every topic usage lists prints its page from the binary, and every page
// docs/usage keeps is a topic's, so none is in the repository alone.
func TestEveryTopicPrintsItsPage(t *testing.T) {
	t.Parallel()
	pages, err := fs.Glob(guide.Pages, "*.md")
	require.NoError(t, err)
	var listed []string
	for _, topic := range Topics {
		listed = append(listed, topic.Page)
		var out bytes.Buffer
		cmd := Command(term.Streams{Out: &out})
		if topic.Name != "" {
			cmd.SetArgs([]string{topic.Name})
		} else {
			cmd.SetArgs([]string{})
		}
		require.NoError(t, cmd.Execute(), topic.Name)
		require.True(t, strings.HasPrefix(out.String(), "# "), "%s prints its page", topic.Page)
		if topic.Name != "" {
			require.Contains(t, cmd.Long, "  "+topic.Name+" ", "usage's help lists it")
		}
	}
	require.ElementsMatch(t, pages, listed)
}

func TestAnUnknownTopicNamesTheTopics(t *testing.T) {
	t.Parallel()
	_, err := Page("hand-edit")
	require.EqualError(t, err, "no usage topic hand-edit: the topics are hand-edits, unattended, checks, review, batches, new-port, and agents")
}
