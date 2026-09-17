package dependency

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestManifestOwnershipHonorsExtractionDirectory(t *testing.T) {
	t.Parallel()
	archive := sourceArchive(t, map[string]string{"other/Cargo.lock": "lock", "actual/subdir/Cargo.lock": "nested"})
	require.NoError(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "actual/subdir"}, false))
	require.ErrorIs(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "missing/subdir"}, false), ErrManifestMissing)
	require.NoError(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "renamed/subdir"}, true))
	require.Error(t, ConfirmSource(t.Context(), Cargo, Input{Archive: archive, Worksrcdir: "../actual"}, false))
}

// The Go PortGroup's worksrcdir names where post-extract moves the source,
// gopath/src/<go.package>; the archive keeps its manifest under its own
// top-level directory, and that is where it is found.
func TestGOPATHWorksrcdirFindsTheManifestUnderTheArchiveTopDirectory(t *testing.T) {
	t.Parallel()
	archive := sourceArchive(t, map[string]string{"uni-2.10.0/go.mod": "module zgo.at/uni/v2\n", "uni-2.10.0/cmd/go.mod": "module zgo.at/uni/v2/cmd\n"})
	in := Input{Archive: archive, Worksrcdir: "gopath/src/zgo.at/uni/v2", Package: "zgo.at/uni/v2"}
	require.NoError(t, ConfirmSource(t.Context(), Go, in, false))
	data, member, err := Manifest(t.Context(), archive, in.Worksrcdir, "go.mod")
	require.NoError(t, err)
	require.Equal(t, "uni-2.10.0/go.mod", member)
	require.Equal(t, "module zgo.at/uni/v2\n", string(data))
	_, _, err = Manifest(t.Context(), archive, in.Worksrcdir, "go.work")
	require.ErrorIs(t, err, ErrManifestMissing)

	two := sourceArchive(t, map[string]string{"a-1/go.mod": "module a\n", "b-1/go.mod": "module b\n"})
	_, _, err = Manifest(t.Context(), two, in.Worksrcdir, "go.mod")
	require.Error(t, err, "two top-level manifests are ambiguous")
	require.False(t, GOPATHLayout("uni-2.10.0"))
	require.True(t, GOPATHLayout("gopath/src/github.com/cli/cli/v2"))
}
