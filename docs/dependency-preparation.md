# Go and Rust dependency preparation

`bump` and `bump --diff` can regenerate literal `go.vendors`, `cargo.crates`, and supported `cargo.crates_github` declarations. The existing preparation contract still produces one candidate Portfile, source tree, diff, and contribution commit. Verification and publication use their usual paths.

## Optional host tools

- Go: `go2port`, selectable with `--go2port` or `GO2PORT_BIN`.
- Rust: `cargo2port`, selectable with `--cargo2port` or `CARGO2PORT_BIN`.

Flags override environment variables; absent a configured path, Dockhand searches PATH. Setup reports availability without installing either helper or failing because it is absent. A preparation requiring a missing helper fails before downloading or editing its source. A nonzero helper exit is reported separately, with its diagnostic output.

## Preparation contract

Dockhand first checks that evaluated declarations correspond to a single literal declaration per block. It temporarily removes those declarations in a frozen tree to separate the main archive from dependencies appended by PortGroups. This path currently requires one main source archive; ordinary source preparation supports multiple archives separately.

Before changing a block, Dockhand regenerates the old source's dependencies and compares them to the existing declaration. Differences stop preparation so maintained overrides are not silently replaced. Unchanged dependency blocks retain their original formatting, and a changed block keeps the maintained layout: rows whose values did not change are reused byte for byte, and changed or new rows are placed on the same columns the existing rows use, so a bump that moves a dozen crates produces a dozen-line diff rather than a rewritten block. Ordering and Go field ordering may differ; changed repository choices, versions, or hashes are treated as overrides. See the [layout report](activity/2026-09-17-dependency-block-layout.md).

The selected new main archive is downloaded and hashed once, with the same bytes retained temporarily for reading its manifest. Manifest reading supports tar, gzip, bzip2, and zip without extracting source files or executing source code. It rejects ambiguous, duplicate, oversized, or nonregular manifest members. A declared source subdirectory can select a nested manifest. Temporary archives and helper directories are removed when preparation returns.

Only dependency declarations from helper output are parsed and applied. A helper's generated Portfile is never evaluated or substituted for the original. Source version/checksum edits use the existing preparation path. The final complete Portfile is reevaluated, generated options are checked against the intended declarations, sibling ports are checked for unexpected changes, and the upstream reference is rechecked. No branch integration occurs on failure.

## Go

Dockhand reads `go.mod` from the main archive, confirms its module matches `go.package`, and invokes `go2port get` with the source tag and manifest subdirectory. Every requirement must appear exactly once with the expected version or pseudo-version revision and a SHA-256 checksum. This catches helpers that exit successfully after silently omitting dependencies.

A Go port without a vendor declaration does not acquire a helper requirement merely by using the Go PortGroup. Explicit empty declarations are checked for newly added dependencies. Go workspaces and `replace` or `exclude` directives require manual preparation because the current helper does not preserve their meaning.

## Rust

Dockhand passes the exact archive's `Cargo.lock` to `cargo2port`. Registry triples must match the lockfile's crates.io package names, versions, and checksums exactly; missing packages, alternative registries, and incomplete output are refused. Added and removed dependencies, including an empty resulting block, are supported.

Git dependencies must come from HTTPS GitHub repositories with a full commit hash. Dockhand records each crate's selector (branch, tag, `rev`, or the default branch) as part of its source identity. Only branch selectors can be declared: the cargo PortGroup writes the declared value as a `branch` into Cargo's source replacement, which matches the lockfile only for branch-qualified sources. What happens to the rest follows the port's own build mode, read from `cargo.offline_cmd`:

- An offline build (the default `--frozen`) must declare every Git crate, so a tag, `rev`, or default-branch selector is refused. The error names the crate and its selector and the two ways forward: a branch-qualified source upstream, or an empty `cargo.offline_cmd`.
- A port that empties `cargo.offline_cmd` resolves Git sources online at build time, which is the maintained workaround in the tree (Codex, mcfly). Crates the PortGroup cannot declare are left to that resolution and reported by name and commit during preparation; no archive is downloaded for them. If the port also declares `cargo.crates_github`, branch-qualified crates are still declared there. If it declares none, no declaration is added, preserving the maintainer's shape.

Non-GitHub repositories, combined selectors, and conflicting branches for one declared repository still require manual preparation. For declared crates, Dockhand evaluates provisional declarations to obtain the PortGroup's actual archive locations, downloads those archives, and fills their SHA-256 checksums. It does not invent a second GitHub archive URL convention. `cargo2port` ignores Git sources, so the registry comparison is unaffected. See the [Git reference report](activity/2026-09-16-cargo-git-references.md).

`cargo.dir` must stay inside the source archive. `cargo.update` must be disabled. Local patches or recognized preparation hooks that edit dependency manifests require manual preparation; otherwise the upstream lockfile would not describe what the port builds. Dynamic or overridden dependency declarations are likewise refused.

## Validation

Default tests use controlled archives and executable helpers, plus native MacPorts evaluation where available. To exercise installed helpers and an upstream Go module archive:

```sh
DOCKHAND_TEST_DEPENDENCY_HELPERS=1 go test ./internal/macports/dependency -run TestInstalledHelpers -v
```

This test requires network access for the Go example. It does not create a branch, start a VM, or publish a pull request. Verification builds remain necessary for real updates; generating a consistent dependency block is not build evidence.
