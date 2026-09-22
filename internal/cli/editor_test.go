package cli

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestEditMessageRunsTheEditorAndDropsComments(t *testing.T) {
	script := filepath.Join(t.TempDir(), "editor.sh")
	testsupport.WriteExecutable(t, script, "#!/bin/sh\nprintf 'jq: update to 1.8.1\\n\\nEdited by the person.\\n# a comment they left\\n' > \"$1\"\n")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
	message, err := editMessage(t.Context(), "jq: update to 1.8.1\n")
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1\n\nEdited by the person.\n", message)

	empty := filepath.Join(t.TempDir(), "empty.sh")
	testsupport.WriteExecutable(t, empty, "#!/bin/sh\nprintf '# nothing\\n' > \"$1\"\n")
	t.Setenv("EDITOR", empty)
	_, err = editMessage(t.Context(), "jq: update to 1.8.1\n")
	require.ErrorContains(t, err, "edited message is empty")
}
