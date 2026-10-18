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
	require.Equal(t, []Change{{Kind: "build", How: "new binaries", Path: "Cargo.toml", Message: "upstream's Cargo packages and binaries change: jgenesis-dotenv added"}},
		cargoBinaries(after, project.Reading{Files: library, Programs: []string{"jgenesis", "jgenesis-dotenv"}}), "one with a main.rs is a program, only added")
}

// A [[bin]] added beside src/main.rs adds a program; the one Cargo infers
// from src/main.rs, named for the package, stays: ratty 0.6.0's ratty-mcp
// read as replacing ratty, and held (field testing at beb6e7bc). Only
// autobins = false, or a [[bin]] building src/main.rs, replaces it.
func TestABinBesideMainAddsAProgram(t *testing.T) {
	t.Parallel()
	reading := func(manifest string) project.Reading {
		return project.Reading{Files: map[string]project.File{"Cargo.toml": {Data: []byte(manifest)}}, Programs: []string{"."}}
	}
	old := reading("[package]\nname = \"ratty\"\nedition = \"2024\"\n")
	now := reading("[package]\nname = \"ratty\"\nedition = \"2024\"\n\n[[bin]]\nname = \"ratty-mcp\"\npath = \"src/bin/ratty-mcp.rs\"\n")
	require.Equal(t, []Change{{Kind: "build", How: "new binaries", Path: "Cargo.toml", Message: "upstream's Cargo packages and binaries change: ratty-mcp added"}}, cargoBinaries(old, now))
	off := reading("[package]\nname = \"ratty\"\nautobins = false\n\n[[bin]]\nname = \"ratty-mcp\"\npath = \"src/bin/ratty-mcp.rs\"\n")
	require.Equal(t, "upstream's Cargo packages and binaries change: ratty-mcp added; ratty removed", cargoBinaries(old, off)[0].Message, "autobins = false builds only the [[bin]]s")
	renamed := reading("[package]\nname = \"ratty\"\n\n[[bin]]\nname = \"rt\"\npath = \"src/main.rs\"\n")
	require.Equal(t, "upstream's Cargo packages and binaries change: rt added; ratty removed", cargoBinaries(old, renamed)[0].Message, "a [[bin]] building src/main.rs is its program")
}

// A Python project's PEP 517 backend moving is said, with the new one:
// justpath 0.1.4 moved from poetry-core to setuptools (field testing at
// 23c5e2aa).
func TestAPythonBackendMovingIsSaid(t *testing.T) {
	t.Parallel()
	reading := func(backend string) project.Reading {
		return project.Reading{Files: map[string]project.File{"pyproject.toml": {Data: []byte("[build-system]\nbuild-backend = \"" + backend + "\"\n")}}}
	}
	require.Equal(t, []Change{{Kind: "build", How: "backend", Path: "pyproject.toml", Name: "setuptools.build_meta",
		Message: "upstream's pyproject.toml moves its build backend from poetry.core.masonry.api to setuptools.build_meta"}},
		pythonBackend(reading("poetry.core.masonry.api"), reading("setuptools.build_meta")))
	require.Empty(t, pythonBackend(reading("hatchling.build"), reading("hatchling.build")))
}
