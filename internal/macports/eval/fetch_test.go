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
	require.True(t, rejectionOnly(wrapper+"set note \"$name is unavailable\"; ui_msg $note; return -code error \"unsupported platform\""), "a plain variable and a message change nothing the fetch reads")
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
	semantics := assessFetch(macports.PortInfo{Options: map[string]string{}}, "portfetch::fetch_main", "{"+perl+"}", "", nil, nil)
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
		wrapper + `if {${a}} { return -code error "no" }; set distfiles other` + "\n",
		"if {${a}} { return -code error \"no\" }\n",
	} {
		require.False(t, conditionalRejection(body), body)
	}
	require.True(t, conditionalRejection(wrapper+`if {${a}} { return -code error "no" }; set x 1`+"\n"), "a plain variable beside the ifs changes nothing the fetch reads")
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
		{"a write instead of rejecting", wrapper + "\n    ui_error \"unsupported\"\n    distfiles-append other.tar.gz\n", plain, "ends with `distfiles-append other.tar.gz` rather than return -code error, which writes `distfiles`"},
		{"error with a computed message", wrapper + "error \"[exec uname] is unsupported\"\n", plain, "returns a computed message `\"[exec uname] is unsupported\"`"},
		{"work before rejecting", wrapper + "catch {set result [active_variants R tcltk]}\nreturn -code error no\n", plain, "runs `catch {set result [active_variants R tcltk]}` before rejecting, which runs `set result [active_variants R tcltk]`, which runs `active_variants R tcltk`, which is unknown to the grammar"},
		{"computed message", wrapper + "return -code error [subst text]\n", plain, "returns a computed message `[subst text]`"},
		{"nested if with a branch doing work", wrapper + "if {${os.major} < 11} {\n    if {![variant_isset x]} { set distfiles other }\n}\n", plain, "has a branch that ends with `set distfiles other` rather than return -code error, which writes `distfiles`"},
		{"nested if after a write", wrapper + "if {${os.major} < 11} {\n    distfiles other\n    if {![variant_isset x]} { return -code error no }\n}\n", plain, "has a branch that runs `distfiles other` before rejecting, which writes `distfiles`"},
		{"host read with a command substitution", wrapper + "if {![file exists [exec brew --prefix]/lib]} { return -code error no }\n", plain, "calls `file` with a computed argument in its condition: `![file exists [exec brew --prefix]/lib]`"},
		{"host write in a condition", wrapper + "if {[file delete ${prefix}/lib]} { return -code error no }\n", plain, "calls `file delete ${prefix}/lib` in its condition: `[file delete ${prefix}/lib]`"},
		{"catch in a condition", wrapper + "if {![catch {set result [active_variants R tcltk]}]} { return -code error no }\n", plain, "calls `catch` in its condition, which runs `set result [active_variants R tcltk]`, which runs `active_variants R tcltk`, which is unknown to the grammar: `![catch {set result [active_variants R tcltk]}]`"},
		{"info other than exists", wrapper + "if {[info commands foo] ne \"\"} { return -code error no }\n", plain, "calls `info commands foo` in its condition: `[info commands foo] ne \"\"`"},
		{"computed query argument", wrapper + "if {[variant_isset ${flavor}]} { return -code error no }\n", plain, "calls `variant_isset` with a computed argument in its condition: `[variant_isset ${flavor}]`"},
		{"procedure after the if", wrapper + "if {${a}} { return -code error no }\nmpi.action_enforce_variants ${name}\n", plain, "runs `mpi.action_enforce_variants ${name}` outside an if, which is unknown to the grammar"},
		{"unbraced condition", wrapper + "if $a { return -code error no }\n", plain, "has a condition that is not braced: `$a`"},
		{"branch doing work", wrapper + "if {${a}} { set distfiles other }\n", plain, "has a branch that ends with `set distfiles other` rather than return -code error, which writes `distfiles`"},
		{"go check off github", goCheck, macports.PortInfo{Options: map[string]string{"go.domain": "gitlab.com"}}, "is the Go PortGroup's toolchain check, recognized only when go.domain is github.com; this port's is gitlab.com"},
		{"go check without a domain", goCheck, plain, "is the Go PortGroup's toolchain check, recognized only when go.domain is github.com; this port's is unset"},
		{"altered go check", strings.Replace(goCheck, "[go_toolchain.ceiling]", "[other.check]", 1), github, "differs from the Go PortGroup's toolchain check as dockhand knows it"},
		{"unwrapped", "return -code error", plain, "is not wrapped as a Base hook"},
		{"empty", wrapper + "\n", plain, "is empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			semantics := assessFetch(test.info, "portfetch::fetch_main", "{"+test.hook+"}", "", nil, nil)
			require.Equal(t, "custom", semantics.Kind)
			require.Equal(t, "pre-fetch hook 1 "+test.want, semantics.Problem)
		})
	}
	// The origin places the offending command at its own line in the file:
	// the hook body starts at Portfile line 12, its first line blank, and the
	// error is on the body's third line.
	hook := wrapper + "\n    ui_error \"unsupported\"\n    distfiles other\n"
	semantics := assessFetch(plain, "portfetch::fetch_main", "{"+hook+"}", "", []hookOrigin{{Label: "Portfile", Line: 12}}, nil)
	require.Equal(t, "pre-fetch hook 1 ends with `distfiles other` rather than return -code error, which writes `distfiles`, at Portfile line 13", semantics.Problem)
	semantics = assessFetch(plain, "portfetch::fetch_main", "{"+hook+"}", "", []hookOrigin{{Label: "the java-1.0 PortGroup", Line: 40}}, nil)
	require.Equal(t, "pre-fetch hook 1 ends with `distfiles other` rather than return -code error, which writes `distfiles`, in the java-1.0 PortGroup at line 41", semantics.Problem)
	require.Equal(t, []hookOrigin{{Label: "Portfile", Line: 5}, {}, {Label: "the x-1.0 PortGroup", Line: 9}}, parseOrigins("{Portfile 5} {} {{the x-1.0 PortGroup} 9}"))
	// A recognized hook with an origin is a guard as before.
	semantics = assessFetch(plain, "portfetch::fetch_main", "{"+wrapper+"return -code error no\n}", "", []hookOrigin{{Label: "Portfile", Line: 3}}, nil)
	require.Equal(t, "guarded", semantics.Kind)
	require.Empty(t, semantics.Problem)
}

// The three extensions of 2026-09-22, each a shape the survey found behind
// ports whose hooks can fail the fetch but never change what is fetched:
// Tcl's error with one plain message where return -code error stood, a
// branch that is itself a conditional rejection, and a host read in a
// condition, a file's existence or kind, a version comparison, or whether
// a variable is set, with plain arguments.
func TestGrammarExtensionsOnlyAdmitRejections(t *testing.T) {
	t.Parallel()
	wrapper := "global {*}[info globals]\n"
	plain := macports.PortInfo{Options: map[string]string{}}
	for name, hook := range map[string]string{
		"error with a plain message":  wrapper + "error \"Building ${subport} @${version} on Mac OS X 10.6 requires the MacOSX10.7.sdk\"\n",
		"ui_error then error":         wrapper + "ui_error \"$name requires Rust\"\nerror \"unsupported OS version\"\n",
		"error in a branch":           wrapper + "if {![variant_isset jdk11] && ![variant_isset jdk17]} {\n    error \"Either +jdk11 or +jdk17 is required\"\n}\n",
		"nested if":                   wrapper + "if {${os.major} < 11} {\n    if {![variant_isset x]} { ui_error \"no runtime\"; return -code error no }\n}\n",
		"llvm-10's check":             wrapper + "if {${os.major} < 11} {\n    if {![file exists /usr/lib/libc++.dylib]} {\n        ui_error \"$name requires a C++11 runtime\"\n        error \"unsupported configuration\"\n    }\n}\n",
		"ld64's check three deep":     wrapper + "if {${os.major} < 9} {\n    if {${llvm_version} != \"\"} {\n        if {![file exists ${prefix}/bin/llvm-config-mp-${llvm_version}]} {\n            return -code error \"install ld64 first\"\n        }\n    }\n}\n",
		"file exists with a variable": wrapper + "if {![file exists ${java_home}]} { ui_error \"Java 1.6 is required\"; return -code error \"Java 1.6 missing\" }\n",
		"file isdirectory":            wrapper + "if {![file isdirectory ${prefix}/lib/foo]} { return -code error no }\n",
		"vercmp":                      wrapper + "if {[vercmp ${xcodeversion} ${xcodeversion_min_required}] < 0} { ui_error \"old Xcode\"; return -code error \"incompatible Xcode version\" }\n",
		"vercmp with an operator":     wrapper + "if {${os.major} >= 12 || [vercmp $xcodeversion >= 4.4]} { return -code error no }\n",
		"info exists":                 wrapper + "if {![info exists python_framework]} { error \"one python variant must be enabled\" }\n",
		"mpi variant query":           wrapper + "if {${mpi.require} && [mpi_variant_name] eq \"\"} { return -code error \"must set at least one mpi variant\" }\n",
		"nested else branches":        wrapper + "if {${a}} { if {${b}} { return -code error b } else { return -code error c } } elseif {${d}} { error d }\n",
	} {
		t.Run(name, func(t *testing.T) {
			semantics := assessFetch(plain, "portfetch::fetch_main", "{"+hook+"}", "", nil, nil)
			require.Empty(t, semantics.Problem)
			require.Equal(t, "guarded", semantics.Kind)
		})
	}
	// A hook that rejects unconditionally through error still preserves the
	// platform restriction; a nested conditional still does not claim one.
	semantics := assessFetch(plain, "portfetch::fetch_main", "{"+wrapper+"error \"unsupported platform\"\n}", "", nil, nil)
	require.True(t, semantics.Rejected)
	semantics = assessFetch(plain, "portfetch::fetch_main", "{"+wrapper+"if {${a}} { if {${b}} { error no } }\n}", "", nil, nil)
	require.False(t, semantics.Rejected)
	require.Equal(t, []string{"pre-fetch hook 1 only rejects unsupported configurations"}, semantics.Guards)
	// What stays outside: a read whose argument is computed by a command, a
	// file subcommand that is not a predicate, a query other than exists, a
	// branch that works before its nested if, and an error carrying more
	// than a message.
	for _, hook := range []string{
		wrapper + "if {[file exists [glob ${prefix}/lib/*.dylib]]} { error no }\n",
		wrapper + "if {[file mkdir ${prefix}/lib]} { error no }\n",
		wrapper + "if {[file exists ${prefix}/a ${prefix}/b]} { error no }\n",
		wrapper + "if {[info vars python*] eq \"\"} { error no }\n",
		wrapper + "if {[vercmp]} { error no }\n",
		wrapper + "if {${a}} { set distfiles x; if {${b}} { error no } }\n",
		wrapper + "if {${a}} { if {${b}} { error no }; set distfiles other }\n",
		wrapper + "error \"no\" [subst info]\n",
		wrapper + "error [subst no]\n",
		wrapper + "error {*}$messages\n",
	} {
		semantics := assessFetch(plain, "portfetch::fetch_main", "{"+hook+"}", "", nil, nil)
		require.NotEmpty(t, semantics.Problem, hook)
		require.Equal(t, "custom", semantics.Kind, hook)
	}
}

// The effect rule: a command the grammar's own rules do not name is judged
// by what it does to the fetch, a procedure by its body as the worker
// shipped it. A write to a fetch option is refused wherever it hides; a
// read, a diagnostic, a plain variable, or an option outside the fetch's
// inputs changes nothing, and is admitted. Unknown stays refused.
func TestEffectRuleJudgesCommandsByWhatTheyDoToTheFetch(t *testing.T) {
	t.Parallel()
	wrapper := "global {*}[info globals]\n"
	plain := macports.PortInfo{Options: map[string]string{}}
	defs := definitions{
		"configure.env-append":    {kind: "option", option: "configure.env"},
		"java.home":               {kind: "option", option: "java.home"},
		"distfiles-append":        {kind: "option", option: "distfiles"},
		"registry_active":         {kind: "command"},
		"active_variants":         {kind: "proc", args: "name variant", body: "set installed [registry_active $name]\nreturn [string match \"*$variant*\" $installed]\n"},
		"mpi_active_variant_name": {kind: "proc", args: "portname", body: "if {[active_variants $portname mpich]} { return mpich }\nreturn \"\"\n"},
		"mpi.action_enforce_variants": {kind: "proc", args: "ports", body: `global name
ui_debug "mpi.enforce_variant list: ${ports}"
foreach depspec $ports {
    set portname [_get_dep_port $depspec]
    if {![catch {set result [active_variants $portname "" ""]}]} {
        set otmpi [mpi_active_variant_name $portname]
        set mympi [mpi_variant_name]
        if {$otmpi ne "" && $mympi eq ""} {
            ui_error "Need to select variant +$otmpi"
            return -code error "$portname +$otmpi is installed, so +$otmpi needs to be selected for $name"
        } elseif {$otmpi ne $mympi} {
            ui_error "Install $portname +$mympi"
            return -code error "$portname +$mympi not installed"
        }
    } else {
        ui_error "Internal error: '$portname' does not refer to an installed port."
    }
}
`},
		"java::java_set_env":   {kind: "proc", args: "", body: "set java_home [java::find_java_home]\nconfigure.env-append JAVA_HOME=${java_home}\njava.home ${java_home}\n"},
		"java::find_java_home": {kind: "proc", args: "", body: "if {[catch {exec /usr/libexec/java_home -V} result]} { return \"\" }\nreturn $result\n"},
		"helper_writes":        {kind: "proc", args: "", body: "distfiles-append extra.tar.gz\n"},
		"deep1":                {kind: "proc", args: "", body: "deep2\n"},
		"deep2":                {kind: "proc", args: "", body: "deep3\n"},
		"deep3":                {kind: "proc", args: "", body: "deep4\n"},
		"deep4":                {kind: "proc", args: "", body: "deep5\n"},
		"deep5":                {kind: "proc", args: "", body: "deep6\n"},
		"deep6":                {kind: "proc", args: "", body: "ui_msg deep\n"},
		"_get_dep_port":        {kind: "proc", args: "depspec", body: "set speclist [split $depspec :]\nset portname [lindex $speclist end]\nset res [_portnameactive $portname]\nif {$res != 0} { return $portname }\nset depfile \"\"\nswitch [lindex $speclist 0] {\n    bin { set depfile [_mportsearchpath $portname /usr/bin 1 1] }\n    path { set depfile [_mportsearchpath $portname /opt 0 1] }\n}\nif {$depfile eq \"\"} { return $portname } else { return [registry_file_registered $depfile] }\n"},
	}
	assess := func(hook string) macports.FetchSemantics {
		return assessFetch(plain, "portfetch::fetch_main", "{"+wrapper+hook+"}", "", nil, defs)
	}
	mpi := "if {${mpi.require} && [mpi_variant_name] eq \"\"} {\n    return -code error \"must set at least one mpi variant\"\n}\nmpi.action_enforce_variants ${mpi.required_variants}\n"
	semantics := assess(mpi)
	require.Empty(t, semantics.Problem, "the MPI PortGroup's hook: a query in the condition and a procedure after the if that only raises")
	require.Equal(t, "guarded", semantics.Kind)
	require.False(t, semantics.Rejected)
	require.Equal(t, []string{"pre-fetch hook 1 only rejects unsupported configurations"}, semantics.Guards)
	for name, test := range map[string]struct{ hook, want string }{
		"an option outside the fetch before rejecting": {"configure.env-append X=1\nreturn -code error no\n", ""},
		"a procedure that writes environment":          {"java::java_set_env\nreturn -code error no\n", "runs `java::java_set_env` before rejecting, which runs `set java_home [java::find_java_home]` inside java::java_set_env, which runs `java::find_java_home`, which runs `if {[catch {exec /usr/libexec/java_home -V} result]} { re...` inside java::find_java_home, which runs `exec /usr/libexec/java_home -V`, which is not followed by the grammar"},
		"a fetch option before rejecting":              {"distfiles-append extra.tar.gz\nreturn -code error no\n", "runs `distfiles-append extra.tar.gz` before rejecting, which writes `distfiles`"},
		"a procedure that writes a fetch option":       {"helper_writes\nreturn -code error no\n", "runs `helper_writes` before rejecting, which runs `distfiles-append extra.tar.gz` inside helper_writes, which writes `distfiles`"},
		"an unknown procedure":                         {"mystery\nreturn -code error no\n", "runs `mystery` before rejecting, which is unknown to the grammar"},
		"a procedure beyond the depth followed":        {"deep1\nreturn -code error no\n", "runs `deep1` before rejecting, which runs `deep2` inside deep1, which runs `deep3` inside deep2, which runs `deep4` inside deep3, which runs `deep5` inside deep4, which runs `deep6` inside deep5, which reaches `deep6` beyond the depth the grammar follows"},
		"a hook that rejects nothing":                  {"ui_msg \"Selected db${db_ver}\"\n", ""},
		"a registry read in a condition":               {"if {![catch {set result [active_variants R tcltk]}]} { return -code error no }\n", ""},
		"a computed array index":                       {"ui_error \"$array([exec touch file])\"\nreturn -code error no\n", "runs `ui_error \"$array([exec touch file])\"` before rejecting, which substitutes a computed index in `$array([exec touch file])`"},
		"an option read":                               {"set required [option bun.version]\nif {$required ne \"\"} { return -code error no }\n", ""},
		"an option written":                            {"option distfiles other.tar.gz\nreturn -code error no\n", "runs `option distfiles other.tar.gz` before rejecting, which writes `distfiles`"},
		"an option named by a variable":                {"option $which value\nreturn -code error no\n", "runs `option $which value` before rejecting, which names a computed option"},
	} {
		t.Run(name, func(t *testing.T) {
			semantics := assess(test.hook)
			if test.want == "" {
				require.Empty(t, semantics.Problem)
				require.Equal(t, "guarded", semantics.Kind)
				return
			}
			require.Equal(t, "custom", semantics.Kind)
			require.Equal(t, "pre-fetch hook 1 "+test.want, semantics.Problem)
		})
	}
	semantics = assess("ui_msg \"Selected db${db_ver}\"\n")
	require.Equal(t, []string{"pre-fetch hook 1 changes nothing the fetch reads"}, semantics.Guards)
	require.False(t, semantics.Rejected)
	semantics = assess("configure.env-append X=1\nreturn -code error no\n")
	require.True(t, semantics.Rejected, "a hook that ends by rejecting outright still preserves the platform restriction")
	semantics = assess("error \"no\" {} {NONE}\n")
	require.True(t, semantics.Rejected, "error with Tcl's info and code arguments is a rejection")
}

func TestParseDefinitionsReadsTheWorkersList(t *testing.T) {
	t.Parallel()
	defs := parseDefinitions("{configure.env-append option configure.env} {helper proc {{a b} {ui_msg $a}}} {registry_active command {}} {broken proc {onlyone}} {odd kind x}")
	require.Equal(t, definitions{
		"configure.env-append": {kind: "option", option: "configure.env"},
		"helper":               {kind: "proc", args: "a b", body: "ui_msg $a"},
		"registry_active":      {kind: "command"},
	}, defs)
	require.Nil(t, parseDefinitions("{unbalanced"))
}
