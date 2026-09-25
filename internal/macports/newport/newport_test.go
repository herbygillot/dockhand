package newport

import (
	"testing"

	"github.com/stretchr/testify/require"
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
	crates, err := CargoCrates(files["Cargo.lock"])
	require.NoError(t, err)
	require.Equal(t, []Crate{{"anyhow", "1.0.89", "86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6"},
		{"serde", "1.0.210", "c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a"}}, crates)

	prefix, version, ok := SplitTag("v0.4.2")
	require.True(t, ok)
	spec := Spec{Name: "rift", Category: "textproc", Owner: "rift-dev", Project: "rift", Version: version, TagPrefix: prefix,
		Description: "Fast structural diff for config files", License: License("MIT"), Maintainer: "{@ada example.org:ada} openmaintainer", Build: build, Crates: crates}
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

checksums           rmd160  0 \
                    sha256  0 \
                    size    0

cargo.crates \
    anyhow  1.0.89   86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6 \
    serde   1.0.210  c8e3592472072e6e22e0a54d5904d9febf8508f65fb8552499a1abc7d1078c3a
`, string(Write(spec)))
	require.Equal(t, []string{"license", "long_description"}, spec.Unconfirmed())
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

	require.Equal(t, "", License("NOASSERTION"))
	require.Equal(t, "Apache-2", License("Apache-2.0"))
	prefix, version, ok := SplitTag("rift-0.4.2")
	require.True(t, ok)
	require.Equal(t, "rift-", prefix)
	require.Equal(t, "0.4.2", version)
	_, _, ok = SplitTag("latest")
	require.False(t, ok)
}
