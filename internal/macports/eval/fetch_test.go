package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoToolchainCheckDoesNotAdmitOtherFetchBehavior(t *testing.T) {
	check := `global {*}[info globals]
# A compatibility gate supplied by a PortGroup.
global go.toolchain_unmet
set ceiling [go_toolchain.ceiling]
if {${ceiling} eq "none"} {
 ui_error "No supported toolchain for ${subport}"
 return -code error "unsupported platform"
}
if {${go.toolchain_unmet} ne ""} {
 ui_error "Needs Go ${go.toolchain_unmet}; newest is ${ceiling}"
 return -code error "unsupported toolchain"
}
`
	require.True(t, goToolchainCheck(check))
	for _, unsupported := range []string{
		check + "system {curl example.invalid}\n",
		strings.Replace(check, "[go_toolchain.ceiling]", "[other.check]", 1),
		strings.Replace(check, "ui_error \"No supported", "ui_error \"[exec curl example.invalid] No supported", 1),
		strings.Replace(check, "${subport}", "$array([exec curl example.invalid])", 1),
		strings.Replace(check, `return -code error "unsupported platform"`, `set master_sites https://example.invalid`, 1),
		strings.Replace(check, `${ceiling} eq "none"`, `1`, 1),
		check + "post-fetch {}\n",
		"global {*}[info globals]\nerror custom\n",
	} {
		require.False(t, goToolchainCheck(unsupported), unsupported)
	}
}

func TestFetchMetadataReportsCompatibilityWithoutExposingHookBodies(t *testing.T) {
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
