package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macos"
)

func TestReadHeaderReadsEveryNameBaseGivesTheRelease(t *testing.T) {
	for _, c := range []struct {
		log    string
		want   header
		wanted bool
	}{
		{"DEBUG: OS darwin/25.6.0 (macOS 26.7) arch i386\nDEBUG: macOS 26.7 (darwin/25.6.0) arch i386\nDEBUG: MacPorts 2.12.6\nDEBUG: Xcode 26.6, CLT 26.6.0.0.1781586589\nDEBUG: SDK 26\n",
			header{MacOS: "26.7", Darwin: 25, MacPorts: "2.12.6", Xcode: "26.6", Tools: "26.6.0.0.1781586589", SDK: "26"}, true},
		{"DEBUG: OS X 10.11.6 (darwin/15.6.0) arch i386\r\nDEBUG: MacPorts 2.12.6\r\nDEBUG: Xcode 8.2.1, CLT 8.2.0.0.1.1480973914\r\nDEBUG: SDK 10.11\r\n",
			header{MacOS: "10.11.6", Darwin: 15, MacPorts: "2.12.6", Xcode: "8.2.1", Tools: "8.2.0.0.1.1480973914", SDK: "10.11"}, true},
		{"DEBUG: Mac OS X 10.6.8 (darwin/10.8.0) arch i386\nDEBUG: MacPorts 2.12.6\nDEBUG: Xcode 3.2.6, CLT none\nDEBUG: SDK 10.6\n",
			header{MacOS: "10.6.8", Darwin: 10, MacPorts: "2.12.6", Xcode: "3.2.6", Tools: "none", SDK: "10.6"}, true},
		{"DEBUG: macOS 26.7 (darwin/25.6.0) arch i386\nDEBUG: MacPorts 2.12.6\n", header{MacOS: "26.7", Darwin: 25, MacPorts: "2.12.6"}, false},
	} {
		got, ok := readHeader(strings.NewReader(c.log))
		require.Equal(t, c.wanted, ok, c.log)
		require.Equal(t, c.want, got, c.log)
	}
}

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
	row, ok, err := tartRow([]byte(probeDoc))
	require.NoError(t, err)
	require.True(t, ok)
	absent := false
	require.Equal(t, macos.Facts{
		Darwin: 25, Architecture: "arm64", Profile: macos.ProfileTools, MacOS: "26.6.2",
		Xcode: "none", Tools: "26.6.0.0.1781586589", DeveloperDir: "/Library/Developer/CommandLineTools",
		SDKs: []string{"MacOSX.sdk -> MacOSX26.5.sdk", "MacOSX26.sdk -> MacOSX26.5.sdk"}, SDK: "26", Clang: "2100.1.1.101", XCSelect: &absent,
		Source: macos.Source{Kind: macos.SourceTart, From: "dockhand-base-tahoe", Date: "2026-09-25", MacPorts: "2.12.6"},
	}, row)

	_, ok, err = tartRow([]byte(`{"image": "dockhand-base-tahoe", "profile": "base", "facts": null}`))
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
