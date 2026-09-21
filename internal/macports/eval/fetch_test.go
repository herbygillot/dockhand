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
	semantics := assessFetch(macports.PortInfo{Options: map[string]string{}}, "portfetch::fetch_main", "{"+perl+"}", "", nil)
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
	require.True(t, conditionalRejection(wrapper+`if {[variant_isset a] && (${x} || [variant_exists b]) && ${y} eq {}} { return -code error "no" }`+"\n"), "grouping and braced text are part of the grammar")
	require.False(t, conditionalRejection(wrapper+`if {[variant_isset a] ? 1 : 0} { return -code error "no" }`+"\n"), "a form the parser does not model is refused")
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

// A refusal names the first command or condition the grammar stopped at, so
// the assessment says what shape the hook has rather than only that it is
// unrecognized, and where the hook lives when the worker found it in a file.
func TestRefusalNamesTheCommandTheGrammarStoppedAt(t *testing.T) {
	t.Parallel()
	wrapper := "global {*}[info globals]\n"
	github := macports.PortInfo{Options: map[string]string{"go.domain": "github.com"}}
	plain := macports.PortInfo{Options: map[string]string{}}
	goCheck := wrapper + "global go.toolchain_unmet\nset ceiling [go_toolchain.ceiling]\nif {${ceiling} eq \"none\"} {\n ui_error \"none\"\n return -code error \"unsupported platform\"\n}\nif {${go.toolchain_unmet} ne \"\"} {\n ui_error \"old\"\n return -code error \"unsupported toolchain\"\n}\n"
	for _, test := range []struct {
		name, hook string
		info       macports.PortInfo
		want       string
	}{
		{"error instead of return", wrapper + "\n    ui_error \"unsupported\"\n    error \"${name} needs macOS 11\"\n", plain, "ends with `error \"${name} needs macOS 11\"` rather than return -code error"},
		{"work before rejecting", wrapper + "catch {set result [active_variants R tcltk]}\nreturn -code error no\n", plain, "runs `catch {set result [active_variants R tcltk]}` before rejecting"},
		{"computed message", wrapper + "return -code error [subst text]\n", plain, "returns a computed message `[subst text]`"},
		{"nested if", wrapper + "if {${os.major} < 11} {\n    if {![variant_isset x]} { return -code error no }\n}\n", plain, "has a branch that ends with `if {![variant_isset x]} { return -code error no }` rather than return -code error"},
		{"host read in a condition", wrapper + "if {![file exists /usr/lib/libc++.dylib]} { return -code error no }\n", plain, "calls `file` in its condition: `![file exists /usr/lib/libc++.dylib]`"},
		{"vercmp in a condition", wrapper + "if {[vercmp ${macosx_version} 10.10] < 0} { return -code error no }\n", plain, "calls `vercmp` in its condition: `[vercmp ${macosx_version} 10.10] < 0`"},
		{"computed query argument", wrapper + "if {[variant_isset ${flavor}]} { return -code error no }\n", plain, "calls `variant_isset` with a computed argument in its condition: `[variant_isset ${flavor}]`"},
		{"procedure after the if", wrapper + "if {${a}} { return -code error no }\nmpi.action_enforce_variants ${name}\n", plain, "runs `mpi.action_enforce_variants ${name}` outside an if"},
		{"unbraced condition", wrapper + "if $a { return -code error no }\n", plain, "has a condition that is not braced: `$a`"},
		{"branch doing work", wrapper + "if {${a}} { set distfiles other }\n", plain, "has a branch that ends with `set distfiles other` rather than return -code error"},
		{"go check off github", goCheck, macports.PortInfo{Options: map[string]string{"go.domain": "gitlab.com"}}, "is the Go PortGroup's toolchain check, recognized only when go.domain is github.com; this port's is gitlab.com"},
		{"go check without a domain", goCheck, plain, "is the Go PortGroup's toolchain check, recognized only when go.domain is github.com; this port's is unset"},
		{"altered go check", strings.Replace(goCheck, "[go_toolchain.ceiling]", "[other.check]", 1), github, "differs from the Go PortGroup's toolchain check as dockhand knows it"},
		{"unwrapped", "return -code error", plain, "is not wrapped as a Base hook"},
		{"empty", wrapper + "\n", plain, "is empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			semantics := assessFetch(test.info, "portfetch::fetch_main", "{"+test.hook+"}", "", nil)
			require.Equal(t, "custom", semantics.Kind)
			require.Equal(t, "pre-fetch hook 1 "+test.want, semantics.Problem)
		})
	}
	// The origin places the offending command at its own line in the file:
	// the hook body starts at Portfile line 12, its first line blank, and the
	// error is on the body's third line.
	hook := wrapper + "\n    ui_error \"unsupported\"\n    error \"no\"\n"
	semantics := assessFetch(plain, "portfetch::fetch_main", "{"+hook+"}", "", []hookOrigin{{Label: "Portfile", Line: 12}})
	require.Equal(t, "pre-fetch hook 1 ends with `error \"no\"` rather than return -code error, at Portfile line 13", semantics.Problem)
	semantics = assessFetch(plain, "portfetch::fetch_main", "{"+hook+"}", "", []hookOrigin{{Label: "the java-1.0 PortGroup", Line: 40}})
	require.Equal(t, "pre-fetch hook 1 ends with `error \"no\"` rather than return -code error, in the java-1.0 PortGroup at line 41", semantics.Problem)
	require.Equal(t, []hookOrigin{{Label: "Portfile", Line: 5}, {}, {Label: "the x-1.0 PortGroup", Line: 9}}, parseOrigins("{Portfile 5} {} {{the x-1.0 PortGroup} 9}"))
	// A recognized hook with an origin is a guard as before.
	semantics = assessFetch(plain, "portfetch::fetch_main", "{"+wrapper+"return -code error no\n}", "", []hookOrigin{{Label: "Portfile", Line: 3}})
	require.Equal(t, "guarded", semantics.Kind)
	require.Empty(t, semantics.Problem)
}
