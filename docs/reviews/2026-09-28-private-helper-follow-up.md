# Private-helper ownership: follow-up

Reviewed 2026-09-28 through `259ee3bac714786589959d9af4fb445c30a40ad1` (`fix(portedit): a port on a mirror group is compared, and a refused ready says what to do`). Work began at `6b56c6e0`; the newer commit was included before validation. Source references below describe `259ee3ba`. Reading and tests used isolated commit exports, not the working checkout's in-progress edits.

This is a focused follow-up to the [private-helper review](2026-09-28-private-helper-ownership.md), checked against its [reconciliation](../activity/2026-09-28-private-helper-review-reconciled.md) and the current callers. It is not a repeat of the broader adversarial architecture review.

## Assessment

The changes have addressed the ownership problems well. Most operations moved to an existing domain package, with only source comparison gaining a new package. Callers generally keep their own workflow policies instead of acquiring a generic helper that erases their differences.

Of the ten findings, **five are addressed, one is substantially addressed with a remaining completeness gap, and four remain explicitly scheduled**. The four deferred items are still present in code; they have not been accidentally counted as completed by the roadmap. The remaining gap in source comparison is described below.

| Original finding | Current assessment | Evidence at the reviewed commit |
| --- | --- | --- |
| 1. Build eligibility | Open, scheduled with planning | `engine/plan.go:335` still decodes `known_fail` and splits `supported_archs` itself. |
| 2. Portfile source inspection | Open, scheduled with planning | `engine/plan.go:350` still classifies revision-only changes by stripping matching lines; `macports/commitrules/rules.go:196` still reads versions with regexes. |
| 3. Source comparison | Substantially addressed; one remaining gap | `sourcecompare` owns manifest interpretation and reports unread input, but `compare.go:182` forwards incomplete readings only from the new version. |
| 4. Shared Cargo.lock interpretation | Addressed | `macports/dependency/cargolock.go:46` serves both generation and creation, retaining source kind and origin. |
| 5. Tcl description quoting | Addressed | `tcl/syntax/quote.go:14` owns encoding; `newport.tclWord` delegates special-character encoding to it. |
| 6. Binary-archive signing | Open, scheduled with reuse/archives | `tart/channel/keys.go:88` still makes archive keys; `buildenv/tart/provider.go:644` still prepares and signs the archive site. |
| 7. Tree-layout facts | Addressed | `macports/layout.go` supplies shared facts and operations to engine, evaluation, workspace, indexing, and reuse. |
| 8. SSH readiness | Open, scheduled under Tart | Separate waits remain in provisioning, the build provider, and `tools/facts`. Refusal handling still differs. |
| 9. Stealth edits and fidelity | Addressed | `macports/portedit/stealth.go` makes and evaluates the edits; engine supplies branch context and consumes the result. |
| 10. Description merge outcomes | Addressed | `engine/body.go:405–439` returns typed section outcomes, which command uses for its preview. |

Paths in the table are beneath `internal/`, except `tools/facts`.

## What is better now

**The new boundaries carry the information their callers need.** The shared Cargo reader returns `CargoPackage` with source kind, original source, version, and checksum. Creation passes unsupported Git/registry crates through `Spec.Unfetched`, marks `cargo.crates` unconfirmed, and writes explanations into the Portfile. Update generation retains its stricter policy. These are appropriate differences between consumers of one interpretation, rather than divergent parsers.

The stealth move is similarly substantive. Engine supplies `StealthRequest` with the files changed since the branch base; the editor performs the revision and `dist_subdir` changes, evaluates the candidate, and checks selected-port and sibling fidelity. `Result.report` makes the evaluated snapshot the prepared result. An unsafe sibling change leaves the optional stealth edits unapplied and returns the problem. There is no longer an engine-side rewrite of an already-certified Portfile.

Description merging now retains what it knew while merging: each section is refreshed, current, kept, or absent. The preview consumes those outcomes rather than discovering changed sections from the finished prose. Keeping these small types in engine is sufficient; this does not yet justify a publication package.

**Shared facts have appropriate owners without absorbing caller policy.** `macports.PortDirectoryOf` supplies layout, while `engine.portChange` still applies CI's narrower Portfile/files rule. Tart uses `macports.ValidName` and explicitly adds archive-site restrictions. GitHub has separate strict remote and permissive page readers, preserving the distinction the earlier review warned about. Provider names live in `buildenv`; maintainer reading, identity, and validation live in `macports`; `macos.RunsOn` is used by observation-profile selection. The small-facts table from the earlier review is therefore largely completed too. The single-consumer license map can stay in newport.

**The latest commit continues that direction.** Previous-version archive fetching and `diff --archive` now consume the observed MacPorts fetch plan, including expanded mirror groups. `PortObservation.FetchPlan` owns the explanation for a missing plan; `archives.Store.Shipped` owns retrieval and checksum matching. This replaces a second interpretation of `master_sites` in those workflows. The direct-source adapter remains for the Go/Cargo dependency path, an explicitly documented limitation rather than a reason to merge every archive operation.

## Remaining gap in finding 3: the old manifest's incomplete reading is dropped

**[P2] Evidence:** `internal/sourcecompare/compare.go:161–185`, particularly the loop at `:182`; incomplete readings originate in `internal/sourcecompare/manifests.go:165–180` and `:186–224`.

The package split, real TOML/Go parsers, malformed-input errors, and oversized-member reporting all address the original problems. However, `manifestChanges` reads both versions into `readings`, then emits only `readings[1].unread`. Parse errors on either side are retained; a successfully parsed but incomplete *old* manifest loses its completeness information.

Two small cases reproduce an empty comparison:

| Manifest | Old version | New version | Actual result |
| --- | --- | --- | --- |
| `requirements.txt` | `-r base.txt` | A comment only | No findings |
| `pyproject.toml` | `[project]` with `name = 'pkg'` and `dynamic = ['dependencies']` | Same project with the `dynamic` declaration removed | No findings |

In both cases the reader explicitly knows it could not enumerate the old dependencies. Dropping that fact makes the result look like a complete comparison. Engine copies the returned changes, including their hold flags, into the upstream comparison (`internal/engine/update.go:528–553`); there is no later recovery of this missing fact. Consequently this uncertainty alone will not hold unattended submission. This does not establish that either example would actually break a build.

**Suggested completion:** preserve incomplete readings from both sides and identify which version they describe. Deduplicate equivalent explanations if useful, while retaining readable dependency differences. The existing private `reading` type already represents the required concept; no additional package or broad interface is necessary. Add regression cases for old-only gaps alongside the existing new-side tests. This assessment does not claim every manifest feature is modeled: active-manifest selection and Python version requirements remain separately recognized roadmap work.

## What should remain next

The planning work is the most consequential unfinished ownership change. `ineligible` still misses Tcl's `on` spelling, ignores option-read failures, and treats architecture lists as whitespace-separated text. Revision-only classification still mistakes matching text inside Tcl data for a command; the version reader still omits `go.setup` and does not establish that captured words are literal. The planned MacPorts eligibility result should distinguish eligible, excluded, and unknown. Source inspection belongs beside Portfile syntax/editing and should conservatively decline to prove a result it cannot establish. Moving the existing regexes unchanged would not complete these findings.

The other two deferred items remain bounded extractions. MacPorts archive signing and site preparation should leave SSH key management before the archive-install workflow expands; preserve existing key locations. SSH readiness needs one operation with a terminal refusal classification, with the build provider mapping that classification into retry policy. Merely sharing the polling loop would leave the behavioral discrepancy intact.

Engine remains about 10,900 production Go lines, command about 8,000, and portedit about 3,300, counting their package directories without subpackages or tests. Those sizes are still a maintenance signal, but the changes reviewed here moved responsibilities in sensible directions. Portedit growing as it takes responsibility for complete evaluated edits is appropriate. The next useful engine boundary is the already-planned planning extraction, not a size-driven split into miscellaneous helpers. There is no new package recommendation from this pass beyond the already-proposed `macports/binaryarchive`.

## Validation and limits

Existing suites passed for `sourcecompare`, `macports`, `macports/dependency`, `macports/newport`, `tcl/syntax`, `buildenv`, `macos`, and `config`. Focused GitHub address, engine description-merge, and command submission tests passed. Native MacPorts tests used `DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh`: stealth edits, sibling protection, removing the stealth directory, mirror-group comparison through preparation, and shipped/archive-diff cases passed.

Fixture HTTP servers initially hit the sandbox's loopback restriction; the affected tests passed when rerun with loopback access. Dependency-helper live tests requiring the explicit opt-in were not enabled. No VM or live GitHub operation was performed, and this was not a whole-repository test run.

One temporary probe in the isolated export exercised the two old-manifest cases above; both failed the expected-hold assertion and returned no findings. The probe was not added to the working checkout. Only this review and its activity note are delivered; no implementation or roadmap changes are made.
