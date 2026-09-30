package project

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A Cargo.lock is read once, for creating a port and for updating one:
// each package with where it comes from, and a lock that isn't one refused.
// (The private-helper review of 2026-09-28, finding 4.)
func TestACargoLockIsReadWithWhereEachPackageComesFrom(t *testing.T) {
	sum := strings.Repeat("a", 64)
	lock := func(packages ...string) []byte {
		return []byte("version = 4\n" + strings.Join(packages, "\n"))
	}
	pkg := func(name, source, checksum string) string {
		text := "[[package]]\nname = \"" + name + "\"\nversion = \"1.0.0\"\n"
		if source != "" {
			text += "source = \"" + source + "\"\n"
		}
		if checksum != "" {
			text += "checksum = \"" + checksum + "\"\n"
		}
		return text
	}
	packages, err := ReadCargoLock(lock(pkg("own", "", ""), pkg("serde", "registry+https://github.com/rust-lang/crates.io-index", sum),
		pkg("tokio", "sparse+https://index.crates.io/", sum), pkg("private", "registry+https://example.org/index", sum),
		pkg("fork", "git+https://github.com/o/fork?branch=main#abc", "")))
	require.NoError(t, err)
	var sources []string
	for _, p := range packages {
		sources = append(sources, p.Name+" "+string(p.Source)+" "+p.Origin)
	}
	require.Equal(t, []string{"own local ", "serde crates.io registry+https://github.com/rust-lang/crates.io-index", "tokio crates.io sparse+https://index.crates.io/",
		"private registry registry+https://example.org/index", "fork git git+https://github.com/o/fork?branch=main#abc"}, sources)

	for _, c := range []struct{ lock, err string }{
		{string(lock(pkg("serde", "registry+https://github.com/rust-lang/crates.io-index", ""))), "registry crate serde has no valid checksum"},
		{string(lock(pkg("serde", "sparse+https://index.crates.io/", sum), pkg("serde", "registry+https://github.com/rust-lang/crates.io-index", sum))), "duplicate registry crate serde 1.0.0"},
		{string(lock(pkg("bad name", "", ""))), "invalid Cargo.lock package"},
		{string(lock(pkg("odd", "path+/somewhere", ""))), "crate odd comes from a source Cargo.lock doesn't define: path+/somewhere"},
		{"[[package]]\nname = \"x\"\nversion = \"1.0.0\"\n", "unsupported or empty Cargo.lock"},
	} {
		_, err := ReadCargoLock([]byte(c.lock))
		require.ErrorContains(t, err, c.err)
	}
}

// A package named for a native library links it, by Cargo's convention:
// foo-sys links foo, in either spelling crates.io treats alike.
func TestACargoPackageNamedForANativeLibraryLinksIt(t *testing.T) {
	for name, library := range map[string]string{"libgit2-sys": "libgit2", "openssl-sys": "openssl", "onig_sys": "onig", "serde": "", "sys": "", "-sys": "", "sysinfo": ""} {
		require.Equal(t, library, CargoPackage{Name: name}.NativeLibrary(), name)
	}
}

// A Cargo.toml's package, binaries, and dependencies are read as Cargo
// declares them: each table in its order, a Git source with what pins it,
// and a table naming none of a version, a source, a path, or the
// workspace refused.
func TestACargoManifestIsReadAsCargoDeclaresIt(t *testing.T) {
	manifest, err := ReadCargoManifest([]byte(`[package]
name = "txt"
license = "MIT OR Apache-2.0"
description = { workspace = true }

[[bin]]
name = "a"

[dependencies]
serde = "1"
fork = { git = "https://example.org/fork", rev = "abc", version = "2", optional = true }

[dev-dependencies]
serde = "1.1"

[target.'cfg(unix)'.dependencies]
nix = { path = "../nix" }

[workspace.dependencies]
shared = { workspace = true }
`))
	require.NoError(t, err)
	require.Equal(t, &CargoPackageInfo{Name: "txt", License: "MIT OR Apache-2.0"}, manifest.Package)
	require.Equal(t, []string{"a"}, manifest.Bins)
	require.Equal(t, []CargoDependency{
		{Name: "fork", Table: "dependencies", Version: "2", Git: "https://example.org/fork", Pin: "rev", At: "abc", Optional: true},
		{Name: "serde", Table: "dependencies", Version: "1"},
		{Name: "serde", Table: "dev-dependencies", Version: "1.1"},
		{Name: "nix", Table: "target.cfg(unix).dependencies", Path: "../nix"},
		{Name: "shared", Table: "workspace.dependencies", Workspace: true},
	}, manifest.Dependencies)

	_, err = ReadCargoManifest([]byte("[dependencies]\nodd = { optional = true }\n"))
	require.ErrorContains(t, err, "odd: neither a version, a Git source, a path, nor the workspace's")
	_, err = ReadCargoManifest([]byte("dependencies = 1\n"))
	require.ErrorContains(t, err, "[dependencies] isn't a table")
}
