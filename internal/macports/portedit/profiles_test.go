package portedit

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProfilesIncludeBoundaryAndArchitectureWithoutImpossibleOldARM(t *testing.T) {
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	profiles, err := observationProfiles([]byte(`if {${os.major} >= 17} {version 1} else {version 0}
if {${build_arch} eq "arm64"} {distfiles a} else {distfiles b}`), native)
	require.NoError(t, err)
	require.Equal(t, native, profiles[0])
	require.Contains(t, profiles, record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"})
	require.Contains(t, profiles, record.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"})
	require.NotContains(t, profiles, record.Platform{OS: "darwin", Version: "16", Architecture: "arm64"})
	_, err = observationProfiles([]byte(`if {${os.major} >= $minimum} {version 1}`), native)
	require.ErrorIs(t, err, ErrProbeInconclusive)
}

func TestProfilesRefuseUnresolvedReadsAlongsideKnownBoundaries(t *testing.T) {
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
			require.ErrorIs(t, err, ErrProbeInconclusive)
		})
	}
}

func TestUnmodeledReadsAreGapsOnlyWhereTheyCanSelectSources(t *testing.T) {
	native := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	for _, source := range []string{
		`configure.env-append MACOSX_DEPLOYMENT_TARGET=${macosx_deployment_target}`,
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
	} {
		t.Run(source, func(t *testing.T) {
			profiles, err := observationProfiles([]byte(source), native)
			require.NoError(t, err)
			require.Equal(t, []record.Platform{native}, profiles)
		})
	}
	for _, source := range []string{
		`set target ${macosx_deployment_target}`,
		`distname fixture-${macosx_deployment_target}`,
		`master_sites https://example.invalid/${os.version}`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { checksums sha256 aaaa }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { configure.args-append x } else { distfiles other.tar.gz }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { if {${os.major} > 20} { version 2 } }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} { foreach f {a} { set x $f } }`,
		`if {[vercmp $macosx_deployment_target 10.12] < 0} "version 1"`,
		`switch -- ${macosx_deployment_target} { 10.12 { version 1 } }`,
		`platform darwin { configure.args-append ${macosx_deployment_target}; version ${macosx_deployment_target} }`,
		`variant legacy { patchfiles-append legacy-${macosx_deployment_target}.diff }`,
		`foreach target [list ${macosx_deployment_target}] { set deployment $target }`,
		`for {set i ${macosx_deployment_target}} {$i < 3} {incr i} { ui_msg $i }`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := observationProfiles([]byte(source), native)
			require.ErrorIs(t, err, ErrProbeInconclusive)
			require.Contains(t, err.Error(), "not modeled")
		})
	}
}
