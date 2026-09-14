# Live README exercise with croc

Exercised the contributor flow against the local MacPorts checkout, using croc's update from 11.5.1 to upstream v11.5.3. The checkout remained on master; existing untracked port directories were excluded and preserved. The source snapshot was commit `412bc354a94c3917b84165c50d6e0a5b098f6672`.

## Findings and fixes

1. The initial automatic preview failed with `macports: invalid version comparison`. MacPorts `vercmp` can return a character difference (2 for 11.5.3 versus 11.5.1), not just -1/0/1. The MacPorts adapter now normalizes the parsed result's sign before returning its existing selection contract. A native regression test failed before the fix and covers newer, equal, and older versions afterward.
2. A freshly provisioned image passed setup validation but verification blocked on `image manifest does not match observed capabilities`. Setup wrote the requested guest-agent release, 0.14.1; the installed binary identified itself as 0.14.1-cb39b12. Setup now validates the installed environment before writing its manifest and records the observed full agent version. Its regression test failed before the fix. Verification's exact comparison remains intact.
3. The CLI rejected `verify` without `--image` before reaching the shared resolver that already selects setup's default base/Xcode image. Removed that obsolete guard and updated the argument-validation test. A CLI integration regression test reproduces the original error, then verifies default image selection and evidence reuse without an image override.
4. Candidate PortIndex generation failed on unchanged `lang/cabal` and `lang/ghc`, which were already omitted from the baseline index. Incremental indexing still requests parse-error exit status, but now permits that status only after checking that all changed existing directories and their declared subports are indexed and that no previously indexed unchanged port disappeared. Native regressions cover unrelated existing failures, changed-port failures, failed subports, removed ports, and full strict indexing. Full/shared-resource rebuilds remain strict.
5. Initial image inspection during bump provides no progress beyond “Binding committed source,” which can look like a stall. This remains a usability follow-up.
6. Read-only status initially rejected the older local database schema. A backup was taken through `db backup`; normal writable service opening migrated it successfully, after which status worked. The initial schema error could explain the required writable opening more clearly.

## Run record

- `make build` succeeded.
- `setup --rebuild` provisioned the default Tahoe image; plain `setup` validated reuse in a disposable clone.
- After the comparator fix, `bump croc --diff` and `bump croc 11.5.3 --diff` produced identical patches, including the inferred v prefix and new archive checksums.
- `bump croc --wait` created one contribution commit, `croc: update to 11.5.3`, changing only `net/croc/Portfile`. Its first verification exposed the manifest mismatch before the port build ran.
- A publication dry-run correctly refused to publish without passing evidence. This checkout uses `herby` for the contributor fork and `origin` for MacPorts, so the dry-run used `--remote herby --upstream origin` instead of changing those remotes.

The default image was reprovisioned successfully with the manifest fix. The successful verification used automatic image selection and the incremental-index fix against the same prepared branch:

```sh
dockhand --tree ~/Source/macports-ports verify croc \
  --branch dockhand/bump/croc-vu3oipeiqrz4jkreamwx3oi5kx --trace
# Ctrl-C detached while the guest was working.
dockhand --tree ~/Source/macports-ports wait job_OHUD4G6VKUNFRODOG5GWQV64BF --trace
```

- Job `job_OHUD4G6VKUNFRODOG5GWQV64BF` completed with passing attempt `attempt_DKPODTL7IS2V4OPDY5P26IKJZ2`. The attempt remained running after detachment and the reattached command exited successfully. Because the initial command was piped through `tee`, its interrupted pipeline exit status is not evidence of Dockhand's standalone interrupt exit code.
- Lint reported zero errors and warnings. croc built from source, installed, and passed the final link scan on Darwin 25 arm64 with MacPorts 2.12.6. Available Go dependency archives were used. The port declared no test phase, so the executed phases were lint, build, and install; no upstream test-suite pass is claimed.
- Verification used image digest `sha256:29c1d4bd505c406292d1caba5d7374d01ee3073cf5035b448c18c3d045a63968`. Its disposable VM was released after success. Earlier retained failure diagnostics and unrelated images were left alone.
- Publication preview selected the passing attempt and the intended fork/upstream. A check found no competing open croc update PR.
- The first actual publication preflight rejected Dockhand's saved credential with HTTP 401 before accepting a publication job or pushing. No token environment overrides were set, while `gh api user` succeeded as `herbygillot`. For the retry, the working `gh auth token` result was passed privately through the child process's `GH_TOKEN`; neither credential storage nor credential precedence was changed. This exposes a usability follow-up for identifying and replacing rejected saved credentials rather than silently falling through to another identity.

With the user's explicit authorization, Dockhand then ran:

```sh
dockhand --tree ~/Source/macports-ports publish \
  --branch dockhand/bump/croc-vu3oipeiqrz4jkreamwx3oi5kx \
  --remote herby --upstream origin --wait
```

Publication job `job_VXX5P324FYCOK5GXK5LYINCMCW` completed and confirmed [MacPorts PR #34676: croc: update to 11.5.3](https://github.com/macports/macports-ports/pull/34676). An independent read of the PR confirmed it is open against `master`, from `herbygillot`, with exactly commit `3a453405bf7feb14010eb0c14df531b19743d614` and only `net/croc/Portfile` changed (four insertions and four deletions). The local checkout stayed on `master`, with its existing `aqua/rockxy/` and `editors/txt/` untracked directories untouched.

## Validation

The native regression tests reproduced their failures before the corresponding fixes. Afterward, targeted package tests, the full `go test ./... -count=1` suite, `go vet ./...`, and `git diff --check` passed. Race checks passed for `internal/macports` and `internal/tart/provision`. `make build` produced the binary used for the successful live verification and publication.

This exercise covered provisioning/reprovisioning, image reuse validation, automatic and explicit-version previews, already-current handling, preparation, verification recovery, detachment/reattachment, publication preflight, and PR creation. It did not exercise cancellation, full Xcode provisioning, or other macOS/architecture combinations.

All fixes and regression tests were authored for this exercise. The README rewrite and these follow-up changes remain uncommitted for review. Full command logs and the pre-migration database backup are local test artifacts under `/private/tmp/dockhand-readme-croc-20260914/`, outside the repository.
