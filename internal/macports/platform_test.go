package macports_test

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/stretchr/testify/require"
)

// The variables agree with MacPorts base, where os_arch is what uname -p
// reports, and with the buildbot's index_vars files, which say the same
// for the platforms they cover.
func TestPlatformVariablesFollowMacPortsBase(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		platform model.Platform
		want     string
	}{
		{model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}, "os_platform darwin os_subplatform macosx os_major 24 os_minor 0 os_version 24.0.0 os_arch arm build_arch arm64 macos_version 15 macos_version_major 15 macosx_version 15 macosx_deployment_target 15.0 universal_archs {arm64 x86_64} cxx_stdlib libc++"},
		{model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, "os_platform darwin os_subplatform macosx os_major 25 os_minor 0 os_version 25.0.0 os_arch i386 build_arch x86_64 macos_version 26 macos_version_major 26 macosx_version 26 macosx_deployment_target 26.0 universal_archs {arm64 x86_64} cxx_stdlib libc++"},
		{model.Platform{OS: "darwin", Version: "19", Architecture: "x86_64"}, "os_platform darwin os_subplatform macosx os_major 19 os_minor 0 os_version 19.0.0 os_arch i386 build_arch x86_64 macos_version 10.15 macos_version_major 10.15 macosx_version 10.15 macosx_deployment_target 10.15 universal_archs x86_64 cxx_stdlib libc++"},
		{model.Platform{OS: "darwin", Version: "12", Architecture: "i386"}, "os_platform darwin os_subplatform macosx os_major 12 os_minor 0 os_version 12.0.0 os_arch i386 build_arch i386 macos_version 10.8 macos_version_major 10.8 macosx_version 10.8 macosx_deployment_target 10.8 universal_archs {x86_64 i386} cxx_stdlib libc++"},
		{model.Platform{OS: "darwin", Version: "9", Architecture: "ppc"}, "os_platform darwin os_subplatform macosx os_major 9 os_minor 0 os_version 9.0.0 os_arch powerpc build_arch ppc macos_version 10.5 macos_version_major 10.5 macosx_version 10.5 macosx_deployment_target 10.5 universal_archs {i386 ppc} cxx_stdlib libstdc++"},
	} {
		got, err := macports.PlatformVariables(test.platform)
		require.NoError(t, err)
		require.Equal(t, test.want, got, "%+v", test.platform)
	}
	for _, platform := range []model.Platform{{OS: "plan9", Version: "20", Architecture: "arm64"}, {OS: "darwin", Version: "7", Architecture: "x86_64"}, {OS: "darwin", Version: "20", Architecture: "mips"}, {OS: "darwin", Version: "twenty", Architecture: "arm64"}} {
		_, err := macports.PlatformVariables(platform)
		require.ErrorContains(t, err, "unsupported modeled platform", "%+v", platform)
	}
}

// A modelled context that states the Command Line Tools takes them from
// the facts table: an image's, a runner's, or an installer reading's row
// where there is one, else a buildbot's made tools-only, and the host where
// no source tells.
func TestToolchainComesFromTheFactsTable(t *testing.T) {
	t.Parallel()
	image, ok := macos.Table().Lookup(25, "arm64", macos.ProfileTools)
	require.True(t, ok)
	got, err := macports.Toolchain(model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	require.False(t, got.Derived)
	require.Equal(t, "none", got.Xcode)
	require.Equal(t, image.Tools, got.Tools)
	require.Equal(t, macports.CommandLineTools, got.DeveloperDir)
	require.Equal(t, image.Clang, got.Clang)
	require.Equal(t, image.SDKs, got.SDKs)
	require.False(t, got.XCSelect)

	// Intel Tahoe's tools are a runner's row, probed with the tools
	// selected, which agrees with the arm64 image's of the same package.
	got, err = macports.Toolchain(model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	require.False(t, got.Derived)
	require.Equal(t, macos.SourceRunner, got.Source.Kind)
	require.Equal(t, "none", got.Xcode)
	require.Equal(t, image.Tools, got.Tools)
	require.Equal(t, macports.CommandLineTools, got.DeveloperDir)
	require.Equal(t, image.Clang, got.Clang)
	require.Equal(t, image.SDKs, got.SDKs)

	// High Sierra's tools are its builder's, with the SDK and clang their
	// installer gives them.
	builder, ok := macos.Table().Lookup(17, "x86_64", macos.ProfileXcode)
	require.True(t, ok)
	got, err = macports.Toolchain(model.Platform{OS: "darwin", Version: "17", Architecture: "x86_64"}, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	require.False(t, got.Derived)
	require.Equal(t, macos.SourceInstaller, got.Source.Kind)
	require.Equal(t, "none", got.Xcode)
	require.Equal(t, builder.Tools, got.Tools)
	require.Equal(t, macports.CommandLineTools, got.DeveloperDir)
	require.Equal(t, "902.0.39.2", got.Clang)
	require.Equal(t, []string{"MacOSX.sdk", "MacOSX10.13.sdk -> MacOSX.sdk"}, got.SDKs)

	got, err = macports.Toolchain(model.Platform{OS: "darwin", Version: "19", Architecture: "x86_64"}, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	require.Equal(t, "none", got.Tools, "the 10.15 builder carries no tools")
	require.NotEqual(t, "none", got.Xcode, "so it is modelled as it is, with Xcode")
	require.Equal(t, macports.XcodeDeveloper, got.DeveloperDir)
	require.True(t, got.XCSelect, "libxcselect is a file before macOS 11")

	_, err = macports.Toolchain(model.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}, model.DeveloperToolsCommandLine)
	require.ErrorIs(t, err, macports.ErrNoToolchain)
}

// A context with Xcode is modelled in the Xcode profile: a Tart Xcode
// image's row where there is one, else a buildbot's, with Xcode's developer
// directory, and its clang answering under that directory. A release
// without an Xcode row has no toolchain with Xcode.
func TestAnXcodeToolchainComesFromItsProfile(t *testing.T) {
	t.Parallel()
	tahoe := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	image, ok := macos.Table().Lookup(25, "arm64", macos.ProfileXcode)
	require.True(t, ok)
	require.Equal(t, macos.SourceTart, image.Source.Kind)
	got, err := macports.Toolchain(tahoe, model.DeveloperToolsXcode)
	require.NoError(t, err)
	require.Equal(t, image.Xcode, got.Xcode)
	require.NotEqual(t, "none", got.Xcode)
	require.Equal(t, image.Tools, got.Tools)
	require.Equal(t, macports.XcodeDeveloper, got.DeveloperDir)
	require.Equal(t, image.Source, got.Source)

	variables, err := macports.ModelVariables(tahoe, model.DeveloperToolsXcode)
	require.NoError(t, err)
	pairs, errs := syntax.DictValues(variables)
	require.Empty(t, errs)
	require.Equal(t, macports.XcodeDeveloper, pairs["developer_dir"])
	require.Equal(t, image.Xcode, pairs["xcodeversion"])
	require.Contains(t, pairs["compiler_version_cache"], "versions {"+macports.XcodeDeveloper+" {/usr/bin/clang "+got.Clang)
	answers, err := macports.ToolchainAnswers(tahoe, model.DeveloperToolsXcode)
	require.NoError(t, err)
	flags, errs := syntax.DictValues(answers)
	require.Empty(t, errs)
	require.Equal(t, "1", flags["xcode"])
	require.Equal(t, "1", flags["tools"])

	builder, err := macports.Toolchain(model.Platform{OS: "darwin", Version: "20", Architecture: "arm64"}, model.DeveloperToolsXcode)
	require.NoError(t, err)
	require.Equal(t, macos.SourceInstaller, builder.Source.Kind, "Big Sur has no Tart image; its builder's Xcode is read from its installer")
	require.Equal(t, macports.XcodeDeveloper, builder.DeveloperDir)

	snowLeopard, err := macports.Toolchain(model.Platform{OS: "darwin", Version: "10", Architecture: "x86_64"}, model.DeveloperToolsXcode)
	require.NoError(t, err)
	require.Equal(t, "/Developer", snowLeopard.DeveloperDir, "Xcode 3 is installed in /Developer")
	require.Equal(t, []string{"MacOSX10.4u.sdk", "MacOSX10.5.sdk", "MacOSX10.6.sdk"}, snowLeopard.XcodeSDKs)

	_, err = macports.Toolchain(model.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}, model.DeveloperToolsXcode)
	require.ErrorIs(t, err, macports.ErrNoToolchain)
}

// ModelVariables is PlatformVariables with the modelled tools, as one list
// of pairs override_vars accepts, whose caches answer for clang and
// /usr/bin's shims; a platform the table has no row for is described alone,
// whether or not it states its tools.
func TestModelVariablesAddTheModelledTools(t *testing.T) {
	t.Parallel()
	platform := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	base, err := macports.PlatformVariables(platform)
	require.NoError(t, err)
	tools, err := macports.Toolchain(platform, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	got, err := macports.ModelVariables(platform, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	pairs, errs := syntax.DictValues(got)
	require.Empty(t, errs)
	require.True(t, strings.HasPrefix(got, base+" "))
	require.Equal(t, macports.CommandLineTools, pairs["developer_dir"])
	require.Equal(t, "none", pairs["xcodeversion"])
	require.Equal(t, tools.Tools, pairs["xcodecltversion"])
	require.Equal(t, "26", pairs["macosx_sdk_version"])
	require.Equal(t, "versions {/Library/Developer/CommandLineTools {/usr/bin/clang "+tools.Clang+" /Library/Developer/CommandLineTools/usr/bin/clang "+tools.Clang+"}}", pairs["compiler_version_cache"])
	paths, errs := syntax.DictValues(pairs["tool_path_cache"])
	require.Empty(t, errs)
	require.Equal(t, "/usr/bin/clang++", paths["clang++"])
	require.Equal(t, "", paths["llvm-gcc-4.2"])

	// The release's versions are the row's, standing over the 22.0.0
	// PlatformVariables gives (#105).
	got, err = macports.ModelVariables(model.Platform{OS: "darwin", Version: "22", Architecture: "x86_64"}, "")
	require.NoError(t, err)
	pairs, errs = syntax.DictValues(got)
	require.Empty(t, errs)
	require.Equal(t, []string{"22.6.0", "6", "13.7.8"}, []string{pairs["os_version"], pairs["os_minor"], pairs["macos_version"]})

	old := model.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}
	described, err := macports.PlatformVariables(old)
	require.NoError(t, err)
	got, err = macports.ModelVariables(old, "")
	require.NoError(t, err)
	require.Equal(t, described, got, "a release without facts is described alone")
	answers, err := macports.ToolchainAnswers(old, "")
	require.NoError(t, err)
	require.Empty(t, answers)

	_, err = macports.ModelVariables(model.Platform{OS: "linux", Version: "6", Architecture: "x86_64"}, "")
	require.ErrorContains(t, err, "unsupported modeled platform")
}

func TestToolchainAnswersNameTheSDKsAndTheirLinks(t *testing.T) {
	t.Parallel()
	got, err := macports.ToolchainAnswers(model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, model.DeveloperToolsCommandLine)
	require.NoError(t, err)
	answers, errs := syntax.DictValues(got)
	require.Empty(t, errs)
	require.Equal(t, "25", answers["darwin"])
	require.Equal(t, "1", answers["tools"])
	require.Equal(t, "0", answers["xcode"])
	require.Equal(t, "0", answers["xcselect"])
	sdks, errs := syntax.ListValues(answers["sdks"])
	require.Empty(t, errs)
	require.Contains(t, sdks, "MacOSX26.sdk")
	require.Contains(t, sdks, "MacOSX.sdk")
	require.NotContains(t, sdks, "->")
}

func TestRuntimeIsModeledWhenItDescribesAnotherHost(t *testing.T) {
	t.Parallel()
	mac := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	require.False(t, macports.Runtime{Platform: mac}.Modeled())
	require.True(t, macports.Runtime{Platform: mac, Host: model.Platform{OS: "linux", Version: "6", Architecture: "x86_64"}}.Modeled())
}

// A context that doesn't state its tools is modelled as MacPorts' builders
// are set up, with Xcode, from the table's Xcode row (D2, decided
// 2026-09-29).
func TestAnUnstatedContextIsModelledWithXcode(t *testing.T) {
	t.Parallel()
	for _, platform := range []model.Platform{{OS: "darwin", Version: "21", Architecture: "arm64"}, {OS: "darwin", Version: "19", Architecture: "x86_64"}} {
		unstated, err := macports.Toolchain(platform, "")
		require.NoError(t, err)
		xcode, err := macports.Toolchain(platform, model.DeveloperToolsXcode)
		require.NoError(t, err)
		require.Equal(t, xcode, unstated, "%+v", platform)
		require.NotEqual(t, "none", unstated.Xcode)
		variables, err := macports.ModelVariables(platform, "")
		require.NoError(t, err)
		pairs, errs := syntax.DictValues(variables)
		require.Empty(t, errs)
		require.Equal(t, macports.XcodeDeveloper, pairs["developer_dir"], "%+v", platform)
		require.Equal(t, unstated.Xcode, pairs["xcodeversion"])
	}
}
