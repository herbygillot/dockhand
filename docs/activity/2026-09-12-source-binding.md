# Source binding and MacPorts evaluation — 2026-09-12

## Implemented

- Added literal local-branch resolution and whole-tree materialization to `internal/git`. Snapshot files come directly from Git blobs through one batch reader; archive attributes and checkout filters cannot omit or rewrite source. Executable bits and internal symlinks are retained. Snapshot cleanup is owned by the returned handle, and failed materialization cleans up its temporary directory. No branches, index entries, checkout files, or source pins are changed.
- Added `macports.Tree` and bound target contexts, real port resolution, and a Tcl evaluator using the installed MacPorts runtime. Supported selections are a snapshot-relative `category/port`, `category/port/Portfile`, or a unique directory name, plus an explicit subport and variants. Evaluation reads the snapshot's Portfile, PortGroups, and auxiliary files. The configured default resource source is also the snapshot, preventing fallback to unrelated installed resources.
- Required metadata includes name, evaluated version, revision, and epoch. Dependencies retain their phase, provider port, and original MacPorts dependency specification. Optional metadata reads expose failures through `PortInfo.OptionErrors`. Top-level snapshots include all subports and fail if any context cannot evaluate; explicit subport selection evaluates only that context.
- Added `workflow.Engine.BindVerification`, which captures branch-state preconditions, resolves the commit, materializes and evaluates it, and returns a request and diagnostic metadata. Git and Tcl operations happen outside state transactions. Binding creates no workflow records and removes its temporary snapshot before returning.
- Extended existing `Submit` intake with optional branch adoption. Change/revision adoption, accepted request, and job are committed atomically. An unchanged source reuses its revision; a correction or rewritten commit creates a successor under the same open change. Concurrent advancement rejects stale preconditions. Identical retries use the original bound request and return the original receipt even after the branch advances.
- Added a repository-scoped open-change lookup using SQLite's existing active-branch index. No table, migration, library, or additional package was needed. `app` injects Git and MacPorts into the existing engine.

## Provenance

The new Git materializer, branch-binding API, intake adoption, source contexts, resolution, decoders, and tests were authored for v2. The Tcl bridge adapts v1's MacPorts `mportopen`/`mportinfo`/worker-option/`mportclose` mechanism to the existing v2 RPC transport. No v1 comments or tests were copied. The existing v2 process/RPC/syntax code is reused.

## Validation

New tests cover raw object fidelity despite Git export attributes, dirty-checkout isolation, resource files, executable modes, internal and invalid symlinks, missing branches/objects, explicit subports, variants, calculated Tcl versions, missing snapshot PortGroups, failed subport evaluations, optional-field errors, platform mismatch, and process cancellation. Workflow tests cover state-free binding, original-source preservation after branch movement, stable retry receipts, same-source reuse, successor revisions, competing branch acceptance, rollback after a request write fails, and repository mismatch. State tests check same-named branch lookup in different repositories and exclusion of closed changes.

`go test -race ./... -timeout 120s`, `go vet ./...`, and `go build ./...` passed. The affected Git, MacPorts, and workflow packages were checked again with the race detector after the final edge-case tests were added. Whitespace checks passed. MacPorts integration tests run when `port-tclsh` is installed and skip elsewhere. An opt-in integration check binds real ports from a selected repository and stores accepted work only in a temporary database:

```sh
DOCKHAND_TEST_PORTS_REPO=/Users/herby/Source/macports-ports \
DOCKHAND_TEST_PORTS_BRANCH=master \
go test -v ./internal/workflow -run TestVerificationBindingAgainstPortsTree -timeout 180s
```

The real-tree run passed at commit `11f22962ec196b85d04fd400eb13d7c1751b5c47`, tree `561db7774a42dc87255b714b3950786b263cf972`, on Darwin 25 / arm64. It resolved jq 1.8.2 and Terraform 1.16.0, evaluated all 23 Terraform subports (including calculated versions), and accepted Terraform's bound request. No optional metadata errors occurred. The combined run took about 35 seconds, including two independent whole-tree materializations; it is a correctness check, not a materialization benchmark.

Live tests caught a subport-identity bug in the first bridge: reading the Portfile's `name` option would overwrite MacPorts' selected subport name. They also exposed lazy optional-option failures and macOS temporary-directory symlink normalization. Those cases were corrected and covered by the passing tests.

## Limits and next work

Binding selects committed branch contents. It does not include staged, unstaged, or untracked files. The eventual command syntax and working-tree edit policy remain unresolved. Broad selector expansion, global subport-name lookup, arbitrary PortIndex names, and cross-platform simulation are deferred. Native platform selection must match the MacPorts installation's OS platform, OS major version, and build architecture.

The first materializer rejects submodules and external, dangling, or cyclic symlinks. It materializes the complete tree on each bind and uses fresh evaluator sessions; caching and pooling remain possible later improvements. The source identity describes the Git input; MacPorts and Portfiles still execute against the real local host environment. Diagnostic metadata paths are temporary, not retained artifacts.

Real Tart builds, CLI action execution, preparation, publication, and persistent residency remain unfinished. The new workflow API can supply genuine accepted inputs to the existing verification cycle with an injected provider. No VM build or PR publication was performed, and the existing user ports checkout was not modified.
