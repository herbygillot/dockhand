package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetadataDecodesEachDictionaryValueOnlyOnce(t *testing.T) {
	port, _, err := decodeMetadata(`name fixture version 1.2 revision 0 epoch 0 description {{literal braces}} livecheck.regex {{v(\d+)\.tar}} github.tag_suffix {{}} option_errors {livecheck.url {failure with \backslashes}}`)
	require.NoError(t, err)
	require.Equal(t, "{literal braces}", port.Options["description"])
	require.Equal(t, `{v(\d+)\.tar}`, port.Options["livecheck.regex"])
	require.Equal(t, "{}", port.Options["github.tag_suffix"])
	require.Equal(t, `failure with \backslashes`, port.OptionErrors["livecheck.url"])
}
