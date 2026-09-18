package eval

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoToolchainCheckDoesNotAdmitOtherFetchBehavior(t *testing.T) {
	t.Parallel()
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

func TestRejectionOnlyFetchGuardPreservesPlatformRestriction(t *testing.T) {
	t.Parallel()
	wrapper := "global {*}[info globals]\n"
	safe := wrapper + `ui_error "${subport} is unavailable on this platform"
return -code error "unsupported platform"`
	require.True(t, rejectionOnly(safe))
	for _, body := range []string{
		wrapper + `set master_sites https://other.invalid; return -code error`,
		wrapper + `ui_error "[exec touch file]"; return -code error`,
		wrapper + `ui_error "$array([exec touch file])"; return -code error`,
		wrapper + `ui_error {*}$messages; return -code error`,
		wrapper + `if {$os.major < 23} {return -code error}`,
		safe + "\nset distfiles other",
		`return -code error`,
	} {
		require.False(t, rejectionOnly(body), body)
	}
	info, _, err := decodeMetadata("name fixture version 1 revision 0 epoch 0 fetch_details {portfetch::fetch_main {{" + safe + "}} {}}")
	require.NoError(t, err)
	require.Equal(t, "guarded", info.Fetch.Kind)
	require.True(t, info.Fetch.Rejected)
	require.Equal(t, "1", info.Options["fetch.archive_compatible"])
	require.NotContains(t, info.Options, "fetch_details")
}

func TestConditionalRejectionGuardsAreRecognized(t *testing.T) {
	t.Parallel()
	wrapper := "global {*}[info globals]\n"
	perl := wrapper + `if {${perl5.variant} eq {} && ${perl5.require_variant}} {
    ui_error "${name} requires one of these variants: ${perl5.variants}"
    return -code error "absence of required perl variant"
}
`
	require.True(t, conditionalRejection(perl))
	require.False(t, rejectionOnly(perl), "a conditional guard is not an unconditional rejection")
	semantics := assessFetch(macports.PortInfo{Options: map[string]string{}}, "portfetch::fetch_main", "{"+perl+"}", "")
	require.Empty(t, semantics.Problem)
	require.Equal(t, "guarded", semantics.Kind)
	require.False(t, semantics.Rejected)
	require.Contains(t, semantics.Guards, "pre-fetch hook 1 only rejects unsupported configurations")
	elseForm := wrapper + `if {${a}} { return -code error "no" } elseif {${b}} { ui_error x; return -code error "no" } else { return -code error "never" }` + "\n"
	require.True(t, conditionalRejection(elseForm))
	fortran := wrapper + `if {${compilers.require_fortran} && [fortran_variant_name] eq ""} {
    return -code error "must set at least one Fortran variant (${compilers.my_fortran_variants})"
}
`
	require.True(t, conditionalRejection(fortran), "the compilers PortGroup's guard queries variants and changes nothing")
	require.True(t, conditionalRejection(wrapper+`if {![variant_isset gfortran] && [variant_exists gcc15]} { return -code error "no" }`+"\n"))
	for _, body := range []string{
		wrapper + `if {[exec uname] eq "Darwin"} { return -code error "no" }` + "\n",
		wrapper + `if {[variant_isset $which]} { return -code error "no" }` + "\n",
		wrapper + `if {[variant_isset [lindex $x 0]]} { return -code error "no" }` + "\n",
		wrapper + `if {[fortran_variant_name] eq "" ]} { return -code error "no" }` + "\n",
		wrapper + `if {${a}} { set fetch.type git; return -code error "no" }` + "\n",
		wrapper + `if {${a}} { ui_error x } else { distfiles other.tar.gz }` + "\n",
		wrapper + `if {${a}} { return -code error "no" }; set x 1` + "\n",
		wrapper + `return -code error "no"` + "\n",
		"if {${a}} { return -code error \"no\" }\n",
	} {
		require.False(t, conditionalRejection(body), body)
	}
}
