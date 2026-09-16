package distfiles

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestManifestCandidatesUseNativeExtraction(t *testing.T) {
	info := macports.PortInfo{Options: map[string]string{"extract.only": "source.tar.gz", "extract.rename": "no"}}
	got, err := ManifestCandidates(info, []string{"pinned-v8.gz", "source.tar.gz"})
	require.NoError(t, err)
	require.Equal(t, []string{"source.tar.gz"}, got)
	info.Options["extract.only"] = "missing.tar.gz"
	_, err = ManifestCandidates(info, []string{"source.tar.gz"})
	require.ErrorContains(t, err, "no source archive")
	info.Options["extract.only"] = "source.tar.gz pinned-v8.gz"
	info.Options["extract.rename"] = "yes"
	_, err = ManifestCandidates(info, []string{"source.tar.gz", "pinned-v8.gz"})
	require.ErrorContains(t, err, "multiple extracted sources")
}
