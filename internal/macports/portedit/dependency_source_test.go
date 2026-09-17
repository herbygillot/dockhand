package portedit

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/stretchr/testify/require"
)

func TestManifestSourceAmbiguityAndAbsence(t *testing.T) {
	t.Parallel()
	archive := func(name, member string) Download {
		filename := filepath.Join(t.TempDir(), name)
		f, err := os.Create(filename)
		require.NoError(t, err)
		writer := tar.NewWriter(f)
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: member, Mode: 0600, Size: 4}))
		_, err = writer.Write([]byte("lock"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		require.NoError(t, f.Close())
		return Download{Name: name, path: filename}
	}
	info := macports.PortInfo{Options: map[string]string{"worksrcdir": "root", "extract.rename": "no"}}
	sources := []archiveSource{{Name: "source.tar"}, {Name: "auxiliary.tar"}}
	downloads := []Download{archive("source.tar", "root/Cargo.lock"), archive("auxiliary.tar", "root/Cargo.lock")}
	_, err := selectDependencySource(t.Context(), info, sources, downloads, &dependency.Plan{Kind: dependency.Cargo})
	require.ErrorContains(t, err, "multiple extracted archives")
	downloads[1] = archive("auxiliary.tar", "other/Cargo.lock")
	selected, err := selectDependencySource(t.Context(), info, sources, downloads, &dependency.Plan{Kind: dependency.Cargo})
	require.NoError(t, err)
	require.Equal(t, downloads[0].path, selected.Archive)
	downloads[0] = archive("source.tar", "root/README")
	_, err = selectDependencySource(t.Context(), info, sources, downloads, &dependency.Plan{Kind: dependency.Cargo})
	require.ErrorContains(t, err, "no extracted archive contains")
}
