package newport

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

const lock = `version = 4

[[package]]
name = "rift"
version = "0.4.2"

[[package]]
name = "serde"
version = "1.0.210"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a"

[[package]]
name = "anyhow"
version = "1.0.89"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6"

[[package]]
name = "local"
version = "0.1.0"
source = "git+https://example.org/local#abc"
`

func TestARustProjectGetsCargoAndItsCrates(t *testing.T) {
	files := map[string][]byte{"Cargo.toml": []byte("[package]\n"), "Cargo.lock": []byte(lock), "README.md": nil}
	build := Detect(files)
	require.Equal(t, Build{System: "cargo", Evidence: "Cargo.toml"}, build)
	crates, unfetched, err := CargoCrates(files["Cargo.lock"])
	require.NoError(t, err)
	require.Equal(t, []Crate{{"anyhow", "1.0.89", "86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6"},
		{"serde", "1.0.210", "c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a"}}, crates)
	require.Equal(t, []string{"local comes from Git, https://example.org/local#abc; cargo2port writes its cargo.crates_github"}, unfetched,
		"a Git crate is named, not dropped")

	prefix, version, ok := SplitTag("v0.4.2")
	require.True(t, ok)
	spec := Spec{Name: "rift", Category: "textproc", Owner: "rift-dev", Project: "rift", Version: version, TagPrefix: prefix,
		Description: "Fast structural diff for config files", License: "MIT", Maintainer: "{@ada example.org:ada} openmaintainer", Build: build, Binaries: []string{"rift", "rift-lsp"}, Crates: crates}
	require.Equal(t, Modeline+`

PortSystem          1.0
PortGroup           github 1.0
PortGroup           cargo 1.0

github.setup        rift-dev rift 0.4.2 v
github.tarball_from archive
revision            0

categories          textproc
# dockhand: unconfirmed, from the forge's license detection
license             MIT
maintainers         {@ada example.org:ada} openmaintainer

description         Fast structural diff for config files
# dockhand: unconfirmed, write a longer description
long_description    {*}${description}

checksums           ${distname}${extract.suffix} \
                    rmd160  0 \
                    sha256  0 \
                    size    0

# dockhand: unconfirmed, installs the programs the manifest names; add what else the port should install
destroot {
    xinstall -m 0755 \
        ${worksrcpath}/target/[cargo.rust_platform]/release/${name} \
        ${destroot}${prefix}/bin/
    xinstall -m 0755 \
        ${worksrcpath}/target/[cargo.rust_platform]/release/rift-lsp \
        ${destroot}${prefix}/bin/
}

cargo.crates \
    anyhow  1.0.89   86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6 \
    serde   1.0.210  c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a
`, string(Write(spec)))
	require.Equal(t, []string{"license", "long_description", "destroot"}, spec.Unconfirmed())
}

// A crate from another registry isn't taken for a crates.io crate, whose
// name, version, and checksum are all cargo.crates keeps: the Portfile
// marks it instead. crates.io's sparse index is crates.io's. (The
// private-helper review of 2026-09-28, finding 4.)
func TestACrateCargoCratesCantFetchIsMarked(t *testing.T) {
	crates, unfetched, err := CargoCrates([]byte(`version = 4
[[package]]
name = "private-crate"
version = "1.0.0"
source = "registry+https://example.org/index"
checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

[[package]]
name = "serde"
version = "1.0.210"
source = "sparse+https://index.crates.io/"
checksum = "c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a"
`))
	require.NoError(t, err)
	require.Equal(t, []Crate{{"serde", "1.0.210", "c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a"}}, crates)
	require.Equal(t, []string{"private-crate 1.0.0 comes from another registry, registry+https://example.org/index, which cargo.crates can't fetch"}, unfetched)
	spec := Spec{Name: "tool", Category: "devel", Owner: "o", Project: "tool", Version: "1.0.0", Build: Build{System: "cargo", Evidence: "Cargo.toml"}, Crates: crates, Unfetched: unfetched}
	require.Contains(t, string(Write(spec)), "\n"+Unconfirmed+" private-crate 1.0.0 comes from another registry, registry+https://example.org/index, which cargo.crates can't fetch\n")
	require.Contains(t, spec.Unconfirmed(), "cargo.crates")
	_, _, err = CargoCrates([]byte("[[package]]\nname = \"x\"\nversion = \"1.0.0\"\n"))
	require.ErrorContains(t, err, "unsupported or empty Cargo.lock", "a lock creating reads as an update reads it")
}

func TestWhatCantBeObservedIsMarked(t *testing.T) {
	spec := Spec{Name: "py-tool", Category: "python", CategoryGuessed: true, Owner: "o", Project: "tool", Version: "2.0", Build: Detect(map[string][]byte{"pyproject.toml": nil})}
	out := string(Write(spec))
	require.Contains(t, out, "PortGroup           python 1.0\n")
	require.Contains(t, out, "name                py-tool\n")
	require.Contains(t, out, "# dockhand: unconfirmed, guessed from the build system\ncategories          python\n")
	require.Contains(t, out, "license             unknown\n")
	require.Contains(t, out, "maintainers         nomaintainer\n")
	require.Contains(t, out, "python.versions     313\n")
	require.Equal(t, []string{"category", "license", "long_description", "maintainers", "build"}, spec.Unconfirmed())

	prefix, version, ok := SplitTag("rift-0.4.2")
	require.True(t, ok)
	require.Equal(t, "rift-", prefix)
	require.Equal(t, "0.4.2", version)
	_, _, ok = SplitTag("latest")
	require.False(t, ok)
}

// A description reaches MacPorts as written: its words, as the Portfile's
// description command reads them, whatever braces or backslashes it holds.
// (The private-helper review of 2026-09-28, finding 5.)
func TestADescriptionReachesTclAsWritten(t *testing.T) {
	tclsh := testsupport.MacPortsTclsh(t)
	descriptions := []string{"A tool for x", "Tool {x}", `Tool C:\temp`, "unbalanced } brace", "costs $5 [maybe]", `quote "it"; done`}
	var script strings.Builder
	script.WriteString("proc description args {puts [join $args { }]}\n")
	for _, description := range descriptions {
		script.WriteString("description " + tclWord(description) + "\n")
	}
	command := exec.CommandContext(t.Context(), tclsh)
	command.Stdin = strings.NewReader(script.String())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Equal(t, strings.Join(descriptions, "\n")+"\n", string(output))
	require.Equal(t, "A tool for x", tclWord("  A   tool\tfor x "), "plain words stay plain, as MacPorts writes them")
}

// A project's own manifest says its license and its one line: Cargo.toml's
// [package], pyproject.toml's [project]. What it doesn't give as a string,
// a license a Cargo workspace inherits or a PEP 621 table, is left out, as
// is a manifest that doesn't parse, and the Portfile says which it read.
func TestAProjectsManifestSaysItsLicenseAndItsLine(t *testing.T) {
	cargo := Build{System: "cargo", Evidence: "Cargo.toml"}
	python := Build{System: "python", Evidence: "pyproject.toml"}
	for _, c := range []struct {
		files    map[string][]byte
		build    Build
		declared Declared
	}{
		{map[string][]byte{"Cargo.toml": []byte("[package]\nname = \"txt\"\nlicense = \"MIT OR Apache-2.0\"\ndescription = \"A fast, intuitive terminal text editor\"\n")}, cargo,
			Declared{License: "MIT OR Apache-2.0", Description: "A fast, intuitive terminal text editor", File: "Cargo.toml"}},
		{map[string][]byte{"Cargo.toml": []byte("[package]\nlicense.workspace = true\ndescription.workspace = true\n")}, cargo, Declared{File: "Cargo.toml"}},
		{map[string][]byte{"Cargo.toml": []byte("[workspace]\nmembers = [\"a\"]\n")}, cargo, Declared{}},
		{map[string][]byte{"Cargo.toml": []byte("[package\n")}, cargo, Declared{}},
		{map[string][]byte{"pyproject.toml": []byte("[project]\nlicense = \"BSD-3-Clause\"\ndescription = \"Tools\"\n")}, python,
			Declared{License: "BSD-3-Clause", Description: "Tools", File: "pyproject.toml"}},
		{map[string][]byte{"pyproject.toml": []byte("[project]\nlicense = {text = \"MIT\"}\n")}, python, Declared{File: "pyproject.toml"}},
		{map[string][]byte{"go.mod": []byte("module x\n")}, Build{System: "go", Evidence: "go.mod"}, Declared{}},
	} {
		require.Equal(t, c.declared, Declare(c.files, c.build), "%s", c.files)
	}
	spec := Spec{Name: "txt", Category: "editors", Owner: "o", Project: "txt", Version: "0.8.1", License: "{MIT Apache-2}", LicenseFrom: "Cargo.toml", Build: cargo}
	require.Contains(t, string(Write(spec)), "# dockhand: unconfirmed, from Cargo.toml's license field\nlicense             {MIT Apache-2}\n")
}

// Neither the cargo nor the golang PortGroup installs anything, so a new
// port installs the programs its manifest names, as the tree's do:
// Cargo.toml's [[bin]] targets, else its package; the one go build makes
// at go.mod's module, a major version's suffix aside (the txt run's
// finding 2).
func TestANewPortInstallsWhatItsManifestNames(t *testing.T) {
	cargo, golang := Build{System: "cargo", Evidence: "Cargo.toml"}, Build{System: "go", Evidence: "go.mod"}
	require.Equal(t, []string{"txt"}, Binaries(map[string][]byte{"Cargo.toml": []byte("[package]\nname = \"txt\"\n")}, cargo))
	require.Equal(t, []string{"a", "b"}, Binaries(map[string][]byte{"Cargo.toml": []byte("[package]\nname = \"x\"\n[[bin]]\nname = \"a\"\n[[bin]]\nname = \"b\"\n")}, cargo))
	require.Empty(t, Binaries(map[string][]byte{"Cargo.toml": []byte("[workspace]\nmembers = [\"a\"]\n")}, cargo))
	require.Equal(t, []string{"lazysql"}, Binaries(map[string][]byte{"go.mod": []byte("module github.com/jorgerojas26/lazysql\n\ngo 1.24\n")}, golang))
	require.Equal(t, []string{"tool"}, Binaries(map[string][]byte{"go.mod": []byte("module example.org/tool/v3\n")}, golang))
	require.Empty(t, Binaries(map[string][]byte{"go.mod": []byte("go 1.24\n")}, golang))

	spec := Spec{Name: "lazysql", Category: "databases", Owner: "o", Project: "lazysql", Version: "0.5.9", TagPrefix: "v", Build: golang, Binaries: []string{"lazysql"}}
	require.Contains(t, string(Write(spec)), "destroot {\n    xinstall -m 0755 ${worksrcpath}/${name} ${destroot}${prefix}/bin/\n}\n")
	require.Contains(t, spec.Unconfirmed(), "destroot")
	spec = Spec{Name: "tool", Category: "devel", Owner: "o", Project: "tool", Version: "1.0", Build: cargo}
	require.Contains(t, string(Write(spec)), Unconfirmed+" the manifest names no program; install what the build makes in a destroot block\n")
	require.NotContains(t, string(Write(spec)), "destroot {")
	require.NotContains(t, Spec{Build: Build{System: "cmake"}}.Unconfirmed(), "destroot", "a cmake build installs what it installs")
}

// Where the person names no category, the project's description can: a
// word of it, or its plural, that is one of the tree's categories, as a
// "terminal text editor" is editors; a Python project is python's, as
// MacPorts keeps them; and otherwise the build system guesses, as before
// (the txt run's finding 1).
func TestACategoryGuessedFromTheDescription(t *testing.T) {
	categories := []string{"devel", "editors", "games", "mail", "net", "python", "textproc", "x11"}
	cargo := Build{System: "cargo", Evidence: "Cargo.toml"}
	for description, want := range map[string][2]string{
		"A fast, intuitive terminal text editor": {"editors", "its description"},
		"A mail client for the terminal":         {"mail", "its description"},
		"Networking tools":                       {"devel", "the build system"},
		"":                                       {"devel", "the build system"},
		"Games, and an editor":                   {"games", "its description"},
	} {
		category, from := GuessCategory(cargo, description, categories)
		require.Equal(t, want, [2]string{category, from}, description)
	}
	category, from := GuessCategory(Build{System: "python", Evidence: "pyproject.toml"}, "A mail client", categories)
	require.Equal(t, [2]string{"python", "the build system"}, [2]string{category, from})
	spec := Spec{Name: "txt", Category: "editors", CategoryGuessed: true, CategoryFrom: "its description", Owner: "o", Project: "txt", Version: "1", Build: cargo}
	require.Contains(t, string(Write(spec)), "# dockhand: unconfirmed, guessed from its description\ncategories          editors\n")
}
