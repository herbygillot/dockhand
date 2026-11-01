package portfile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// mindforger 2.0.0, fetched with Git for its submodules from a
// github.tarball_from tarball port, moves to its release's
// mindforger_2.5.0_tarball.tgz as #35195 moved it, less the comment it
// wrote (the roadmap's D3): the release mode, the block after the setup
// lines, the checksums for the refresh to fill, and fetch.type git and
// the submodule hook gone.
func TestAGitPortMovesToItsReleaseAssetAsMindforgerDid(t *testing.T) {
	before, err := os.ReadFile("testdata/source_move/mindforger-2.0.0")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/source_move/mindforger-2.0.0-moved")
	require.NoError(t, err)
	moved, ok := MoveSource(before, SourceMove{Mode: "releases", Distname: "${name}_${version}_tarball", Suffix: ".tgz", Worksrcdir: "ci_${name}_${version}", FromGit: true})
	require.True(t, ok)
	require.Equal(t, string(want), string(moved))
}

// A .tbz asset takes use_bzip2 for its compressor and extract.suffix for
// its name, which bzip2's default, .tar.bz2, isn't.
func TestATbzAssetWritesUseBzip2AndItsSuffix(t *testing.T) {
	src := []byte(`PortSystem          1.0
PortGroup           github 1.0

github.setup        owner tool 1.2.0 v
# Change github.tarball_from to 'releases' or 'archive' next update
github.tarball_from tarball
revision            0
categories          devel
`)
	moved, ok := MoveSource(src, SourceMove{Mode: "releases", UseOption: "use_bzip2", Suffix: ".tbz"})
	require.True(t, ok)
	require.Equal(t, `PortSystem          1.0
PortGroup           github 1.0

github.setup        owner tool 1.2.0 v
github.tarball_from releases
revision            0

use_bzip2           yes
extract.suffix      .tbz

categories          devel
`, string(moved))
}

// An asset the PortGroup's default names, tool-1.2.0.tar.gz, is one line:
// the mode.
func TestADefaultNamedAssetWritesOneLine(t *testing.T) {
	src := []byte(`github.setup        owner tool 1.2.0 v
github.tarball_from tarball
revision            1
`)
	moved, ok := MoveSource(src, SourceMove{Mode: "releases"})
	require.True(t, ok)
	require.Equal(t, `github.setup        owner tool 1.2.0 v
github.tarball_from releases
revision            1
`, string(moved))
}

// What a move doesn't make: a mode set in an if or a subport's block, a
// Git port with a hook of its own, a pinned checksums beside its clone,
// and archive asked of a port setting no mode.
func TestAMoveIsTheMaintainersWhereItIsntAtTheTop(t *testing.T) {
	for name, src := range map[string]string{
		"in an if": `github.setup owner tool 1.0
if {${os.major} > 20} {
    github.tarball_from tarball
}
`,
		"subport": `github.setup owner tool 1.0
subport tool-devel {
    github.setup owner tool 2.0
}
`,
		"custom hook": `github.setup owner tool 1.0
fetch.type git
post-fetch {
    system -W ${worksrcpath} "git submodule update --init"
    system -W ${worksrcpath} "./bootstrap"
}
`,
		"checksums": `github.setup owner tool 1.0
fetch.type git
checksums sha256 abc
post-fetch {
    system -W ${worksrcpath} "git submodule update --init"
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := MoveSource([]byte(src), SourceMove{Mode: "releases", FromGit: name != "in an if" && name != "subport"})
			require.False(t, ok)
		})
	}
	_, ok := MoveSource([]byte("github.setup owner tool 1.0\n"), SourceMove{Mode: "archive"})
	require.False(t, ok)
}

// A Git port with no github.tarball_from gets none for releases, the
// default, and a hook's comment above it goes with it.
func TestAGitPortWithNoModeGetsNone(t *testing.T) {
	src := []byte(`github.setup        owner tool 1.0 v

fetch.type          git

# Fetch the submodules.
post-fetch {
    system -W ${worksrcpath} "git submodule update --init --recursive"
}

destroot {}
`)
	moved, ok := MoveSource(src, SourceMove{Mode: "releases", FromGit: true})
	require.True(t, ok)
	require.Equal(t, `github.setup        owner tool 1.0 v

checksums           rmd160  0 \
                    sha256  0 \
                    size    0

destroot {}
`, string(moved))
}
