# Git-fetched ports bump through their version

Asked for on 2026-09-17 after `bump vault` was refused: "fetch customization or vendored source requires a dedicated preparer". The proposal was to bump the version field and try for a build, and that is what this does, with one check added.

## Why the version is enough

A port with `fetch.type git` has no archive and no checksums; MacPorts clones `git.url` at `git.branch` when it fetches. In the tree, 147 ports fetch this way. 109 derive `git.branch` from the version through `github.setup`, `go.setup`, or `gitlab.setup`, so the version edit moves the clone target by itself. 31 pin a literal 40-hex commit. 19 also declare checksums for an archive beside the clone. None declares `go.vendors` or `cargo.crates`.

## The rule

`planArchiveVersion` hands a git-fetched port to `planGitVersion` after the version probe: no download, no modeled contexts, since there is no archive plan for them to cover. What stands in for the checksum match is `fidelity.GitVersion`: the version moves, the revision resets, and the evaluated `git.branch` must equal the resolved tag. When the Portfile pins a literal commit, dockhand moves that one literal declaration to the resolved commit and expects `git.branch` to equal it; a pin carried through a variable is refused rather than guessed. Checksums beside the clone and generated dependency blocks keep the port unsupported, with reasons that say so. `assess` reports "Git source; the build clones git.url at git.branch" and "No checksums: the source is cloned, not downloaded". Declared patches are not checked on the host, since nothing is extracted there; the build checks them.

The build is the fetch. A wrong tag fails at MacPorts' own fetch step, a clean needs-attention; the `git.branch` check is what stops a version-only edit from building old code under a new number on a port pinned to a commit.

## Exercise

`assess vault`: ready, fetch and checksums passing with the git wording. `bump vault --diff` produced the two-line change, `go.setup github.com/hashicorp/vault 2.1.1 v` and `revision 0`, and reported "vault is fetched with git; the build clones https://github.com/hashicorp/vault.git at v2.1.1". The real `bump vault` prepared the branch, built vault 2.1.1 from a clone in Tart on macOS 26 (Tahoe) arm64 in about nine minutes, passed, and published https://github.com/macports/macports-ports/pull/34741. Exit 0, first run.

## Tests

`portedit`: a `git.branch v${version}` port bumps with no download and lands on the tag; a literal commit pin moves to the resolved commit, a variable-carried pin and a release without a commit are refused; a branch that does not follow the version is a fidelity failure; an archive beside the clone is refused. The portedit, fidelity, assess, workflow, and cli suites pass.
