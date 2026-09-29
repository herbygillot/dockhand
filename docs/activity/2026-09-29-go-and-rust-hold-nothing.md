# 2026-09-29: Go and Rust dependencies hold nothing

The person decided D9 on 2026-09-29. A Go module or a Rust crate is compiled into what the port builds, and a check builds with only what the port declares, so one that needs a library the port doesn't declare fails the check. What go.mod and Cargo.toml change no longer holds a submission nobody reviews.

- **Counted.** What a Go or Rust manifest gained, lost, and moved is one line, "upstream: go.mod: 2 added, 1 dropped, 4 moved", which holds nothing. The same holds for what couldn't be read of one, a manifest that doesn't parse or a file past the 1 MiB the comparison reads: it's said, and holds nothing. What counts as a change is as before (`dependencyDeltas`): a module promoted from indirect, or demoted to it, without its version moving is nothing.
- **A crate that links a native library** is listed on its own line, without holding. By Cargo's convention, a package named `foo-sys` links the native library `foo`, and often links a copy it finds installed, building one it bundles otherwise ("The `*-sys` Packages", The Cargo Book). A clean check can't tell those apart. So each crate new to the upstream Cargo.lock that links one, direct or transitive, is listed: "Cargo.lock adds libgit2-sys 0.18.1, which links the native library libgit2: MacPorts may provide it, for the Portfile to declare, rather than the crate linking whatever copy it finds".
  - The lock is read with `macports/dependency`'s reader, and `CargoPackage.NativeLibrary` says what a package links by its name, `_sys` included, since crates.io treats both spellings alike.
  - A lock that can't be read is said, and holds nothing.
  - A source with no Cargo.lock at its top has its Cargo.toml's changes counted, with no crate listed.
- **Python and Node** are as they were. A new dependency is another port, found when the software runs, which a build doesn't prove, so it holds.

The pull request says none of this: its description never carried the comparison. The guide's account of `update`, and the design's `submit --passing` example and serve's hold rule, now say so.

Tests:
- `TestGoAndRustDependenciesAreCountedAndHoldNothing` and `TestANewCrateLinkingANativeLibraryIsListed`, in `sourcecompare`;
- `TestACargoPackageNamedForANativeLibraryLinksIt`, in `macports/dependency`;
- the earlier readings' tests, which now pin their counts, and the engine's update test, whose go.mod line no longer holds.

Sixteen mutations each fail a test.
