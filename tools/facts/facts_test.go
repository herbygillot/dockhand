package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macos"
)

const probeDoc = `{
  "image": "dockhand-base-tahoe", "slug": "tahoe", "profile": "base", "date": "2026-09-25T22:26:37Z",
  "facts": {
    "shell": {"sw_vers_productVersion": {"value": "26.6.2", "error": null},
              "clt_sdks": {"value": ["MacOSX.sdk -> MacOSX26.5.sdk", "MacOSX26.sdk -> MacOSX26.5.sdk"], "error": null}},
    "host_checks": {"file_exists_usr_lib_libxcselect_dylib": {"value": false, "error": null}},
    "base": {"os_major": {"value": "25", "error": null}, "build_arch": {"value": "arm64", "error": null},
             "xcodeversion": {"value": "none", "error": null}, "xcodecltversion": {"value": "26.6.0.0.1781586589", "error": null},
             "developer_dir": {"value": "/Library/Developer/CommandLineTools", "error": null},
             "macosx_sdk_version": {"value": "26", "error": null}, "macports_version": {"value": "2.12.6", "error": null}},
    "worker": {"compiler.command_line_tools_version(clang)": {"value": "2100.1.1.101", "error": null}}
  }
}`

func TestATartProbeIsARowOfTheTable(t *testing.T) {
	row, ok, err := probeRow([]byte(probeDoc))
	require.NoError(t, err)
	require.True(t, ok)
	absent := false
	require.Equal(t, macos.Facts{
		Darwin: 25, Architecture: "arm64", Profile: macos.ProfileTools, MacOS: "26.6.2",
		Xcode: "none", Tools: "26.6.0.0.1781586589", DeveloperDir: "/Library/Developer/CommandLineTools",
		SDKs: []string{"MacOSX.sdk -> MacOSX26.5.sdk", "MacOSX26.sdk -> MacOSX26.5.sdk"}, SDK: "26", Clang: "2100.1.1.101", XCSelect: &absent,
		Source: macos.Source{Kind: macos.SourceTart, From: "dockhand-base-tahoe", Date: "2026-09-25", MacPorts: "2.12.6"},
	}, row)

	_, ok, err = probeRow([]byte(`{"image": "dockhand-base-tahoe", "profile": "base", "facts": null}`))
	require.NoError(t, err)
	require.False(t, ok, "a probe that failed gives no row")
}

func TestTheNewestHarvestOfARowWins(t *testing.T) {
	older := macos.Facts{Darwin: 25, Architecture: "arm64", Profile: macos.ProfileTools, Tools: "27.0", Source: macos.Source{Kind: macos.SourceTart, From: "dockhand-base-tahoe", Date: "2026-09-23"}}
	newer := older
	newer.Tools, newer.Source.Date = "26.6", "2026-09-25"
	buildbot := macos.Facts{Darwin: 25, Architecture: "arm64", Profile: macos.ProfileXcode, Tools: "26.6", Source: macos.Source{Kind: macos.SourceBuildbot, From: "ports-26_arm64-builder build 1", Date: "2026-09-26"}}
	require.Equal(t, []macos.Facts{newer, buildbot}, newest([]macos.Facts{older, buildbot, newer}))
}

func TestAGenerationIsWhatTheArm64BuilderRuns(t *testing.T) {
	row := func(darwin int, arch, tools string, kind string) macos.Facts {
		return macos.Facts{Darwin: darwin, Architecture: arch, Profile: macos.ProfileXcode, Tools: tools, Source: macos.Source{Kind: kind, From: "x", Date: "2026-09-26"}}
	}
	got := generations([]macos.Facts{
		row(24, "x86_64", "26.0.0.0.1.1757719676", macos.SourceBuildbot),
		row(24, "arm64", "16.4.0.0.1.1747106510", macos.SourceBuildbot),
		row(25, "arm64", "27.0.0.0.1788430756", macos.SourceTart),
		row(25, "arm64", "26.6.0.0.1781586589", macos.SourceBuildbot),
		row(19, "x86_64", "none", macos.SourceBuildbot),
	})
	require.Len(t, got, 2)
	require.Equal(t, 24, got[0].Darwin)
	require.Equal(t, 16, got[0].Tools, "the arm64 builder's, not the Intel one's")
	require.Equal(t, 26, got[1].Tools, "the builder's, not an image's")
}

const runnerDoc = `{
  "image": "macos-15-intel", "slug": "Xcode_16.4", "profile": "xcode", "source": "runner",
  "developer": "/Applications/Xcode_16.4.app/Contents/Developer", "date": "2026-11-05T03:10:00Z",
  "host": {"image_version": "20261027.0101", "run": "https://github.com/herbygillot/dockhand/actions/runs/123"},
  "facts": {
    "shell": {"sw_vers_productVersion": {"value": "15.7.1", "error": null},
              "xcode_sdks": {"value": ["/Applications/Xcode.app: MacOSX.sdk", "/Applications/Xcode_16.4.app: MacOSX.sdk",
                                       "/Applications/Xcode_16.4.app: MacOSX15.5.sdk -> MacOSX.sdk", "/Applications/Xcode_26.3.app: MacOSX.sdk"], "error": null}},
    "base": {"os_major": {"value": "24", "error": null}, "build_arch": {"value": "x86_64", "error": null},
             "xcodeversion": {"value": "16.4", "error": null}, "xcodecltversion": {"value": "16.4.0.0.1.1747106510", "error": null},
             "developer_dir": {"value": "/Applications/Xcode_16.4.app/Contents/Developer", "error": null},
             "macosx_sdk_version": {"value": "15", "error": null}, "macports_version": {"value": "2.12.7", "error": null}},
    "base_tools": {"get_compiler_version(clang)": {"value": "1700.0.13.5", "error": null}},
    "worker": {"compiler.command_line_tools_version(clang)": {"value": "1700.0.13.5", "error": null}}
  }
}`

// A runner's probe is a row of its own kind, named by its image, run, and
// the Xcode selected, whose Xcode SDKs are that Xcode's, not
// /Applications/Xcode.app's.
func TestARunnerProbeIsARowOfItsSelectedXcode(t *testing.T) {
	row, ok, err := probeRow([]byte(runnerDoc))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, macos.ProfileXcode, row.Profile)
	require.Equal(t, macos.Source{Kind: macos.SourceRunner, From: "macos-15-intel run 123, Xcode_16.4.app", Date: "2026-11-05", MacPorts: "2.12.7"}, row.Source)
	require.Equal(t, []string{"MacOSX.sdk", "MacOSX15.5.sdk -> MacOSX.sdk"}, row.XcodeSDKs)
	require.Equal(t, "1700.0.13.5", row.XcodeClang, "Xcode's own clang, as Base reads it under Xcode's developer_dir (#213)")

	clt := strings.NewReplacer(`"profile": "xcode"`, `"profile": "clt"`, `"developer": "/Applications/Xcode_16.4.app/Contents/Developer"`, `"developer": "/Library/Developer/CommandLineTools"`).Replace(runnerDoc)
	row, ok, err = probeRow([]byte(clt))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, macos.ProfileTools, row.Profile)
	require.Equal(t, "macos-15-intel run 123, Command Line Tools", row.Source.From)
	require.Empty(t, row.XcodeClang, "the tools profile has no Xcode clang")

	host := strings.NewReplacer(`"source": "runner"`, `"source": "host"`, `"image": "macos-15-intel"`, `"image": "aws-mac1-sequoia"`).Replace(runnerDoc)
	host = regexp.MustCompile(`"run": "[^"]*"`).ReplaceAllString(host, `"macos": "15.8"`)
	row, ok, err = probeRow([]byte(host))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, macos.Source{Kind: macos.SourceHost, From: "aws-mac1-sequoia, Xcode_16.4.app", Date: "2026-11-05", MacPorts: "2.12.7"}, row.Source, "a host is named without a run")
}

// The probes kept in tools/facts/probes are rows of the table: each one
// parses, names a host, and has its release and toolchain.
func TestTheKeptHostProbesAreRows(t *testing.T) {
	paths, err := filepath.Glob("probes/*/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		row, ok, err := probeRow(data)
		require.NoError(t, err, path)
		require.True(t, ok, path)
		require.Equal(t, macos.SourceHost, row.Source.Kind, path)
		require.NoError(t, macos.FactsTable{Facts: []macos.Facts{row}}.Check(), path)
	}
}

// Of a runner's Xcodes the table keeps the one with the toolchain the
// release's builder runs, and the tools selected where they are the
// builder's; a runner whose tools aren't the builder's stands for nothing.
func TestTheTableKeepsARunnersBuilderToolchain(t *testing.T) {
	runner := func(darwin int, xcode, tools string) macos.Facts {
		profile := macos.ProfileXcode
		if xcode == "none" {
			profile = macos.ProfileTools
		}
		return macos.Facts{Darwin: darwin, Architecture: "x86_64", Profile: profile, Xcode: xcode, Tools: tools, Source: macos.Source{Kind: macos.SourceRunner, From: "run 1, " + xcode, Date: "2026-11-05"}}
	}
	builder := func(darwin int, xcode, tools string) macos.Facts {
		return macos.Facts{Darwin: darwin, Architecture: "x86_64", Profile: macos.ProfileXcode, Xcode: xcode, Tools: tools, Source: macos.Source{Kind: macos.SourceBuildbot, From: "builder build 1", Date: "2026-09-27"}}
	}
	var report strings.Builder
	kept := runnerRows([]macos.Facts{
		runner(25, "26.5", "26.6"), runner(25, "26.6", "26.6"), runner(25, "none", "26.6"),
		runner(24, "16.4", "16.4"), runner(24, "none", "16.4"),
		runner(23, "16.2", "16.2"),
		runner(22, "16.4", "26.0.0.0.1.1756965055"), runner(22, "none", "26.0.0.0.1.1756965055"), runner(22, "16.4", "26.1.0.0.1.1760000000"),
	}, []macos.Facts{builder(25, "26.6", "26.6"), builder(24, "16.4", "26.0"), builder(22, "16.4", "26.0.0.0.1.1757719676")}, &report)
	require.Equal(t, []macos.Facts{runner(25, "26.6", "26.6"), runner(25, "none", "26.6"),
		runner(22, "16.4", "26.0.0.0.1.1756965055"), runner(22, "none", "26.0.0.0.1.1756965055")}, kept,
		"tools of the builder's release in another build are the builder's")
	require.Contains(t, report.String(), "darwin 25 x86_64 xcode: Xcode 26.5, tools 26.6, clang  (run 1, 26.5): the builder runs Xcode 26.6, tools 26.6")
	require.Contains(t, report.String(), "darwin 24 x86_64 xcode: Xcode 16.4, tools 16.4, clang  (run 1, 16.4): the builder runs Xcode 16.4, tools 26.0")
	require.Contains(t, report.String(), "darwin 23 x86_64 xcode: Xcode 16.2, tools 16.2, clang  (run 1, 16.2): no builder row to choose by")
}

type file struct {
	fs.FileInfo
	dir bool
}

func (f file) IsDir() bool { return f.dir }

// A runner probes each Xcode once, not the links its image adds beside
// them, and the tools last.
func TestARunnerProbesEachXcodeOnceAndTheToolsLast(t *testing.T) {
	glob := func(string) ([]string, error) {
		return []string{"/Applications/Xcode.app", "/Applications/Xcode_16.4.0.app", "/Applications/Xcode_26.3.app", "/Applications/Xcode_16.4.app"}, nil
	}
	lstat := func(path string) (os.FileInfo, error) {
		switch path {
		case "/Applications/Xcode.app", "/Applications/Xcode_16.4.0.app":
			return file{dir: false}, nil
		}
		return file{dir: true}, nil
	}
	require.Equal(t, []string{
		"/Applications/Xcode_16.4.app/Contents/Developer",
		"/Applications/Xcode_26.3.app/Contents/Developer",
		"/Library/Developer/CommandLineTools",
	}, runnerDevelopers(glob, lstat))
}

// An installer reading gives each builder's toolchain the SDKs and clang
// its log doesn't: Xcode's SDKs with the tools' clang where the builder runs
// the tools, Xcode's clang where it runs none, the tools alone as the
// Command Line Tools profile, and Xcode 3's developer directory, /Developer.
func TestInstallersFillTheBuildersToolchains(t *testing.T) {
	builder := func(darwin int, macOS, xcode, tools string) macos.Facts {
		return macos.Facts{Darwin: darwin, Architecture: "x86_64", Profile: macos.ProfileXcode, MacOS: macOS, Xcode: xcode, Tools: tools, SDK: macOS[:strings.LastIndex(macOS, ".")],
			Source: macos.Source{Kind: macos.SourceBuildbot, From: "builder build 1", Date: "2026-09-27"}}
	}
	sdks := func(names ...string) []sdk {
		var listed []sdk
		for _, name := range names {
			listed = append(listed, sdk{Entry: name})
		}
		return listed
	}
	xcode := func(name, version, layout, clang string, listed ...string) installer {
		i := installer{Installer: name, Kind: "xcode", XcodeVersion: version, Layout: layout, XcodeSDKs: sdks(listed...)}
		i.Clang.Clang = clang
		return i
	}
	clt := func(name, version, clang string, listed ...string) installer {
		i := installer{Installer: name, Kind: "clt", ToolsVersion: version, DeveloperDir: "/Library/Developer/CommandLineTools", SDKs: sdks(listed...)}
		i.Clang.Clang = clang
		return i
	}
	installers := []installer{
		xcode("xcode_3.2.6_and_ios_sdk_4.3.dmg", "3.2.6", "/Developer", "77", "MacOSX10.4u.sdk", "MacOSX10.5.sdk", "MacOSX10.6.sdk"),
		xcode("Xcode_9.2.xip", "9.2", "Xcode.app/Contents/Developer", "900.0.39.2", "MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"),
		clt("Command_Line_Tools_macOS_10.12_for_Xcode_9.2.dmg", "9.2.0.0.1.1510905681", "900.0.39.2", "MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"),
		clt("Command_Line_Tools_macOS_10.13_for_Xcode_9.2.dmg", "9.2.0.0.1.1510905681", "900.0.39.2", "MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"),
	}
	var report strings.Builder
	rows := installerRows(installers, "2026-11-10", []macos.Facts{
		builder(10, "10.6.8", "3.2.6", "none"), builder(17, "10.13.6", "9.2", "9.2.0.0.1.1510905681"), builder(16, "10.12.6", "8.2.1", "8.2.0.0.1.1480973914"),
	}, &report)
	source := func(from string) macos.Source {
		return macos.Source{Kind: macos.SourceInstaller, From: from, Date: "2026-11-10"}
	}
	require.Equal(t, []macos.Facts{
		{Darwin: 10, Architecture: "x86_64", Profile: macos.ProfileXcode, MacOS: "10.6.8", Xcode: "3.2.6", Tools: "none", DeveloperDir: "/Developer", SDK: "10.6",
			XcodeSDKs: []string{"MacOSX10.4u.sdk", "MacOSX10.5.sdk", "MacOSX10.6.sdk"}, Clang: "77", XcodeClang: "77", Source: source("xcode_3.2.6_and_ios_sdk_4.3.dmg")},
		{Darwin: 17, Architecture: "x86_64", Profile: macos.ProfileXcode, MacOS: "10.13.6", Xcode: "9.2", Tools: "9.2.0.0.1.1510905681", DeveloperDir: "/Applications/Xcode.app/Contents/Developer", SDK: "10.13",
			SDKs: []string{"MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"}, XcodeSDKs: []string{"MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"}, Clang: "900.0.39.2",
			XcodeClang: "900.0.39.2", Source: source("Xcode_9.2.xip, Command_Line_Tools_macOS_10.13_for_Xcode_9.2.dmg")},
		{Darwin: 17, Architecture: "x86_64", Profile: macos.ProfileTools, MacOS: "10.13.6", Xcode: "none", Tools: "9.2.0.0.1.1510905681", DeveloperDir: "/Library/Developer/CommandLineTools", SDK: "10.13",
			SDKs: []string{"MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"}, Clang: "900.0.39.2", Source: source("Command_Line_Tools_macOS_10.13_for_Xcode_9.2.dmg")},
	}, rows, "the tools' installer named for the builder's release")
	require.Contains(t, report.String(), "reported: darwin 16 x86_64: Xcode 8.2.1, tools 8.2.0.0.1.1480973914: no installer read")
	require.NoError(t, macos.FactsTable{Facts: rows}.Check())
}

// The readings kept in tools/facts/installers parse, are dated, and give
// every buildbot release from Darwin 10 to 17 its Xcode's SDKs and a clang.
func TestTheKeptInstallerReadingsCoverTheOldBuilders(t *testing.T) {
	paths, err := filepath.Glob("installers/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	installers, date, err := readInstallers(paths)
	require.NoError(t, err)
	require.NotEmpty(t, date)
	for _, i := range installers {
		require.NotEmpty(t, i.Clang.Clang, i.Installer)
		require.Contains(t, []string{"xcode", "clt"}, i.Kind, i.Installer)
	}
	for _, release := range []struct {
		darwin       int
		xcode, tools string
	}{{10, "3.2.6", "none"}, {11, "4.6.3", "none"}, {12, "5.1.1", "none"}, {13, "6.2", "6.2.0.0.1.1424975374"}, {14, "7.2.1", "7.2.0.0.1.1447826929"},
		{15, "8.2.1", "8.2.0.0.1.1480973914"}, {16, "9.2", "9.2.0.0.1.1510905681"}, {17, "9.4.1", "9.4.1.0.1.1528165917"}} {
		builder := macos.Facts{Darwin: release.darwin, Architecture: "x86_64", Profile: macos.ProfileXcode, MacOS: "10.x.0", Xcode: release.xcode, Tools: release.tools,
			Source: macos.Source{Kind: macos.SourceBuildbot, From: "builder build 1", Date: "2026-09-27"}}
		rows := installerRows(installers, date, []macos.Facts{builder}, io.Discard)
		require.NotEmpty(t, rows, "darwin %d", release.darwin)
		require.NotEmpty(t, rows[0].XcodeSDKs, "darwin %d", release.darwin)
		require.NotEmpty(t, rows[0].Clang, "darwin %d", release.darwin)
		require.NoError(t, macos.FactsTable{Facts: rows}.Check())
	}
}
