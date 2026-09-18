package portedit

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProfilesIncludeBoundaryAndArchitectureWithoutImpossibleOldARM(t *testing.T) {
	t.Parallel()
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	profiles, err := observationProfiles([]byte(`if {${os.major} >= 17} {version 1} else {version 0}
if {${build_arch} eq "arm64"} {distfiles a} else {distfiles b}`), native)
	require.NoError(t, err)
	require.Equal(t, native, profiles[0])
	require.Contains(t, profiles, record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"})
	require.Contains(t, profiles, record.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"})
	require.NotContains(t, profiles, record.Platform{OS: "darwin", Version: "16", Architecture: "arm64"})
	_, err = observationProfiles([]byte(`if {${os.major} >= $minimum} {version 1}`), native)
	require.ErrorIs(t, err, errProbeInconclusive)
}

func TestProfilesRefuseUnresolvedReadsAlongsideKnownBoundaries(t *testing.T) {
	t.Parallel()
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	for _, source := range []string{
		`set major ${os.major}; if {$major >= 17} {version 1}`,
		`if {${os.major} >= 17 && ${os.major} < $limit} {version 1}`,
		`if {${os.major} >= 17 + 5} {version 1}`,
		`if {${os.major} >= 17.5} {version 1}`,
		`if {17 + 25 < ${os.major}} {version 1}`,
		`if {${os.version} eq "25.1.0"} {version 1}`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := observationProfiles([]byte(source), native)
			require.ErrorIs(t, err, errProbeInconclusive)
		})
	}
}

func TestUnmodeledReadsAreGapsOnlyWhereTheyCanSelectSources(t *testing.T) {
	t.Parallel()
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	for _, source := range []string{
		`configure.env-append MACOSX_DEPLOYMENT_TARGET=${macosx_deployment_target}`,
		`if {${os.platform} eq "darwin" && [vercmp ${macosx_deployment_target} >= 15.0]} {
    macosx_deployment_target 14.0
}`,
		`platform darwin { if { [vercmp ${macosx_deployment_target} >= 15.0]} { macosx_deployment_target 14.0 } }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { patchfiles-append patch-old.diff }`,
		// Patch selection never reaches the archive plan; the patch check reads the native selection.
		`variant legacy { patchfiles-append legacy-${macosx_deployment_target}.diff }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { legacysupport.use_mp_libcxx yes }`,
		`build.env-append MACOSX_DEPLOYMENT_TARGET=[shellescape ${macosx_deployment_target}]`,
		`post-patch { reinplace "s/X/${macosx_deployment_target}/" ${worksrcpath}/Info.plist }`,
		`build { system -W ${worksrcpath} "env MACOSX_DEPLOYMENT_TARGET=${macosx_deployment_target} swift build" }`,
		`if {${os.platform} eq "darwin" && [vercmp $macosx_deployment_target 10.12] < 0} {
    configure.args-append --without-clock_gettime
    configure.env-append MACOSX_DEPLOYMENT_TARGET=${macosx_deployment_target}
}`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { foreach f {a b} { configure.args-append $f } } elseif {${os.version} eq "1"} { ui_msg x } else { build.env-append A=1 }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} then { if {${os.platform} eq "darwin"} { depends_lib-append port:x } }`,
		`ui_msg "targeting ${macos_version}"`,
		`foreach re [list "s/A/$a/" "s/\$(MACOSX_DEPLOYMENT_TARGET)/${macosx_deployment_target}/"] { reinplace $re ${build.dir}/Info.plist }`,
		`while {[vercmp $macosx_deployment_target 10.12] < 0} { ui_msg looping }`,
		`set target ${macosx_deployment_target}`,
		`set target ${macosx_deployment_target}; configure.args-append --target=${target}`,
		`set target ${macosx_deployment_target}; set target 10.12; distname fixture-${target}`,
		`for {set i ${macosx_deployment_target}} {$i < 3} {incr i} { ui_msg $i }`,
	} {
		t.Run(source, func(t *testing.T) {
			profiles, err := observationProfiles([]byte(source), native)
			require.NoError(t, err)
			require.Equal(t, []record.Platform{native}, profiles)
		})
	}
	for _, source := range []string{
		`set target ${macosx_deployment_target}; distname fixture-${target}`,
		`set target ${macosx_deployment_target}; set copy $target; master_sites https://example.invalid/${copy}`,
		`distname fixture-${macosx_deployment_target}`,
		`master_sites https://example.invalid/${os.version}`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { checksums sha256 aaaa }`,
		`if {[vercmp ${macosx_deployment_target} >= 15.0]} { macosx_deployment_target 14.0; distname legacy }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { configure.args-append x } else { distfiles other.tar.gz }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { if {${os.major} > 20} { version 2 } }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { foreach f {a} { set x $f }; distname fixture-$x }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} "version 1"`,
		`switch -- ${macosx_deployment_target} { 10.12 { version 1 } }`,
		`platform darwin { configure.args-append ${macosx_deployment_target}; version ${macosx_deployment_target} }`,

		`foreach target [list ${macosx_deployment_target}] { set deployment $target }; distname fixture-${deployment}`,
		`for {set i ${macosx_deployment_target}} {$i < 3} {incr i} { version $i }`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := observationProfiles([]byte(source), native)
			require.ErrorIs(t, err, errProbeInconclusive)
			require.Contains(t, err.Error(), "not modeled")
		})
	}
}

// Test-only helpers: production selects profiles through contextProfiles.
func contextBoundaries(src []byte) (map[int]bool, bool, error) {
	n, err := scanPlatformNeeds(src)
	if err == nil && (len(n.operands) > 0 || n.exhaustive) {
		err = fmt.Errorf("%w: native platform observations required", errProbeInconclusive)
	}
	return n.majors, n.arch, err
}

// observationProfiles includes the host, relevant architecture choices, and
// both sides of literal Darwin conditions. Unmodeled expressions are gaps.
func observationProfiles(src []byte, native record.Platform) ([]record.Platform, error) {
	majors, archDependent, err := contextBoundaries(src)
	if err != nil {
		return nil, err
	}
	return profilesForBoundaries(majors, archDependent, native)
}

func TestToolchainReadsAreBenignInBuildPositionsOnly(t *testing.T) {
	t.Parallel()
	require.True(t, toolchainReadsBenign([]byte(`set CFLAGS "${configure.cflags} -std=gnu99 [get_canonical_archflags cc]"
build.args CFLAGS="${CFLAGS}" CC=${configure.cc}
if {[string match macports-clang-* ${configure.compiler}]} { depends_run-append port:[string map {"macports-" ""} ${configure.compiler}] }
if {[string match macports-clang-* ${configure.compiler}]} { post-patch { reinplace "s|CC|${configure.cc}|" ${worksrcpath}/Makefile } }
`)), "git's and mrustc's toolchain reads only shape the build")
	require.False(t, toolchainReadsBenign([]byte(`if {${configure.compiler} eq "clang"} { post-extract { distfiles other.tar.gz } }`)), "extraction hooks are not judged here")
	for _, source := range []string{
		`distname fixture-${configure.compiler}`,
		`set cc ${configure.cc}; master_sites https://example.invalid/${cc}`,
		`if {${configure.compiler} eq "clang"} { distfiles other.tar.gz }`,
	} {
		require.False(t, toolchainReadsBenign([]byte(source)), source)
	}
}
