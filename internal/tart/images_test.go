package tart

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestImagesNormalizesBothRunningRepresentations(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, `#!/bin/sh
[ "$*" = "list --format json" ] || exit 2
printf '%s' '[{"Name":"one","Source":"local","Running":true},{"Name":"two","Source":"local","State":"running"},{"Name":"base","Source":"oci","State":"stopped"}]'
`)
	images, err := (Client{Executable: executable}).Images(t.Context(), RunOptions{})
	require.NoError(t, err)
	require.Equal(t, []Image{{Name: "one", Source: "local", Running: true}, {Name: "two", Source: "local", Running: true}, {Name: "base", Source: "oci"}}, images)
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\necho invalid\n")
	_, err = (Client{Executable: executable}).Images(t.Context(), RunOptions{})
	require.ErrorContains(t, err, "invalid image listing")
}
