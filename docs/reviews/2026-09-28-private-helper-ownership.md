# Private-helper ownership review

Reviewed 2026-09-28 at `7be0dc2d992c0a84e9ac3f2fea1c27347db81bcf` (`fix(selection): a port the tree doesn't have is "no port named X"`), the latest commit when work began. The checkout advanced once during the review, to `f43228acfdb60570d134757774632383931dd5e8` (`fix(submit): an open pull request's Type(s) are dockhand's while unchanged`). That delta was also inspected; it does not change findings 1–9 and supplies finding 10. This reviews the repository cross-section, not just either small diff. The focus is facts and operations hidden in private helpers: their proper owners, duplicated interpretations, and the concepts their callers need.

## Assessment

The main problem is **domain interpretation escaping its owner**, rather than private functions being too numerous. Some helpers are small enough to look harmless but independently decide what a Tcl value, Portfile change, Cargo dependency, or archive comparison means. Several already disagree with other code in the tree.

The strongest changes are to put Portfile inspection beside Portfile editing, give source comparison an explicit completeness result, and share the Cargo lock interpretation used by creation and updates. The new archive-install work also exposes a good package boundary: MacPorts binary-archive signing is currently split between a provider and its SSH transport.

An inventory found 1,019 unexported function/method declarations in 51 package directories under `internal/`, counting platform-specific implementations separately. The largest concentrations are `engine` (200; 10,891 production Go lines), `command` (235; 7,994 lines), `macports/portedit` (76; 3,097 lines), and `buildenv/tart` (36; 1,439 lines). These counts guided inspection; they are not reasons to split packages. For example, the 289-line `newport` package has only one private function, but that function contains a demonstrably wrong language-encoding rule.

P2 below means a current behavioral discrepancy with an ownership remedy. P3 means an organizational improvement, with any observed limitation stated separately. Counts and repository-relative `path:line` citations refer to `7be0dc2d`, except finding 10, which refers to `f43228ac`. Proposed API names describe the intended boundary, not required spellings.

## Findings

### 1. [P2] Build eligibility privately redefines MacPorts option semantics

**Evidence:** `internal/engine/plan.go:335` (`ineligible`), called at `:133`; `internal/macports/metadata.go:69` (`PortInfo.Bool`).

`ineligible` knows the MacPorts CI rules for `replaced_by`, `known_fail`, and `supported_archs`, and also implements their value decoding. The boolean switch accepts `yes`, `1`, and `true`, whereas the existing `PortInfo.Bool` also accepts `on` and reports evaluation errors. The helper reads list-valued `supported_archs` with `strings.Fields`, although `PortInfo.Options` explicitly preserves Tcl list encoding. It has no error return, so unreadable metadata cannot become an unresolved plan through this path. The adjacent `use_xcode` handling already uses `Bool` and records an unresolved target on error.

**Reproduced:** `known_fail = on` is true through `PortInfo.Bool` but eligible through `ineligible`. The valid Tcl list `{arm64}` excludes an arm64 target because the helper compares the braces too. These are synthetic `PortInfo` inputs exercising the declared metadata contract; this review did not measure their frequency in the ports tree.

**Ownership:** Move this decision into `macports`, with an API such as `BuildEligibility(port, platform) (Eligibility, error)`. Use the existing boolean reader and the existing Tcl list parser. Preserve the difference between excluded and unknown; the engine maps these to exclusions and unresolved targets. A named exclusion reason can carry the replacement or supported architectures, leaving presentation outside the decoder. No new package is necessary.

### 2. [P2] The planner's source helpers need a Portfile inspection contract

**Evidence:** `internal/engine/plan.go:350`–`:445` (`revisionDeclaration`, `targetKind`, `loadsChangedSharedCode`); `internal/macports/commitrules/rules.go:160`–`:205` (`version` and revision/version regexes); `internal/engine/tidy.go:302` (`derivedSubject`). Existing syntax-aware operations are in `internal/macports/portfile/revision.go:13`, `declaration.go:15`, and `version.go:28`.

The engine decides that a change is revision-only by removing every line matching `revision <non-whitespace>` from both Portfiles. This is a textual filter, not proof that only executed revision declarations changed. **Reproduced:** changing the contents of `set contents {\nrevision 1\n}` to `revision 2` is classified as `RevisionOnly`. The edited text is data, and could be consumed by a build hook; it is not the port's revision declaration. This classification matters beyond wording: `engine.Acceptable` (`evidence.go:244`) permits failed revision-only targets to be acknowledged with `--accept`.

The same area has two more source interpreters. `loadsChangedSharedCode` reads PortGroup commands with regexes and `strings.Fields`; `commitrules.version` uses separate setup/version regexes, and tidy calls its exported wrapper. The latter is a grammar service housed in a rule checker: it lacks `go.setup`, which the editor's `portfile.Candidates` understands, and can return a substitution token despite the wrapper promising a literal-enough version. These readers have different purposes, but should not independently implement Tcl syntax.

**Ownership:** Add a conservative inspection API to `macports/portfile`, built on `tcl/syntax`. It should distinguish literal declarations, executable bodies, and data, and explicitly say when a conclusion cannot be proved. Expose operations such as a literal declared-version observation and a revision-only comparison. Engine retains tree reads, changed-path aggregation, and the final `model.TargetKind` decision; `commitrules` retains the rule that a version update resets revision. Shared-code traversal can consume parsed PortGroup references through a small file-reader callback, retaining its conservative fallback for dynamic loads. Do not turn the syntax package into a Git or MacPorts workflow package.

### 3. [P2] Source comparison hides incomplete interpretation behind empty maps

**Evidence:** `internal/archive/compare.go:40`, `:106`, `:182`, `:214`, `:235`, `:255`, `:270`; propagation through `internal/engine/update.go:526` and `internal/engine/servesubmit.go:121`.

`archive` currently combines container mechanics (`Walk`, `Extract`) with software-project semantics: build-file names, dependency-manifest grammars, license-file recognition, and the rule that certain changes hold unattended submission. Its private parsers all return only `map[string]string`. That shape cannot distinguish an empty dependency set from an unreadable or unsupported manifest.

Three probes reproduce the consequence through the public `archive.Compare` API:

- Adding `[dependencies.serde]` with `version = '1'` in Cargo.toml produces no change. The helper recognizes only sections whose name ends in `dependencies`.
- Adding a dependency to a single-quoted TOML array in pyproject.toml produces no change. The helper recognizes only double-quoted strings and does not bind its search to `[project]`.
- Replacing a package.json with malformed JSON returns an empty comparison and no error. `nodeDependencies` suppresses the decode error.

The Go helper also reimplements go.mod reading with a regex, although `golang.org/x/mod/modfile` is already used by `macports/dependency`; TOML decoding is already a dependency too. The issue is not that every ecosystem needs complete static analysis. It is that limited analysis is represented as complete. The engine already turns a comparison error into `UpstreamComparison.Problem`, which can hold an unattended submission; these helpers prevent that existing path from seeing the problem. The missing hold is a consequence traced through the code, not an end-to-end submission performed by this review.

**Ownership:** Introduce a focused `internal/sourcecompare` package for the operation of comparing upstream source, using `archive` for traversal. Give its manifest observations a status/error as well as dependencies, and preserve incomplete reads in the comparison report. Use the existing Go and TOML libraries, with explicit treatment of unsupported manifest forms and truncated members. Keep the initial scope narrow: the existing comparison operation and its parsers. Do not create a general ecosystem framework or move Portfile dependency generation into it. This is a justified new package even though `archive` is only 498 lines: its two responsibilities change for different reasons.

### 4. [P2] The private Cargo generator contains a reusable lockfile model that creation bypasses

**Evidence:** `internal/macports/dependency/cargo.go:18`–`:96` (`cargoPackage`, `cargoLock`, `generateCargo`); `internal/macports/newport/newport.go:83`–`:116` (`Crate`, `CargoCrates`) and `:226` (writing `cargo.crates`).

The update path's private `generateCargo` decodes Cargo.lock, validates package names, versions and checksums, distinguishes crates.io from unsupported registries, and retains Git sources. Creation independently decodes the same structure and collapses every `registry+...` package with a nonempty checksum into `Crate{Name, Version, Checksum}`. The source field is discarded before writing `cargo.crates`.

**Reproduced:** a lock entry from `registry+https://example.org/index` is accepted by `newport.CargoCrates` and loses its registry identity. Static inspection confirms that `generateCargo` rejects this source. Conversely, the generator explicitly recognizes `sparse+https://index.crates.io/`, which creation's prefix test skips. These are discrepancies between the project's two interpretations; no registry was contacted.

**Ownership:** Extract a typed lockfile reader from `generateCargo` into an exported operation in the existing `macports/dependency` package. Retain source kind and source identity until the caller deliberately chooses what it can represent. Creation and updates may have different policies for unsupported sources—creation can leave an explicit unconfirmed requirement—but both should receive the same parsed facts. Keep invoking cargo2port and checking its generated output inside the generator. Cargo.toml comparison in finding 3 and Cargo.lock-to-Portfile conversion are distinct operations; do not equate their dependency sets.

### 5. [P2] `newport.tclWord` is an unowned Tcl encoder

**Evidence:** `internal/macports/newport/newport.go:256`; the trusted-table-only `tclList` at `internal/macports/programs.go:113`; existing decoding in `internal/tcl/syntax/list.go:195`.

The new-port writer escapes braces and backslashes, then encloses the text in braces. Tcl's braced word retains those escapes. **Reproduced with the local Tcl interpreter:** `Tool {x}` becomes `Tool \{x\}`, and `Tool C:\temp` gains a second backslash. The writer preserves neither string exactly.

**Ownership:** Add a small, tested literal-word/list encoding API beside the existing Tcl syntax operations, and have `newport` call it after its intentional whitespace normalization. Encoding one command argument and encoding a Tcl list are separate contracts. Keep the choice to render ordinary descriptions as readable plain words in `newport`; move the language escaping rule out. Validate the encoder by round-tripping punctuation, backslashes, braces, newlines, and Unicode through Tcl. `macports.tclList` currently documents a narrower, trusted balanced-pattern input; it need not become a general encoder merely because the helper names look related.

### 6. [P3] MacPorts binary-archive signing belongs outside the SSH channel

**Evidence:** `internal/buildenv/tart/provider.go:644` (`install`), `:706` (`signRMD160`), `:712` (`signingKeys`), `:892` (`keep`); `internal/tart/channel/keys.go:70`–`:163`; `internal/buildenv/tart/guest.tcl:319`. See also the exercised protocol in [the kept-archives activity note](../activity/2026-09-28-kept-archives.md).

The new install helper understands archive-site layout, both MacPorts signature formats, public-key filenames, and the software-directory layout. Its signing keys are generated and stored by `channel.Keys.ArchiveKeys`, beside SSH private and host keys. Consequently the transport package imports RSA/X.509/PEM and signify specifically for a MacPorts distribution protocol. The provider coordinates signing, local temporary files, and SSH uploads in one loop.

There is no demonstrated signing failure here; the existing activity note documents successful guest verification. The ownership cost is concrete: changing MacPorts' signature requirements currently changes both the build provider and the SSH package, and another provider cannot use this operation without adopting Tart's SSH key abstraction.

**Ownership:** A focused `internal/macports/binaryarchive` package is warranted. Start with a concrete signing-key store and an operation that prepares/verifies an archive plus its two signatures and public keys. Return the files needed for a site entry. Tart retains guest paths, upload, permissions, startup, and guest configuration; `channel` retains byte transport and SSH trust. Preserve the existing key directory through configuration when moving ownership, rather than silently rotating keys. The engine's persisted archive retention and reuse policy remain separate.

Use names that distinguish three existing concepts: upstream source archives (`archive` and `portedit/archives`), built MacPorts packages (`model.Archive`), and an installable signed archive-site entry. Another undifferentiated `archives` package would worsen the ambiguity.

### 7. [P3] Tree-layout facts still have several private owners

**Evidence:** `internal/engine/scope.go:13`, `tidy.go:332` (`groupOf`), `update.go:345` (`portDirectory`), `engine.go:259` (`treeHoldsPorts`); `internal/macports/workspace/workspace.go:22`, `:438`; `internal/macports/context.go:135`; `internal/macports/eval/resolve.go:47`; `internal/macports/portindex/selection.go:122`; `internal/reuse/inputs.go:14`.

These helpers independently know the category/port/Portfile layout and whether a leading dot or underscore excludes a category. Even the `_resources` directory has an exported constant in `reuse`, a private constant in `workspace`, and literals in the engine. Reuse is a consumer of that MacPorts fact, not its natural owner.

The helpers are not interchangeable as written. CI scope intentionally counts only Portfile and files/ changes, while tidy groups other files in a port directory too. `engine.portDirectory` has a fallback to `path.Dir`; the workspace helper returns no port for `_resources`. Consolidating all of them into the CI regex would erase those distinctions.

**Ownership:** Put `ResourcesDirectory`, category validation, Portfile-path validation, and a `PortDirectoryOf(path) (directory, ok)` operation in `macports`. Expose operations over the fact, not a mutable regex or table. Then layer explicit CI-scope and grouping policies on top. Keep Git tree walking and filesystem checks in their respective consumers. Start with strings and named results; a persisted directory type migration is unnecessary.

Also replace `buildenv/tart.validPortName` (`provider.go:725`) with `macports.ValidName`, adding any genuinely archive-site-specific restriction explicitly. Its current local test rejects a space but accepts tabs and other control characters that the domain validator rejects. No exploit or real malformed target is claimed.

This extends and revalidates finding 29 of the [previous code-organization review](2026-09-27-code-organization-review.md), already on the roadmap. The additional evidence here is the newer `reuse.Resources` ownership and archive installer validator.

### 8. [P3] SSH readiness is one operation implemented by three private helpers

**Evidence:** `internal/tart/provision/connect.go:60`, `internal/buildenv/tart/machine.go:141`, and `tools/facts/tart.go:164`; transport classification at `internal/tart/channel/channel.go:83`.

All three helpers repeatedly run `/usr/bin/true`, wait for SSH, observe VM termination, and bound the wait. Provisioning additionally recognizes the output for a refused login or host-key check and stops immediately. The provider and facts tool discard that output and wait on any `ErrTransport`; channel classifies SSH exit 255 as that broad error. They have different polling intervals but substantially the same operation.

**Ownership:** Give `channel` a readiness operation and a structured distinction between a transient connection failure and a terminal authentication/host-key refusal. Let callers provide their timeout and VM-done observation through a small interface or callback. Preserve the provider's responsibility to map a terminal refusal into its retry policy; moving the loop alone does not fix repeated attempts. This needs neither a new package nor a general VM lifecycle controller.

This is the earlier review's finding 7, revalidated, with the facts tool identified as a third consumer. The current roadmap already includes both the wait and runner-retry fixes. No guest was contacted in this review.

### 9. [P2] Prepared-edit mutation remains on the wrong side of the fidelity boundary

**Evidence:** `internal/engine/stealth.go:44`, `:106`, `:118`, `:130`; the editor's established re-evaluation pattern at `internal/macports/portedit/go_toolchain.go:42`–`:113`.

`stealth` and `dropStealthDistSubdir` mutate the prepared Portfile after preparation has supplied its evaluated result and fidelity reports. `rewritePrepared` updates the Git tree only. The helper then computes the new revision and dist_subdir itself from the earlier evaluation. Those facts describe the edits the helper intends, rather than a fresh evaluation of the final text.

The package boundary is the problem even though the low-level textual changes call `portfile`: the engine is completing a semantic Portfile edit after the editor has certified an earlier state. By comparison, `raiseGoToolchain` already lives inside the editor, re-evaluates the edit, records expected changes, and checks the result.

**Ownership:** Keep the engine's branch-base comparison, which determines whether a checksum change qualifies as a stealth update. Pass that decision as an explicit preparation request, and perform the complete revision/dist_subdir operation inside `portedit` before its final result is returned. Its result should carry the observed final metadata and any incomplete action. Avoid exposing a general mutate-after-prepare hook that recreates the same problem.

This is the still-open half of the earlier review's finding 19, already listed under the roadmap's latent items. The mirrored `preparation.Result` issue from that finding has been fixed; this review does not reopen it. This claim was verified by tracing the current code, without another runtime probe.

### 10. [P3] The newest body-merge helpers imply a section-level merge result

**Evidence at `f43228ac`:** `internal/engine/body.go:370` (`typesSpan`), `:389` (`mergeBody`), `:404` (`mergeTypes`), `:425` (`refreshedParts`); `internal/engine/submit.go:115` (`Answer`).

The newest commit correctly makes Type(s) independently owned and removes command's duplicate `sameText` helper. It also makes the missing concept clearer. `mergeBody` returns text and one boolean about the Tested-on tail; `mergeTypes` returns only text. `refreshedParts` then locates the sections again and compares before/after text to reconstruct which parts changed, returning English phrases for the preview. Ownership and change information existed while merging but was not returned.

**Ownership:** Keep the operation in engine for now, but introduce a local `BodyMerge` result with the merged text and typed section outcomes (changed, retained, absent). `SubmitPlan` can expose the relevant part identifiers and command can word them. This would remove the second interpretation of the merged body and make partial ownership explicit, rather than stretching a whole-body boolean as more sections become independently managed. It does not require a Markdown framework or a new publication package. No incorrect merge was reproduced; this is a small concept extraction suggested by the latest change.

## Smaller facts and concepts with existing homes

These are worthwhile when their area is next changed. They do not justify a generic facts, helpers, or utilities package.

| Current helper/fact | Appropriate owner and smallest useful change |
| --- | --- |
| `engine.namesRepository` (`engine.go:307`), `engine.githubName` (`create.go:311`), `theirRemote` (`submit.go:388`), and command's PR URL construction (`json.go:409`) | Put GitHub remote/web-address parsing and URL construction behind named pure operations in the existing GitHub layer. Reuse `github.ValidRepositoryName`, and let engine supply the PR URL for command to render. Preserve different input contracts: a project web URL may include a subpath; a Git remote should identify a repository exactly. `forge/github.NameFromRemote` already contains the stricter remote parser. Earlier finding 13, with additional consumers. |
| Provider IDs `"tart"`, `"github"`, `"command"` in `engine/provider.go`, `config.Capacity`, and command composition | Export provider-name constants from the existing `buildenv` contract so callers need not import implementations merely to name them. Keep configuration defaults separate from platform limits. This revalidates earlier finding 14. |
| `profilesForBoundaries` (`macports/portedit/observe/profiles.go:246`) choosing arm64 only for Darwin >=20 | The historical release/architecture relationship belongs in `macos`, beside `ProductForDarwin`. An `ArchitecturesForDarwin` or `SupportsArchitecture` query would let the observer keep responsibility for which boundary profiles it samples. This is an ownership suggestion, not a claim that the current threshold is wrong. |
| `portindex.maintainerIdentities` / `maintainerIdentity` (`selection.go:214`, `:228`) and `config.checkMaintainer` (`config.go:390`) | A MacPorts maintainer representation should own Tcl group parsing and identity normalization (`user`, `domain:user`, `@handle`, `handle@github`, and the special markers). Configuration can validate through it; selection can compare normalized identities. Preserve the grouping and original spelling for Portfile output. Start in `macports`; do not move selector matching into configuration. No runtime defect was established here. |
| `newport.licenses` (`newport.go:269`) | SPDX-to-MacPorts mapping is a MacPorts fact, exposed appropriately through a lookup function. Move the lookup to the MacPorts domain if another workflow needs it; do not export the map or introduce a license package for this single consumer. Likewise, keep build-system precedence in `newport.Detect`: comparison's build-file set has a different purpose. |

## What should stay private or separate

- The latest commit's `selection.unknownPort` is well placed. It adds the selection layer's wording while retaining both `macports.ErrTarget` and the underlying index error through `Unwrap`. Exporting the wrapper type or moving its message into the raw index would add coupling without a consumer.
- Helpers such as `macos.sourceRank`, `macos.xcodeUpperBound`, `archive.decompressor`, `patchcheck.checkFlag`, and sqlite's scan/encoding helpers already sit with the behavior they implement. Private facts need an exported query only when a real caller needs that question answered. The Xcode bound being a table is not itself an ownership problem.
- `patchcheck.extract`, `dependency.Manifest`, and `archive.Extract` have materially different contracts: selected patch targets under worksrcdir, an unambiguous named manifest, and whole-source extraction. They already share `archive.Walk` and `Member.Clean`. Their similar loops do not justify merging their selection rules.
- `portsource`'s interpretation and `upstream`'s network discovery remain separate. There is a local consolidation opportunity between `discoverListing` and `discoverOverridden`, but their result provenance and forge verification differ. Extract only the shared listing-to-candidate selection inside `upstream`; no new package is needed.
- Small formatting helpers (`plural`, `short`, first-nonempty selection) do not warrant a shared package. The model's partial branch validation and Git's full-ref validation also have different contracts; do not solve superficial duplication by pulling repository machinery into `model`.
- The shared launchd-plist proposal was explicitly declined in the [previous reconciliation](../activity/2026-09-27-code-organization-review-reconciled.md): command cannot import `macos`, and its service has a different lifecycle. This review does not propose reinstating that dependency or a configurable plist framework solely to remove three writers.

## Recommended sequence

1. Fix the reproduced semantic gaps at their owners: eligibility, conservative Portfile inspection, manifest completeness, Cargo lock source retention, and Tcl encoding. Keep regression tests around the behavior as code moves.
2. Finish the existing stealth fidelity and SSH-readiness roadmap items, including the newly identified facts-tool consumer.
3. Extract binary-archive signing while the new archive-install workflow is still contained. Keep SSH transport, package signing, and persisted retention distinct.
4. Consolidate MacPorts layout facts and the small identity/name operations as their callers are touched.

The two new packages justified now are **source comparison** and **MacPorts binary-archive preparation**. Most other recommendations are new operations or small value types in existing packages. Neither `engine` nor `command` needs a wholesale split to take these steps, and `model` should not absorb language parsers or operating-system mechanics.

## Validation and limits

The review used an isolated `git archive` export of the pinned commit. It inventoried private declarations across `internal/`, inspected cross-package fact/grammar duplication and relevant callers, and checked `tools/facts` and `tools/survey` for additional consumers. This was targeted source analysis, not a claim to have exhaustively verified every helper. The prior reviews, reconciliation, architecture document, and roadmap were read to avoid reporting completed changes as current defects.

The [companion regression-probe patch](2026-09-28-private-helper-probes.patch) contains seven test functions across three packages. All seven fail at the reviewed commit in the ways described above, including the option and Tcl-encoding subcases. It is an optional reproducer, not an applied production change. In a disposable checkout of the commit, apply it and run:

```sh
git apply docs/reviews/2026-09-28-private-helper-probes.patch
go test ./internal/engine ./internal/archive ./internal/macports/newport -run '^TestHelperReview' -count=1
```

The patch must be copied into that checkout first if the review documents are absent there. The description round-trip probe requires `tclsh`. The existing suites for `engine`, `archive`, `newport`, and `macports/selection` pass with the probes excluded at `7be0dc2d`. The engine suite required local loopback access for its `httptest` listeners and completed in 144 seconds. No real MacPorts build, VM, forge request, or submission was performed. This review changes documentation only.
