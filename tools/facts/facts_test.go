package main

import (
	"io/fs"
	"os"
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
             "macosx_sdk_version": {"value": "15", "error": null}, "macports_version": {"value": "2.12.7", "error": null}}
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

	clt := strings.NewReplacer(`"profile": "xcode"`, `"profile": "clt"`, `"developer": "/Applications/Xcode_16.4.app/Contents/Developer"`, `"developer": "/Library/Developer/CommandLineTools"`).Replace(runnerDoc)
	row, ok, err = probeRow([]byte(clt))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, macos.ProfileTools, row.Profile)
	require.Equal(t, "macos-15-intel run 123, Command Line Tools", row.Source.From)
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
	}, []macos.Facts{builder(25, "26.6", "26.6"), builder(24, "16.4", "26.0")}, &report)
	require.Equal(t, []macos.Facts{runner(25, "26.6", "26.6"), runner(25, "none", "26.6")}, kept)
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
