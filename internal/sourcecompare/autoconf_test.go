package sourcecompare

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/project"
)

// A configure.ac's change says what it asks of the build: the options,
// pkg-config modules, and libraries it adds or drops (field testing,
// batch 11: dateutils).
func TestAConfigureAcChangeSaysWhatItAsks(t *testing.T) {
	t.Parallel()
	old := project.ReadAutoconf([]byte("AC_ARG_ENABLE([fast-arith])\nAC_CHECK_LIB([m], [floor])\n"))
	now := project.ReadAutoconf([]byte("AC_ARG_ENABLE([fast-arith])\nAC_ARG_ENABLE([contrib])\nPKG_CHECK_MODULES([TZ], [libtzdb])\n"))
	require.Equal(t, ": --enable-contrib added; pkg-config module libtzdb added; library m removed", autoconfWords(old, now))
	require.Equal(t, ", in nothing it names as an option, a pkg-config module, or a library", autoconfWords(old, old))
}

// A Makefile.am's -version-info moving anywhere in the source is a new
// library version, said by its file (field testing, batch 12: libunibreak).
func TestAMovedLibtoolVersionIsSaid(t *testing.T) {
	t.Parallel()
	changes := libraryVersions(map[string]string{"src/Makefile.am": "7:0:0", "tools/Makefile.am": "1:0:0"}, map[string]string{"src/Makefile.am": "8:0:0", "tools/Makefile.am": "1:0:0", "new/Makefile.am": "1:0:0"})
	require.Equal(t, []Change{{Kind: "build", How: "library", Path: "src/Makefile.am", Message: "upstream's src/Makefile.am moves its library's -version-info from 7:0:0 to 8:0:0, a new library version"}}, changes)
}

// The programs a Cargo project builds changing is said: jgenesis 0.14.0
// merged jgenesis-cli and jgenesis-gui into one jgenesis (field testing,
// batch 12).
func TestACargoProjectsChangedBinariesAreSaid(t *testing.T) {
	t.Parallel()
	old := map[string]project.File{
		"Cargo.toml":              {Data: []byte("[workspace]\nmembers = [\"jgenesis-cli\", \"jgenesis-gui\"]\n")},
		"jgenesis-cli/Cargo.toml": {Data: []byte("[package]\nname = \"jgenesis-cli\"\n")},
		"jgenesis-gui/Cargo.toml": {Data: []byte("[package]\nname = \"jgenesis-gui\"\n")},
	}
	now := map[string]project.File{
		"Cargo.toml":          {Data: []byte("[workspace]\nmembers = [\"jgenesis\"]\n")},
		"jgenesis/Cargo.toml": {Data: []byte("[package]\nname = \"jgenesis\"\n")},
	}
	before := project.Reading{Files: old, Programs: []string{"jgenesis-cli", "jgenesis-gui"}}
	after := project.Reading{Files: now, Programs: []string{"jgenesis"}}
	require.Equal(t, []Change{{Kind: "build", How: "binaries", Path: "Cargo.toml", Message: "upstream's Cargo packages and binaries change: jgenesis added; jgenesis-cli, jgenesis-gui removed"}}, cargoBinaries(before, after))
	require.Empty(t, cargoBinaries(after, after))

	// A member added with no src/main.rs, or src/bin, is a library, which
	// installs nothing a destroot names: mise's mise-dotenv (field
	// testing, batch 13).
	library := map[string]project.File{"jgenesis-dotenv/Cargo.toml": {Data: []byte("[package]\nname = \"jgenesis-dotenv\"\n")}}
	for name, file := range now {
		library[name] = file
	}
	require.Empty(t, cargoBinaries(after, project.Reading{Files: library, Programs: []string{"jgenesis"}}))
	require.NotEmpty(t, cargoBinaries(after, project.Reading{Files: library, Programs: []string{"jgenesis", "jgenesis-dotenv"}}), "one with a main.rs is a program")
}
