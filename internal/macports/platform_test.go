package macports_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/stretchr/testify/require"
)

// The variables agree with MacPorts base, where os_arch is what uname -p
// reports, and with the buildbot's index_vars files, which say the same
// for the platforms they cover.
func TestPlatformVariablesFollowMacPortsBase(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		platform record.Platform
		want     string
	}{
		{record.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}, "os_platform darwin os_subplatform macosx os_major 24 os_version 24.0.0 os_arch arm build_arch arm64 macos_version 15 macos_version_major 15 macosx_version 15 macosx_deployment_target 15.0 universal_archs {arm64 x86_64} cxx_stdlib libc++"},
		{record.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, "os_platform darwin os_subplatform macosx os_major 25 os_version 25.0.0 os_arch i386 build_arch x86_64 macos_version 26 macos_version_major 26 macosx_version 26 macosx_deployment_target 26.0 universal_archs {arm64 x86_64} cxx_stdlib libc++"},
		{record.Platform{OS: "darwin", Version: "19", Architecture: "x86_64"}, "os_platform darwin os_subplatform macosx os_major 19 os_version 19.0.0 os_arch i386 build_arch x86_64 macos_version 10.15 macos_version_major 10.15 macosx_version 10.15 macosx_deployment_target 10.15 universal_archs x86_64 cxx_stdlib libc++"},
		{record.Platform{OS: "darwin", Version: "12", Architecture: "i386"}, "os_platform darwin os_subplatform macosx os_major 12 os_version 12.0.0 os_arch i386 build_arch i386 macos_version 10.8 macos_version_major 10.8 macosx_version 10.8 macosx_deployment_target 10.8 universal_archs {x86_64 i386} cxx_stdlib libc++"},
		{record.Platform{OS: "darwin", Version: "9", Architecture: "ppc"}, "os_platform darwin os_subplatform macosx os_major 9 os_version 9.0.0 os_arch powerpc build_arch ppc macos_version 10.5 macos_version_major 10.5 macosx_version 10.5 macosx_deployment_target 10.5 universal_archs {i386 ppc} cxx_stdlib libstdc++"},
	} {
		got, err := macports.PlatformVariables(test.platform)
		require.NoError(t, err)
		require.Equal(t, test.want, got, "%+v", test.platform)
	}
	for _, platform := range []record.Platform{{OS: "plan9", Version: "20", Architecture: "arm64"}, {OS: "darwin", Version: "7", Architecture: "x86_64"}, {OS: "darwin", Version: "20", Architecture: "mips"}, {OS: "darwin", Version: "twenty", Architecture: "arm64"}} {
		_, err := macports.PlatformVariables(platform)
		require.ErrorContains(t, err, "unsupported modeled platform", "%+v", platform)
	}
}

// A host that is not a Mac describes the platform and a Mac with the current
// Command Line Tools and no Xcode, whose clang answers for both places Base
// looks for it, as one list of pairs override_vars accepts.
func TestModelVariablesAddTheCommandLineTools(t *testing.T) {
	t.Parallel()
	platform := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	base, err := macports.PlatformVariables(platform)
	require.NoError(t, err)
	got, err := macports.ModelVariables(platform)
	require.NoError(t, err)
	tools := macos.CurrentToolchain
	require.Equal(t, base+" developer_dir /Library/Developer/CommandLineTools xcodeversion none xcodecltversion "+tools.Xcode+
		" compiler_version_cache {versions {/Library/Developer/CommandLineTools {/usr/bin/clang "+tools.Clang+" /Library/Developer/CommandLineTools/usr/bin/clang "+tools.Clang+"}}}", got)
	pairs, errs := syntax.ListValues(got)
	require.Empty(t, errs)
	require.Zero(t, len(pairs)%2)
	_, err = macports.ModelVariables(record.Platform{OS: "linux", Version: "6", Architecture: "x86_64"})
	require.ErrorContains(t, err, "unsupported modeled platform")
}

func TestRuntimeIsModeledWhenItDescribesAnotherHost(t *testing.T) {
	t.Parallel()
	mac := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	require.False(t, macports.Runtime{Platform: mac}.Modeled())
	require.True(t, macports.Runtime{Platform: mac, Host: record.Platform{OS: "linux", Version: "6", Architecture: "x86_64"}}.Modeled())
}
