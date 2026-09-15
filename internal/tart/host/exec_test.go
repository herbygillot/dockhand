package host

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/stretchr/testify/require"
)

func TestGuestCommandsCloseInheritedDescriptorsAndPreserveInputAndArguments(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "tart")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
set -eu
[ "$1" = exec ]
shift
[ "$1" = -i ]
shift 2
exec 9>/dev/null
exec "$@"
`), 0700))
	n := Machine{Client: tart.Client{Home: root, Executable: executable}}
	var output bytes.Buffer
	argument := "spaces; $(do-not-execute) 'literal'"
	_, err := n.Exec(t.Context(), "vm", tart.RunOptions{Input: strings.NewReader("payload\n"), Output: &output}, "/bin/sh", "-c", `
[ ! -e /dev/fd/9 ] || exit 42
read -r input
printf '%s\n%s\n' "$input" "$1"
`, "check", argument)
	require.NoError(t, err)
	require.Equal(t, "payload\n"+argument+"\n", output.String())
}
