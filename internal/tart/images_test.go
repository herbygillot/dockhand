package tart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImagesNormalizesBothRunningRepresentations(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
[ "$*" = "list --format json" ] || exit 2
printf '%s' '[{"Name":"one","Source":"local","Running":true},{"Name":"two","Source":"local","State":"running"},{"Name":"base","Source":"oci","State":"stopped"}]'
`), 0700))
	images, err := (Client{Executable: executable}).Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "one", Source: "local", Running: true}, {Name: "two", Source: "local", Running: true}, {Name: "base", Source: "oci"}}, images)
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\necho invalid\n"), 0700))
	_, err = (Client{Executable: executable}).Images(t.Context(), RunOptions{})
	require.ErrorContains(t, err, "invalid image listing")
}
