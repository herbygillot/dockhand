# Assessment design critique

Written 2026-09-30 by Codex as section 8 of the [assessment design](../assessment-design.md), read at `278547e9`, and moved here when the design took it in, unchanged but for its two links to files, made relative.

Reviewed the addition at commit **278547e9**, alongside Design v3, the principles, the accepted roadmap decisions, and the current comparison/publication code. These are comments on the proposed contracts, not a request to reopen D13 or the decision to retain build-system scoping.

**I agree with the direction.** The project reader, pure MacPorts assessment, and separation of reusable source facts from current policy address the underlying problem. Assessing the net contribution also gives hand edits and reviewed PRs a coherent route through the same logic. Keeping edit history for explanation, and keeping assessment separate from build evidence, are both right.

Before implementing steps 1–3, I would settle the following contracts. Most need a few explicit sentences and focused fixtures, not additional packages.

### 1. The comparison cache key must include what was read, not just which source was fetched

**Sections B and D currently disagree at the cache boundary.** B reads a caller-selected root; D keys a comparison only by the pair of source identities and the reader version. The same archive can contain several projects. Changing cargo.dir, or selecting another subport rooted elsewhere in that archive, leaves the source identities unchanged but changes the reading. A cached comparison of project A must not answer for project B.

Include each side's normalized project selection and relevant reading options in the reading identity. Different source roots before and after an update are legitimate. If the cache stores a diff, it must also identify the diff implementation/schema separately, or explicitly version the reader and diff as one contract. Keep the assessment policy version out of this expensive cache key so a policy change does not require downloading again.

One practical arrangement is reusable readings by source content plus read specification, with a comparison referring to two readings. A pair cache can work too; the important requirement is that its key identifies all inputs that affect the result. Do not key a supposedly portable reading by a temporary extraction directory.

**Acceptance example:** two subports consume the same tarball, one from cli/ and one from bindings/python/. Their readings differ; rerunning the same read reuses its evidence.

### 2. Assessment needs the candidate's full relevant facts, not only the upstream diff

C names “the two readings' differences” as its input. That is insufficient for reapplying rules to current port facts.

For example, upstream still requires requests >= 2, but a Portfile edit removes its provider or selects an incompatible Python subport. The source identities and upstream dependency diff have not changed. A policy function given only that diff cannot find the new compatibility problem. Conversely, a branch update to the providing port can resolve an earlier concern without changing the application archive.

Pass the complete relevant candidate reading, the source delta, and the evaluated facts for both base and candidate contexts. This also permits distinguishing **introduced**, **resolved**, **already present**, and **unknown-baseline** concerns. The design should say how inherited problems affect the gate; a revision-only change should not accidentally become an audit whose unrelated pre-existing findings all acquire new holds.

“The index at the base, plus the branch's own updates” should be an implementation description of a candidate-tree lookup, not an overlay of Dockhand edit records. It must include manual edits, renames/deletions, and context-dependent dependency selection, with unreadable and unresolved observations retained.

**Acceptance example:** unchanged upstream manifests, but a provider removed, added, or updated by hand, produce a different assessment without refetching the application source.

### 3. Define the subject as a target/context, and specify asymmetric changes

D's “for each port directory … assesses that port” is underspecified. A directory can define multiple subports, use different sources on different platforms, and change dependencies under variants. C already wants per-archive-context relevance, but the input/output records and pairing rules need to preserve that context.

Use the revision's existing target/scope concepts. A directory finds candidate subjects; it is not itself the complete assessment identity. Pair sources by their role and context, retaining added, removed, and unpaired artifacts. Do not assume a basename or positional zip of two archive lists establishes correspondence. Share expensive readings across contexts when the inputs coincide.

Also specify the minimal treatment of new ports, removed/renamed ports, source-format changes, and metaports with no upstream source. A new port has a candidate to assess and no historical release to compare; a metaport may legitimately have no source comparison. Neither is the same as a failed attempt to fetch an expected baseline.

For PRs, “base” should mean the captured contribution base already used by the revision/review flow, not a fresh moving base-branch tip chosen during assessment. A rebase changes that baseline explicitly.

**Acceptance examples:** one changed directory contains two Python subports with different applicability; a new port receives candidate checks without a fictitious “old archive missing” failure.

### 4. Distinguish declared source, observed content, and the source actually built

A is a useful shared concept, but its consumers need related facts with different evidentiary strength.

A Portfile's checksums constrain an archive; a fetched, validated digest identifies observed bytes. A Git URL/tag expresses a fetch request; its recorded resolution identifies one observed commit. Batch 20 establishes which commit the build actually fetched. Keep these distinctions explicit, even if represented within one small record.

This matters when assessing a manually edited or third-party branch: an old Git tag may already have moved, and resolving it now does not reconstruct the source that the base historically meant. Record that the baseline resolution was observed now; where the historical identity cannot be established, retain the gap. Once an expected commit is recorded, resolving the tag again must not silently substitute a different source under the same assessment/build claim. Forge archives remain useful evidence of a commit's source, with their stated submodule/other materialization limitations; they do not themselves attest the build's fetched checkout.

The same distinction prevents false stealth classifications. “Source identity changed at the same version” is too broad if identity includes filenames and the set of declared checksum algorithms. Adding SHA-256 to existing checksums can change that representation without changing any bytes. A mirror or filename change is also not automatically a same-name content replacement. Define stealth from changed content under the relevant unchanged release/distfile identity, and report unknown content separately.

**Acceptance examples:** checksum modernization alone is not a stealth update; a moved Git tag does not make prior build evidence apply to the newly resolved commit.

### 5. Add an execution and freshness contract for a missing assessment

“The policy is cheap” is true only once its observations exist. The document should identify who evaluates the port/dependency facts and who fetches a missing comparison. “Re-run wherever it's needed” must not make status a new downloader or evaluator with hidden durable side effects. The [principles](../principles.md) explicitly keep status observational.

I would have action/driver paths ensure the required observations for an immutable revision, and let status report what is recorded: not requested, pending, available, incomplete, or stale. Those need not all become database enums, but the behaviors must be distinct. A pure reassessment on already captured facts is fine; an absent dependency observation remains absent until an authorized collection path obtains it.

State what check does when assessment cannot finish. An unavailable old archive should not necessarily prevent a useful requested build; the check can complete with assessment incomplete and unattended submission held. Publication must bind the assessment and its input fingerprints to the same candidate as its build evidence, rechecking applicability if files/base/policy changed while work ran.

Keep transient acquisition failures retryable rather than caching a rate-limit or network failure forever as a definitive source reading. Version deterministic parse limitations; retain timestamps and error provenance for failed collection. This extends the existing driver, not a second workflow engine.

### 6. A unified concern record must preserve the existing gate distinctions

E's hold boolean is enough for selecting unattended warnings, but not enough to replace all publication decisions. Today, ordinary ready submission can be blocked by missing/failed required evidence; an upstream license concern can require human review while allowing a person's submission. Drafts, no-check publication, and accepted eligible failures have their own explicit rules.

Specify whether “one gate” means one presentation/aggregation of those existing rules, or a single publication decision operation parameterized by the requested mode and recorded authorizations. Either can work. Do not reduce everything to “has a hold”: that would either weaken existing blockers or make human submission unexpectedly stricter. D13 must remain intact: failed advisory tests do not become a new unattended hold merely because checks now emit concerns.

Also give concerns a stable semantic identity: rule/code, subject/context, and relevant evidence references. “Compare records, not messages” is not sufficient if record equality still includes detail text, rendering order, or timestamps. Keep display detail out of the deduplication key, and do not merge away the fact that the same concern applies in two different contexts. Persist structured evidence so rendering is not dependent on reconstructing prose.

### 7. Preserve the accepted scoping policy without calling it demonstrated irrelevance

C and step 2 promise “used / demonstrated irrelevant / unknown,” while the roadmap explicitly retains the heuristic that a known primary build system can set aside other systems' files. The roadmap also acknowledges that this does not prove those systems unused.

Those can coexist, but only if the record separates **evidence of relevance** from **policy treatment**. For example: relevance unknown, set aside under the PortGroup-scoping policy, with its reason. “Demonstrated irrelevant” should be reserved for an actual observation establishing that fact.

This is not a request to restore the noisy holds that were declined. It is a request for truthful coverage wording while preserving the chosen behavior. It also allows a later rule to revisit a heuristic exclusion without treating it as a proven fact.

### 8. Keep assessment independent of an edit, including the Go minimum check

“The Go toolchain check moves here” needs one ownership clarification. The current [raiseGoToolchain](../../internal/macports/portedit/go_toolchain.go) both assesses a requirement and rewrites the Portfile with fidelity checks. Only the judgment belongs in pure assess. File editing and its evaluation remain with portedit.

The final assessment must judge the final declaration against the manifest, without depending on the edit's historical “raised” outcome. Otherwise hand edits and reviewed PRs still have second-class support. It is fine for authoring to reuse the same pure judgment to choose an allowed fix and then reassess its result; no general fix-planning framework is needed.

Likewise, keep project free of MacPorts dependencies when moving its readers. “Manifest missing” and ecosystem/build-system identities should belong to their lowest shared owner; MacPorts maps its evaluated facts onto them. This avoids recreating the current dependency direction inside the new package.

### Implementation order and small wording corrections

I would keep the proposed order, but define the reading key, target/context subject, baseline semantics, and gate disposition before the schema/cache commit. Move the existing tests with the extraction, then add the contract fixtures above as the behaviors land. An extraction that retains one native port, changed requirements only, or a missing comparison represented as nil would preserve precisely the information loss this addition aims to remove.

Two wording changes would make the scope clearer:

- §2's “readers keep only names and specifiers” understates the current implementation: Python requirements already retain markers and repeated declarations, and Cargo constraints retain Git identity. Preserve that work during the move.
- §6's “each step keeps behavior … behavior changes land afterwards” conflicts with steps 1–3 explicitly including fixes. Identify the behavior-preserving move and the accompanying behavior change within each step; there is no need to postpone the already accepted fixes.

The architecture is worth proceeding with. The main work before implementation is to make its claims precise: what a reading covers, which candidate an assessment applies to, and what its conclusions authorize.
