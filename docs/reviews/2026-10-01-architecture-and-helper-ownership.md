# Architecture and helper ownership after the assessment work

Reviewed **`7dc72df793860fbd9ab765fad170278f644b847c`**, `docs(roadmap): what batches 26 and 28 left, and tests under load`, on 2026-10-01. This review uses an isolated export of that commit. Development continued in the original checkout; its uncommitted changes, including the retention work, are outside this review. Source locations below refer to the reviewed commit.

The architecture has improved substantially since the September 30 reviews. `project`, `sourcecompare`, and `macports/assess` now separate reading, differences, and judgment. `planning`, `reuse`, and `history` give other complicated decisions identifiable owners. Preparation carries the editor's result instead of reconstructing a partial copy. The provider contract carries fetched source identity, and findings have rule/subject identities and baseline classes. These are useful working boundaries.

The remaining pressure is mostly **above those boundaries**: engine helpers assemble parallel versions of the same operation, then treat their results as interchangeable. Another pressure point is below them: CMake helpers have grown enough that two packages now interpret its structure. I would address those seams incrementally, without another broad rewrite.

## Package weight

Counts are physical lines in production `.go` files, including comments and blank lines, excluding tests. Each row counts only that directory, not its children.

| Package | Lines | Files | Assessment |
| --- | ---: | ---: | --- |
| `internal/engine` | 15,117 | 41 | The main concentration of responsibilities; collection, evidence interpretation, document editing, and orchestration share one package. |
| `internal/command` | 9,387 | 32 | Large, but much is legitimate CLI and JSON presentation. The linked authoring workflow deserves attention before more commands reuse it. |
| `internal/macports/portedit` | 3,731 | 21 | Substantial but organized around an editor operation, with observation, archives, fidelity, and dependency work already separated. |
| `internal/macports` | 2,658 | 29 | A reasonable home for evaluated facts and MacPorts vocabulary; a few more facts still live in engine. |
| `internal/project` | 2,247 | 11 | A useful shared reader. Its growing CMake analysis needs a richer internal representation. |
| `internal/buildenv/tart` | 1,507 | 5 | Mostly coherent provider work. Its large provider file alone does not justify a new abstraction. |
| `internal/macports/assess` | 1,297 | 5 | The judgment boundary is sound; input completeness and source identity need strengthening. |
| `internal/sourcecompare` | 1,154 | 2 | Most is in one 1,008-line file, increasingly occupied by CMake interpretation and explanation. |

Size is a navigation signal here, not a defect by itself. The following findings identify operations or information that justify a boundary change.

## 1. Give assessment collection one owner before caching its result

**Priority: P2 — reproduced difference between entry paths.**

`engine.assessUpstream` (`update.go:697`) and `engine.assessPort` (`assessment.go:205`) both assemble an `assess.Input`, obtain readings, observe providers, and call the pure assessor. They do not collect the same evidence. The revision path calls `revisionPatches`; the update path never supplies `Input.Patches`, even though preparation already returns patch results.

This matters because `Update` promotes its comparison into a current-policy `model.Assessment` when the update starts from its base (`update.go:332–368`). `makeAssessments` subsequently keeps that record when it is not transient (`assessment.go:184–193`). Matching tree/base/policy establishes applicability, but does not establish that this particular collector performed every assessment operation.

The probe performs an actual update on a temporary repository with a declared patch, then asks for the revision assessment. The primed record has no patch coverage and bypasses collection. Collecting the same revision afresh produces patch coverage. This is an information-loss issue: current patch findings intentionally do not hold submission, and the build still applies patches. I am not claiming this bypasses a patch-related publication hold.

**Recommended boundary:** one assessment collection service, with an explicit request containing the base/candidate sources, port facts, and optional already-collected artifacts and patch observations. Update should supply those observations to avoid refetching; it should not implement a second definition of a complete assessment. Engine should retain branch selection and recording. Keep `macports/assess` pure.

Initially this can be an engine-local `assessmentCollector` with narrow dependencies. Extract it into a focused package such as `internal/macports/assessmentinput` once its contract is clear. Do not pass it the whole `Engine` or `preparation.Result`. A partial preview should either be marked incomplete by construction or finish collection before becoming a reusable assessment. Existing affected records will also need recollection, for example through an assessment policy bump.

**Acceptance check:** collecting identical base/candidate inputs through update and through revision assessment produces equivalent findings and coverage, including patch results; already-fetched artifacts are reused.

## 2. Model source correspondence and provenance instead of pairing anonymous readings

**Priority: P2 — two reproduced losses of information.**

There are three different correspondence rules:

| Path | Current rule |
| --- | --- |
| Update, `portedit.pairArchives` (`version.go:167`) | Uses the editor's observed replacement pairs, carrying before/after port facts. |
| Revision assessment, `engine.readPlans` (`assessment.go:472`) | Matches equal names first, then pairs the remaining entries by order. |
| Archive diff, `engine.archiveDiff` (`archivediff.go:97`) | Pairs by position, retaining old-only and new-only entries. |

These consumers need different outputs, but should not independently guess which source replaces which. In particular, `readPlans` iterates the remaining *new* names after matching equal names (`assessment.go:547–558`). Any surplus old archives vanish from the returned pairs. The probe gives the base a main archive and a supplementary archive, then removes the latter: assessment receives only the surviving archive. The removed archive's license files and the removal itself receive neither comparison nor coverage, although its reading was obtained.

Identity is then reduced again. `assess.Pair` has an `Archive`, but normal findings and coverage do not retain it. `UpstreamChange.Key()` is rule/path/subject (`model/history.go:122`), and `assessment.add` deduplicates matching key and message (`assess.go:206`). The second probe changes a separate `LICENSE` in each of two archives. It gets one finding and one coverage entry. A hold still exists in this example, but the reviewer cannot identify both sources or account for each one separately.

**Recommended concept:** a source set with explicit correspondence entries: matched, added, removed, or uncertain; each side retains artifact identity, project root, and the evaluated facts used to read it. An observed editor pairing is stronger evidence than a positional fallback. Preserve that distinction instead of silently making both into an ordinary pair.

Put the shared source-set operation beside the collection boundary in finding 1; it does not need another independent service. The generic `project` reader should still read one project, without knowing a port's fetch-plan semantics. Carry an artifact/occurrence identity into findings, coverage, and concern keys. A removed source can be informational or set apart under an explicit policy; it should remain represented. This recommendation does not require implementing every modeled platform context at once.

**Acceptance checks:** removals remain visible; reorderings do not create arbitrary cross-archive comparisons; a renamed archive can use an observed replacement relation; two archives containing `LICENSE` retain two identifiable occurrences. Update, revision assessment, and archive diff consume the same correspondence result where their inputs permit it.

## 3. Give CMake structure one representation shared by judgment and explanation

**Priority: P2 — reproduced explanation error; no incorrect hold demonstrated.**

`project/cmake.go` now contains a scanner, command arguments, option defaults, branch discovery, and three-valued condition evaluation. Meanwhile `sourcecompare/compare.go` contains another condition-stack reader: `cmakeConditions` uses a regular expression over individual lines (`:702–729`), and `cmakeTests` tokenizes conditions again (`:888`). Helpers such as `cmakeWhere`, `cmakeElsewhere`, and `cmakeOptionsOnly` combine those results with the project's richer scanner.

The difference is observable. For a changed `message()` inside a multiline `if(DEMO)` with `DEMO` off by default, the project reader correctly recognizes the unreachable block. The comparison's explanation says it changes only what the default build cannot reach, **“outside any if()”**. The second parser missed the multiline condition. The existing guard policy worked; the location and reason lost their supporting structure.

This is the point at which an additional **object** is more useful than more private helpers. Introduce a `project.CMakeDocument` or equivalent parsed representation with command spans and conditional regions. Let option/default analysis, unreachable-region filtering, and changed-region descriptions use that representation. This also avoids rescanning the same file separately for options, off defaults, stripped content, and location descriptions.

Keep generic source differences in `sourcecompare` and the decision to hold in `macports/assess`. The accepted D12 policy, including its bounds on variable indirection and included files, need not change. Start within `project`; a separate `project/cmake` package becomes worthwhile only if it improves ownership of this representation. A complete CMake interpreter is unnecessary.

**Acceptance check:** a conditional region has the same identity and location whether used to decide that it is unreachable or to explain the changed source; exercise multiline commands and `elseif`/`else` through both consumers.

## 4. Evidence aggregation is ready to become a domain operation

**Priority: P2 architectural follow-up — no new behavioral failure claimed.**

The `Cell`/`TargetEvidence` work has improved evidence semantics, and consumers increasingly ask the same methods. But `engine/evidence.go` is now 1,004 lines containing several layers: store traversal, provider identity collection, result-origin loading, plan/environment union, source compatibility, blocked-result propagation, cell merging, publication eligibility, and display wording. `runEvidence`, the foundation for those operations, lives in `runner.go:902`.

The difficult core is already identifiable: `evidencePlan`, `Counts`, `dropRemade`, `standing`, `fill`, and `settle`. It answers a coherent question: **what do these recorded checks establish about this tree in the environments required now?** That is as substantial an operation as planning or reuse, and is currently accessible only through engine's much larger package.

**Recommended boundary:** `internal/evidence` with explicit recorded-run inputs, required environments, current identities, fetched-source facts, and a typed evidence result. Engine loads the records and asks providers outside transactions; the evidence package combines and classifies the supplied observations. Preserve origin and per-run test policy in the input instead of looking them up lazily during the merge. Move wording to the relevant presentation/document consumer as practical.

Keep `reuse` distinct. Reusing a build across trees requires complete recorded inputs; aggregating evidence from checks of one tree has different allowances. `reuse.Current` and `engine.Counts` are not interchangeable just because both ask whether a result still stands. Continue sharing the existing source-dependency compatibility operation where appropriate.

This extraction should move the current cell, source, blocked-dependency, and environment tests with their rules. It should not change D1 test-policy interpretation, D17 required environments, or accepted unknown-identity behavior. It would also make the document extraction below possible without introducing an import back into engine.

## 5. PR document ownership is a domain concept hidden in engine helpers

**Priority: P3 architectural follow-up — no new behavioral failure claimed.**

`engine/body.go` is a 701-line document subsystem. It owns MacPorts template headings and allowed types (`:18–28`), interpretation of evidence into template claims, Markdown escaping, provenance wording, and a three-way update of sections that preserves the person's edits (`mergeBody`, `:607`). Its `SectionOutcome` and `DescriptionSections` types already express a meaningful public concept.

This belongs together, but is a poor fit for the branch/workflow engine. It also makes a template fact such as `PullRequestTypes` an exported mutable slice in engine, which submission validation then consults (`submit.go:301`).

**Recommended boundary:** a small `internal/macports/prdescription` package owning template vocabulary, typed composition facts, and section merge results. It can compose and merge a document without a store, repository, provider, or engine. Engine supplies the facts, observes the remote description, and performs publication. Keep MacPorts' template out of the generic forge client. Prefer an accessor or validator for supported types over exporting mutable package state.

Extract this after evidence, or initially pass a small document-specific fact model. Do not move all engine display helpers into a generic formatting package: this recommendation is about the ownership and preservation rules of a particular document.

## Smaller ownership opportunities

| Current helper or flow | Better owner or next step |
| --- | --- |
| `engine.portChange` / `ScopeOf` (`scope.go:13`, `:29`) | The Portfile-or-`files/` CI selection rule is a MacPorts fact. Put its classifier, and potentially its scope value, beside `macports/layout.go`. Keep it distinct from “any file belonging to a port directory.” No new package is necessary. |
| `engine.observe` / `defaultName` (`create.go:92`, `:116`) | Default Python naming and manifest/forge metadata precedence are new-port policy. `macports/newport` already owns detection, category guessing, and declaration reading; a typed observation result there would keep creation's pure decisions together. Engine should retain the remote read and branch edits. |
| `command.tidyAndSubmit` / `submitChecked`, compared with `engine.prepareOne` (`author.go:251`, `submit.go:238`, `outdated.go:216`) | The update → tidy → capture → plan → queue progression exists in both CLI helpers and unattended preparation. Keep prompts and previews in command, but consider an engine-level staged preparation operation before adding another entry point. Interactive and unattended stop policies differ legitimately; represent them explicitly rather than adding a universal workflow framework. No current divergence is claimed here. |

The command package's line count alone is not a reason to move its many output helpers. Likewise, I would retain the editor's current decomposition, the typed provider capabilities, `history.Transitions`, and the separate source-archive/build-archive concepts. The roadmap's baseline-variant limitation, broader assessment contexts, included build files, and retention work remain known work rather than new findings from this scan.

## Suggested sequence and validation

1. Fix assessment completeness and preserve source correspondence/identity in the present code. Add the cross-entry-path and multi-archive regressions before moving functions.
2. Establish the shared collector and CMake document representation while those areas are changing.
3. Extract evidence with its existing semantics and tests, then the PR description subsystem.
4. Move the small facts into their existing domain packages when touching their callers. Consolidate linked command workflows only as a concrete shared contract emerges.

Validation used the isolated committed snapshot, Go 1.27.1, and vendored dependencies. The existing suites passed for `project`, `sourcecompare`, `macports/assess`, `planning`, and `reuse`. Six focused engine regressions passed for net-change assessment, fresh-update priming, revision and dropped patches, subports, and transient retries; the engine import-boundary test also passed. This was not a full repository test run or a live MacPorts build.

The [probe patch](2026-10-01-architecture-and-helper-ownership-probes.patch) contains four **passing characterization tests of the discrepancies**, not proposed fixes or desired-behavior assertions. They ran only in the export. The engine probes use temporary repositories and local HTTP fixtures; their first sandboxed run could not bind a loopback socket, and the permitted rerun passed. To reproduce on an isolated checkout of the reviewed commit, apply the patch and run:

```sh
go test -mod=vendor ./internal/engine ./internal/sourcecompare ./internal/macports/assess -run '^TestReview(Primed|Removed|CMake|Different)' -count=1 -v
```

Only this report, its probe patch, and an activity note were added to the active checkout. No application code, existing tests, or roadmap was changed by this review.
