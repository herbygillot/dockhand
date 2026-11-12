package portedit

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
)

func TestManifestSourceAmbiguityAndAbsence(t *testing.T) {
	t.Parallel()
	archive := func(name, member string) distfetch.Download {
		filename := filepath.Join(t.TempDir(), name)
		f, err := os.Create(filename)
		require.NoError(t, err)
		writer := tar.NewWriter(f)
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: member, Mode: 0600, Size: 4}))
		_, err = writer.Write([]byte("lock"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		require.NoError(t, f.Close())
		return distfetch.Download{Path: filename, Checksum: portfile.Checksum{Name: name}}
	}
	info := macports.PortInfo{Options: map[string]string{"worksrcdir": "root", "extract.rename": "no"}}
	sources := []distfetch.Source{{Name: "source.tar"}, {Name: "auxiliary.tar"}}
	downloads := []distfetch.Download{archive("source.tar", "root/Cargo.lock"), archive("auxiliary.tar", "root/Cargo.lock")}
	_, err := selectDependencySource(t.Context(), info, sources, downloads, &depblock.Plan{Kind: depblock.Cargo})
	require.ErrorContains(t, err, "multiple extracted archives")
	downloads[1] = archive("auxiliary.tar", "other/Cargo.lock")
	selected, err := selectDependencySource(t.Context(), info, sources, downloads, &depblock.Plan{Kind: depblock.Cargo})
	require.NoError(t, err)
	require.Equal(t, downloads[0].Path, selected.Archive)
	downloads[0] = archive("source.tar", "root/README")
	_, err = selectDependencySource(t.Context(), info, sources, downloads, &depblock.Plan{Kind: depblock.Cargo})
	// The auxiliary archive's lock is under another directory, which
	// nothing points the build at.
	require.ErrorContains(t, err, "no Cargo.lock is at worksrcdir root or above it, where the build reads one; the source holds other/Cargo.lock")

	// A Cargo source with no lock the build reads says what it holds
	// instead (#229): none, and where its Cargo.toml are, or locks only
	// below worksrcdir, each named, none picked.
	members := func(name string, names ...string) distfetch.Download {
		filename := filepath.Join(t.TempDir(), name)
		f, err := os.Create(filename)
		require.NoError(t, err)
		writer := tar.NewWriter(f)
		for _, member := range names {
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: member, Mode: 0600, Size: 4, Typeflag: tar.TypeReg}))
			_, err = writer.Write([]byte("data"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		require.NoError(t, f.Close())
		return distfetch.Download{Path: filename, Checksum: portfile.Checksum{Name: name}}
	}
	sources = sources[:1]
	_, err = selectDependencySource(t.Context(), info, sources, []distfetch.Download{members("source.tar", "root/sub/cli/Cargo.toml", "root/README")}, &depblock.Plan{Kind: depblock.Cargo})
	require.ErrorContains(t, err, "ships no Cargo.lock")
	require.ErrorContains(t, err, "root/sub/cli")
	require.NotContains(t, err.Error(), "at worksrcdir")
	require.ErrorIs(t, err, macports.ErrManifestMissing)
	_, err = selectDependencySource(t.Context(), info, sources, []distfetch.Download{members("source.tar", "root/sub/cli/Cargo.lock")}, &depblock.Plan{Kind: depblock.Cargo})
	require.ErrorContains(t, err, "root/sub/cli/Cargo.lock")
	require.ErrorContains(t, err, "nothing in the Portfile points the build at one")
	_, err = selectDependencySource(t.Context(), info, sources, []distfetch.Download{members("source.tar", "root/a/Cargo.lock", "root/b/Cargo.lock")}, &depblock.Plan{Kind: depblock.Cargo})
	require.ErrorContains(t, err, "root/a/Cargo.lock, root/b/Cargo.lock")
}
