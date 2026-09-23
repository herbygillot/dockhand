package eval

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchMetadataReportsCompatibilityWithoutExposingHookBodies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		details    string
		compatible string
	}{
		{"{portfetch::fetch_main {} {}}", "1"},
		{"{custom_fetch {} {}}", "0"},
		{"{portfetch::fetch_main {{error custom}} {}}", "0"},
		{"{portfetch::fetch_main {} {post_fetch}}", "0"},
	} {
		info, _, err := decodeMetadata("name fixture version 1 revision 0 epoch 0 fetch_details " + test.details)
		require.NoError(t, err)
		require.Equal(t, test.compatible, info.Options["fetch.archive_compatible"])
		require.NotContains(t, info.Options, "fetch_details")
	}
}

// A rejection-only guard decoded through the evaluator preserves the platform
// restriction and never exposes the hook body as an option.
func TestDecodedRejectionOnlyGuardPreservesPlatformRestriction(t *testing.T) {
	t.Parallel()
	safe := "global {*}[info globals]\n" + `ui_error "${subport} is unavailable on this platform"
return -code error "unsupported platform"`
	info, _, err := decodeMetadata("name fixture version 1 revision 0 epoch 0 fetch_details {portfetch::fetch_main {{" + safe + "}} {}}")
	require.NoError(t, err)
	require.Equal(t, "guarded", info.Fetch.Kind)
	require.True(t, info.Fetch.Rejected)
	require.Equal(t, "1", info.Options["fetch.archive_compatible"])
	require.NotContains(t, info.Options, "fetch_details")
}
