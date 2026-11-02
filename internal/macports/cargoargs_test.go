package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/project"
)

// A cargo port's features and packages are read from its evaluated build
// arguments, in each form the tree's cargo ports write them.
func TestACargoPortsBuildArgumentsAreRead(t *testing.T) {
	for _, c := range []struct {
		name    string
		options map[string]string
		want    project.CargoFeatures
	}{
		{"none", map[string]string{"build.cmd": "/opt/local/bin/cargo build", "build.pre_args": "--release --frozen -v -j8"}, project.CargoFeatures{}},
		{"gitui", map[string]string{"build.args": `{--features "vendor-openssl"}`}, project.CargoFeatures{Features: []string{"vendor-openssl"}}},
		{"taplo", map[string]string{"build.args": "--features=lsp"}, project.CargoFeatures{Features: []string{"lsp"}}},
		{"stylua", map[string]string{"build.args": "--features lua52,lua53,lua54,luajit,luau"}, project.CargoFeatures{Features: []string{"lua52", "lua53", "lua54", "luajit", "luau"}}},
		{"quoted, repeated", map[string]string{"build.args": `{--features "a b"} -F c --features=d,e`}, project.CargoFeatures{Features: []string{"a", "b", "c", "d", "e"}}},
		{"lightcraft", map[string]string{"build.args": "--locked -p lightcraft -p lightcraft-cli --no-default-features --features denoise"},
			project.CargoFeatures{Features: []string{"denoise"}, NoDefault: true, Packages: []string{"lightcraft", "lightcraft-cli"}}},
		{"all", map[string]string{"build.pre_args": "--release --all-features", "build.args": "--package=x"}, project.CargoFeatures{All: true, Packages: []string{"x"}}},
		{"codex-switcher's build.cmd", map[string]string{"build.cmd": "cargo build --release -j8 --bins --features tauri/custom-protocol"}, project.CargoFeatures{Features: []string{"tauri/custom-protocol"}}},
	} {
		require.Equal(t, c.want, CargoBuildArgs(c.options), c.name)
	}
}
