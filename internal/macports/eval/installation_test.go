package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// hostInstallation is what the test prefix has installed: a port the
// registry holds, a program only the prefix has, and one the prefix
// shadows along PATH with an argument that only reports, and whose answer
// tells the two apart.
type hostInstallation struct {
	prefix, port, only, shadowed, flag string
}

func findHostInstallation(t *testing.T) hostInstallation {
	t.Helper()
	_, tcl := tclSession(t)
	host := hostInstallation{prefix: tcl("set ::macports::prefix")}
	host.port = tcl("set entries [registry::entry imaged]; expr {[llength $entries] ? [[lindex $entries 0] name] : {}}")
	programs, err := filepath.Glob(filepath.Join(host.prefix, "bin", "*"))
	require.NoError(t, err)
	base := []string{"port", "portf", "portindex", "portmirror", "port-tclsh", "daemondo"}
	for _, program := range programs {
		name := filepath.Base(program)
		if slices.Contains(base, name) {
			continue
		}
		elsewhere := false
		for _, dir := range []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				elsewhere = true
			}
		}
		if !elsewhere && host.only == "" {
			host.only = program
		}
	}
	for _, candidate := range [][2]string{{"perl", "-V:installprefix"}, {"python3", "--version"}} {
		if _, err := os.Stat(filepath.Join(host.prefix, "bin", candidate[0])); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join("/usr/bin", candidate[0])); err == nil {
			host.shadowed, host.flag = candidate[0], candidate[1]
			break
		}
	}
	return host
}

// evaluatedDescription evaluates lines that leave a Tcl list in results,
// and returns the list: the port's description carries it out.
func evaluatedDescription(t *testing.T, lines string) []string {
	t.Helper()
	got, err := evaluateLines(t, "set results {}\n"+lines+"\ndescription {*}$results", false)
	require.NoError(t, err)
	values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["description"])
	require.Empty(t, errs)
	return values
}

func TestTheInstallationIsFresh(t *testing.T) {
	t.Parallel()
	host := findHostInstallation(t)
	got := evaluatedDescription(t, strings.Join([]string{
		"lappend results [file exists ${prefix}/bin/port]",
		"lappend results [file isdirectory ${prefix}/lib] [file isdirectory ${prefix}/lib/pkgconfig]",
		"lappend results [file exists ${prefix}/share/macports/install/prefix.mtree]",
		"lappend results [lsort [glob -nocomplain -tails -directory ${prefix}/bin *]]",
		"lappend results [catch {registry_active dockhand-no-such-port} message] $message",
	}, "\n"))
	require.Equal(t, "1", got[0], "Base's own program is there")
	require.Equal(t, "1", got[1], "the prefix's skeleton is there")
	require.Equal(t, "0", got[2], "a directory MacPorts' package leaves for ports isn't, whatever this Mac has")
	require.Equal(t, "1", got[3], "Base's own files are there")
	listed, errs := syntax.ListValues(got[4])
	require.Empty(t, errs)
	for _, name := range listed {
		require.Contains(t, []string{"port", "portf", "portindex", "portmirror", "port-tclsh", "daemondo"}, name, "only Base's programs are in a fresh bin")
	}
	require.Equal(t, []string{"1", "Registry error: dockhand-no-such-port not registered as installed & active."}, got[5:7])

	if host.port != "" {
		got := evaluatedDescription(t, strings.Join([]string{
			"lappend results [_portnameactive " + host.port + "]",
			"lappend results [catch {registry_active " + host.port + "} message] $message",
			"lappend results [registry_exists_for_name " + host.port + "]",
		}, "\n"))
		require.Equal(t, []string{"0", "1", "Registry error: " + host.port + " not registered as installed & active.", "0"}, got,
			"%s is installed on this Mac and not in a fresh installation", host.port)
	}
	if host.only != "" {
		name := filepath.Base(host.only)
		got := evaluatedDescription(t, strings.Join([]string{
			"lappend results [file exists " + host.only + "] [file executable " + host.only + "]",
			"lappend results [catch {file size " + host.only + "} message] $message",
			"lappend results [catch {open " + host.only + "} message] $message",
			"lappend results [catch {exec " + host.only + "} message] $message",
			"lappend results [catch {exec " + name + "} message] $message",
			"lappend results [catch {findBinary " + name + "} message]",
			"lappend results [registry_file_registered " + host.only + "]",
		}, "\n"))
		require.Equal(t, []string{
			"0", "0",
			"1", `could not read "` + host.only + `": no such file or directory`,
			"1", `couldn't open "` + host.only + `": no such file or directory`,
			"1", `couldn't execute "` + host.only + `": no such file or directory`,
			"1", `couldn't execute "` + name + `": no such file or directory`,
			"1",
			"0",
		}, got, "%s is installed on this Mac and not in a fresh installation", host.only)
	}
	if host.shadowed != "" {
		want, err := exec.Command(filepath.Join("/usr/bin", host.shadowed), host.flag).CombinedOutput()
		require.NoError(t, err)
		got := evaluatedDescription(t, "lappend results [exec "+host.shadowed+" "+host.flag+" 2>@1]")
		require.Equal(t, strings.TrimSuffix(string(want), "\n"), got[0], "%s runs from /usr/bin, not the prefix", host.shadowed)
		require.NotContains(t, got[0], host.prefix)
	}
}

func TestTheLedgerNamesWhatTheInstallationWasAsked(t *testing.T) {
	t.Parallel()
	got, err := evaluateLines(t, "catch {registry_active dockhand-asked}\nset found [_portnameactive dockhand-too]", true)
	require.NoError(t, err)
	var asked []string
	for _, entry := range got.Ports["effects"].Ledger {
		if entry.Source == "fresh" && entry.Subject != "" {
			asked = append(asked, entry.Command+" "+entry.Subject)
		}
	}
	require.Contains(t, asked, "registry_active dockhand-asked")
	require.Contains(t, asked, "_portnameactive dockhand-too")
}

// A program neither this Mac nor the fresh installation has is answered
// missing from the fresh installation, as one only the host has is, never
// run on the host: perl5.30 and php-config7x, which the Mac hadn't, were
// run there and counted host-in-model, 9,855 of a whole-tree survey's
// modelled evaluations (#101).
func TestAProgramNeitherHasIsAnsweredWithoutRunningIt(t *testing.T) {
	t.Parallel()
	got, err := evaluateLines(t, strings.Join([]string{
		"set results {}",
		"lappend results [catch {exec dockhand-absent-101 -V:archname} message] $message",
		"lappend results [catch {exec ${prefix}/bin/dockhand-absent-101 --extension-dir} message] $message",
		"description {*}$results",
	}, "\n"), true)
	require.NoError(t, err)
	values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["description"])
	require.Empty(t, errs)
	prefix := findHostInstallation(t).prefix
	require.Equal(t, []string{
		"1", `couldn't execute "dockhand-absent-101": no such file or directory`,
		"1", `couldn't execute "` + prefix + `/bin/dockhand-absent-101": no such file or directory`,
	}, values)
	for _, entry := range got.Ports["effects"].Ledger {
		if entry.Command == "exec" {
			require.NotContains(t, []string{"process", "host", "host-in-model"}, entry.Source, "nothing ran: %+v", entry)
		}
	}
}

func TestFileStatSetsItsCallersArray(t *testing.T) {
	t.Parallel()
	got := evaluatedDescription(t, "file stat /etc/hosts info\nlappend results [info exists info(size)]")
	require.Equal(t, []string{"1"}, got)
}

// observedDescription observes lines in a platform's context, leaving a Tcl
// list in results that the port's description carries out.
func observedDescription(t *testing.T, platform model.Platform, tools model.DeveloperTools, lines string) ([]string, macports.PortObservation) {
	t.Helper()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/effects/Portfile", "PortSystem 1.0\nname effects\nversion 1\nset results {}\n"+lines+"\ndescription {*}$results\n")
	bound, err := tree.Select(model.Target{Name: "effects", Portfile: "devel/effects/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true, Platform: platform, DeveloperTools: tools})
	require.NoError(t, err)
	values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["description"])
	require.Empty(t, errs)
	return values, got.Ports["effects"]
}

// A modelled context that doesn't state its tools is modelled as MacPorts'
// builders are set up, with Xcode from the table's Xcode row, whatever
// tools this Mac has (D2, decided 2026-09-29).
func TestAnUnstatedContextIsEvaluatedWithXcode(t *testing.T) {
	t.Parallel()
	monterey := model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}
	xcode, err := macports.Toolchain(monterey, model.DeveloperToolsXcode)
	require.NoError(t, err)
	got, _ := observedDescription(t, monterey, "", "lappend results $xcodeversion $developer_dir [file exists /Applications/Xcode.app]")
	require.Equal(t, []string{xcode.Xcode, macports.XcodeDeveloper, "1"}, got)
}

// A modelled context answers Base's toolchain questions from the facts
// table (docs/oracle.md, phase 5), whatever tools this Mac has: here, one
// that states the Command Line Tools.
func TestAModelledContextTakesItsToolsFromTheTable(t *testing.T) {
	t.Parallel()
	lines := strings.Join([]string{
		"lappend results $xcodeversion $developer_dir [compiler.command_line_tools_version clang]",
		"lappend results ${configure.sdkroot} ${configure.cc}",
		"lappend results [file executable /Library/Developer/CommandLineTools/usr/bin/make] [file exists /usr/lib/libxcselect.dylib] [file exists /Applications/Xcode.app]",
	}, "\n")
	for _, c := range []struct {
		platform model.Platform
		clang    string
		sdk      string
	}{
		{model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}, "1400.0.29.202", "MacOSX12.sdk"},
		{model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, "2100.1.1.101", "MacOSX26.sdk"},
	} {
		tools, err := macports.Toolchain(c.platform, model.DeveloperToolsCommandLine)
		require.NoError(t, err)
		require.Equal(t, c.clang, tools.Clang, "the table, as this test expects it")
		got, port := observedDescription(t, c.platform, model.DeveloperToolsCommandLine, lines)
		require.Equal(t, []string{"none", macports.CommandLineTools, c.clang,
			macports.CommandLineTools + "/SDKs/" + c.sdk, "/usr/bin/clang",
			"1", "0", "0"}, got, "%+v", c.platform)
		sources := map[string]int{}
		for _, entry := range port.Ledger {
			sources[entry.Source] += entry.Count
		}
		require.Positive(t, sources["table"], "%+v: %v", c.platform, sources)
	}
}

// Whether the system has headers in /usr/include, which Base's
// portconfigure asks of /usr/include/sys/cdefs.h, is the modelled
// release's: the tools' through 10.13, none from 10.15, and the host's to
// say on 10.14, where a headers package put them (the survey's insight 1:
// 3,419 ports asked the Mac).
func TestAModelledContextSaysWhetherItHasSystemHeaders(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		version, architecture string
		want                  string
	}{
		{"15", "x86_64", "1"},
		{"21", "arm64", "0"},
		{"25", "x86_64", "0"},
		{"18", "x86_64", ""},
	} {
		platform := model.Platform{OS: "darwin", Version: c.version, Architecture: c.architecture}
		got, port := observedDescription(t, platform, model.DeveloperToolsCommandLine, "lappend results [file exists /usr/include/sys/cdefs.h]")
		if c.want == "" {
			require.True(t, port.ModeledHostAccess, "darwin %s asks the host", c.version)
			continue
		}
		require.Equal(t, []string{c.want}, got, "darwin %s", c.version)
		require.False(t, port.ModeledHostAccess, "darwin %s: %v", c.version, port.Problems)
	}
}

// A modelled context with Xcode answers Xcode's SDKs, and xcrun's
// --show-sdk-path, from the facts table, where it lists them: the survey's
// insight 1 found 3,651 ports testing …/SDKs/MacOSX* on the host, and
// 3,563 running xcrun there (#17). An SDK not listed is absent and not
// located; xcrun finds an SDK by its version exactly, MacOSX15.5.sdk,
// never macosx15.
func TestAModelledContextAnswersXcodesSDKsFromTheTable(t *testing.T) {
	t.Parallel()
	sequoia := model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}
	sdks := macports.XcodeDeveloper + "/Platforms/MacOSX.platform/Developer/SDKs"
	clt := macports.CommandLineTools + "/SDKs"
	lines := strings.Join([]string{
		"lappend results [file exists " + sdks + "/MacOSX15.sdk] [file exists " + sdks + "/MacOSX10.13.sdk] [file isdirectory " + sdks + "/MacOSX.sdk]",
		"lappend results [lsort [glob -nocomplain -tails -directory " + sdks + " MacOSX*.sdk]]",
		"lappend results [exec xcrun --sdk macosx --show-sdk-path 2> /dev/null] [exec xcrun --sdk macosx15.5 --show-sdk-path 2> /dev/null]",
		"lappend results [catch {exec xcrun --sdk macosx15 --show-sdk-path 2> /dev/null}] [catch {exec xcrun --sdk macosx10.13 --show-sdk-path 2> /dev/null}]",
		"lappend results [exec env DEVELOPER_DIR=" + macports.CommandLineTools + " xcrun --sdk macosx15.5 --show-sdk-path 2> /dev/null] [exec env DEVELOPER_DIR=" + macports.CommandLineTools + " xcrun --sdk macosx --show-sdk-path 2> /dev/null]",
	}, "\n")
	got, port := observedDescription(t, sequoia, model.DeveloperToolsXcode, lines)
	require.Equal(t, []string{"1", "0", "1",
		"MacOSX.sdk MacOSX15.5.sdk MacOSX15.sdk",
		sdks + "/MacOSX15.5.sdk", sdks + "/MacOSX15.5.sdk",
		"1", "1",
		clt + "/MacOSX.sdk", clt + "/MacOSX.sdk"}, got, "the tools' MacOSX.sdk links to MacOSX15.5.sdk and comes first, as the tools' 27.0 said here")
	require.False(t, port.ModeledHostAccess, "%v", port.Problems)
}

// An Xcode before 8 has an SDK per version and no MacOSX.sdk: Xcode 4.6.3,
// Darwin 11's builder's, has MacOSX10.7.sdk and MacOSX10.8.sdk, read from
// its installer, and xcrun takes the newest for --sdk macosx.
func TestAModelledContextAnswersAnOldXcodesSDKsByVersion(t *testing.T) {
	t.Parallel()
	lion := model.Platform{OS: "darwin", Version: "11", Architecture: "x86_64"}
	sdks := macports.XcodeDeveloper + "/Platforms/MacOSX.platform/Developer/SDKs"
	lines := strings.Join([]string{
		"lappend results [file isdirectory " + sdks + "/MacOSX10.7.sdk] [file exists " + sdks + "/MacOSX.sdk]",
		"lappend results [exec xcrun --sdk macosx --show-sdk-path 2> /dev/null] [exec xcrun --sdk macosx10.7 --show-sdk-path 2> /dev/null]",
		"lappend results [catch {exec xcrun --sdk macosx10.6 --show-sdk-path 2> /dev/null}]",
	}, "\n")
	got, port := observedDescription(t, lion, model.DeveloperToolsXcode, lines)
	require.Equal(t, []string{"1", "0", sdks + "/MacOSX10.8.sdk", sdks + "/MacOSX10.7.sdk", "1"}, got)
	require.False(t, port.ModeledHostAccess, "%v", port.Problems)
}

// A modelled release's os.version and macos_version are the table's,
// whose minor some Portfiles test: py-numpy, py-scipy, and stimfit offer
// +accelerate on Darwin 22 only from os.minor 4, and at 22.0.0 the model
// picked +openblas where MacPorts' builder, 22.6.0, has +accelerate
// (#105).
func TestAModelledContextHasItsReleasesMinorVersion(t *testing.T) {
	t.Parallel()
	ventura := model.Platform{OS: "darwin", Version: "22", Architecture: "x86_64"}
	tools, err := macports.Toolchain(ventura, "")
	require.NoError(t, err)
	require.Equal(t, []string{"22.6.0", "13.7.8"}, []string{tools.DarwinVersion, tools.MacOS}, "the table, as this test expects it")
	got, _ := observedDescription(t, ventura, "", strings.Join([]string{
		"lappend results ${os.version} ${os.minor} $macos_version ${macos_version_major} ${macosx_deployment_target}",
		"if {${os.major} == 22 && ${os.minor} >= 4} { lappend results accelerate }",
	}, "\n"))
	require.Equal(t, []string{"22.6.0", "6", "13.7.8", "13", "13.0", "accelerate"}, got)
}

// A C++20 port in a modelled context with Xcode and the tools takes the
// tools' clang's version from the table, under configure.developer_dir,
// which is the tools': Ventura's Intel builder's 1403 is under Base's 1500
// minimum for C++20, so a MacPorts clang builds it, as on the builder,
// where the model had asked this Mac's clang and kept Apple's (#106).
func TestAModelledCxx20PortTakesTheTablesClang(t *testing.T) {
	t.Parallel()
	ventura := model.Platform{OS: "darwin", Version: "22", Architecture: "x86_64"}
	tools, err := macports.Toolchain(ventura, "")
	require.NoError(t, err)
	require.Equal(t, "1403.0.22.14.1", tools.Clang, "the table, as this test expects it")
	for _, standard := range []string{"2017", "2020"} {
		got, port := observedDescription(t, ventura, "", "compiler.cxx_standard "+standard+"\nlappend results [compiler.command_line_tools_version clang] ${configure.compiler}")
		require.Len(t, got, 2)
		require.Equal(t, tools.Clang, got[0], standard)
		if standard == "2017" {
			require.Equal(t, "clang", got[1], "Apple's clang builds C++17")
		} else {
			require.True(t, strings.HasPrefix(got[1], "macports-clang-"), "C++20: %s", got[1])
		}
		require.False(t, port.ModeledHostAccess, "%v", port.Problems)
	}
}

// Where the row lists no SDKs, as the Command Line Tools before 10.14
// have none and Darwin 10's /Developer is no directory the model keeps
// Xcode's SDKs in, xcrun finds none and the SDK glob is empty, the
// table's lack marked, and the host's xcrun isn't run: it ran 8,059 times
// in Darwin 10 to 12 contexts in the M1 survey, and gave Darwin 10 the
// host's own SDK (#108).
func TestAnUnlistedSDKIsAbsentWithoutTheHost(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		platform model.Platform
		tools    model.DeveloperTools
		glob     string
	}{
		{model.Platform{OS: "darwin", Version: "13", Architecture: "x86_64"}, model.DeveloperToolsCommandLine, macports.CommandLineTools + "/SDKs"},
		{model.Platform{OS: "darwin", Version: "10", Architecture: "x86_64"}, model.DeveloperToolsXcode, ""},
	} {
		lines := "lappend results [catch {exec xcrun --sdk macosx --show-sdk-path 2> /dev/null}] [catch {exec xcrun --sdk macosx10.9 --show-sdk-path 2> /dev/null}]"
		if c.glob != "" {
			lines += "\nlappend results [llength [glob -nocomplain -directory " + c.glob + " MacOSX*.sdk]] [catch {glob -directory " + c.glob + " MacOSX*.sdk}]"
		}
		got, port := observedDescription(t, c.platform, c.tools, lines)
		want := []string{"1", "1"}
		if c.glob != "" {
			want = append(want, "0", "1")
		}
		require.Equal(t, want, got, "%+v", c.platform)
		require.False(t, port.ModeledHostAccess, "%+v: %v", c.platform, port.Problems)
		unlisted := 0
		for _, entry := range port.Ledger {
			if entry.Source == "table" && entry.Subject == "sdks-unlisted" {
				unlisted += entry.Count
			}
		}
		require.Positive(t, unlisted, "%+v: %v", c.platform, port.Ledger)
	}
}
