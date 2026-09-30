# Port update workflow and decision review

Reviewed 2026-09-30 at **073a880b449be4bfc2a9a8a507328453b5579700** (“fix(status): say what changed where a result no longer stands”).

This review follows a port through release selection, preparation, upstream assessment, working-file application, checking, and submission. It also considers checksum refreshes, dependent revision bumps, and batch/serve entry points. It examines the committed snapshot; concurrent uncommitted changes were excluded. Source locations below refer to that snapshot.

## Overall judgment

**I agree with most of the mechanical workflow. I am less comfortable with the strength of some of its packaging conclusions.** Dockhand does unusually careful work to establish that it edited the intended source and preserved other evaluated behavior. Its weaker question is: “Have we considered enough to recommend publishing this update?”

Keep the current preparation, fidelity, planning, and evidence boundaries. The next improvement should be a **current, explicitly scoped update assessment** that knows what was examined, what remains unknown, and which checks can resolve each concern. This would improve automation without another broad rewrite.

Several findings below concern deliberate policy choices, including D4, D9, and D12 in the roadmap. They are identified as such. A demonstrated behavior is not automatically a defect, and a successful synthetic fixture is not evidence that a real port has been published incorrectly.

## The decisions Dockhand makes today

| Stage | Current decision | My take |
| --- | --- | --- |
| Establish the source | Capture the tracked branch's working tree, or fetch master for a new branch/plan. A new update branch is deferred until there is an edit; unsupported automatic edits can leave a branch for manual work. | Good. Preparation has a stable input, current ports avoid empty branches, and unsupported work remains recoverable. |
| Choose a release | Honor the source convention and livecheck filter; ordinarily exclude prereleases unless the current version follows them; use MacPorts version ordering and evaluate version transformations. Explicit versions bypass automatic selection. | Good default policy. Two selection heuristics overstate what their evidence proves; see finding 6. |
| Prove an edit | Find the version-bearing literal, evaluate candidate edits, require a unique acceptable edit, reset revision, and reject unintended changes. Shared releases require authorization, with carefully limited obsolete followers. | Keep this. Syntax suggests an edit; actual MacPorts evaluation decides whether it is right. |
| Resolve artifacts | Model relevant platform/archive contexts, inspect variants declaring their own archives, protect unaffected checksum owners, fetch changed artifacts, modernize legacy checksums unless asked not to, and reevaluate. | Strong. Modeled metadata is correctly distinguished from build evidence. Coverage still needs to travel into later assessment. |
| Refresh generated dependencies | Regenerate old declarations first and refuse to overwrite maintained deviations; then generate the new blocks. Reject unsupported manifest patching. | Sensible conservatism. Preserving an intentional override matters more than forcing an automatic update. |
| Assess upstream | Compare replaced archives, inspect selected license/build/manifest files, check some Python constraints and Go toolchain requirements, and report patch applicability and HTTP URLs. | Useful, but the result needs coverage and uncertainty, not only findings and a hold flag. |
| Apply | Recheck the resolved upstream tag around preparation, verify the stored candidate evaluates equivalently, then apply file edits with preconditions and record the edit. Plain update commits nothing. | Good separation. Git-fetched source identity has a later gap, described below. |
| Check | Tidy when requested, capture the commit, plan changed ports/subports in configured environments, order prerequisites, build targets from source, and retain applicable evidence. | Keep this. The recent source-build/clean-work fix and verifier identity invalidation are appropriate. |
| Publish | A human submission sees warnings. Bump/serve additionally hold for upstream concerns, incomplete archive comparison, commit findings, and duplicate-PR uncertainty. The linked check binds the submitted commit. | Correct distinction between authoring and unattended publication. Some missing assessments currently evade that distinction. |

Main orchestration: [Engine.Update](/Users/herby/Source/dockhand2/internal/engine/update.go:151), [preparation](/Users/herby/Source/dockhand2/internal/preparation/preparation.go:154), [archive/context planning](/Users/herby/Source/dockhand2/internal/macports/portedit/artifact_plan.go:67), [linked submission](/Users/herby/Source/dockhand2/internal/command/submit.go:228), and [unattended holds](/Users/herby/Source/dockhand2/internal/engine/servesubmit.go:103).

## Findings

### 1. Missing assessment can look like a clean assessment

**Fix first: distinguish inspected, uninspected, and irrelevant.**

There are three separate holes in the comparison surface:

- **Git-fetched source:** [compareUpstream](/Users/herby/Source/dockhand2/internal/engine/update.go:637) returns nil when there are no downloads. This is the normal Git path. A nil comparison contributes no hold. Licensing and runtime dependency changes still exist in Git source; a successful build does not assess them. D4 already holds archive updates whose comparison could not be completed. I would apply the same principle here. Deferring Git patch applicability to the actual build is reasonable, but that does not justify deferring licensing to it.
- **Archive/project layout:** [interesting](/Users/herby/Source/dockhand2/internal/sourcecompare/compare.go:275) assumes one enclosing directory, skips archive-root files, and reads build/manifests only immediately below that enclosing directory. It neither validates that layout nor reports unsupported project roots. Fixtures containing a changed root-level LICENSE, and a newly added requirement in python/pyproject.toml, both return no changes and no error.
- **Recognized file, incomplete interpretation:** [pyprojectDependencies](/Users/herby/Source/dockhand2/internal/sourcecompare/manifests.go:234) projects dependencies but omits requires-python, build-system requirements/backend, and optional-dependency groups. A fixture changing the Python minimum from 3.9 to 3.13 and switching setuptools to hatchling returns no findings. Because pyproject.toml is a recognized manifest, the generic build-file warning does not catch these changes either.

The nested-project case is especially relevant to a port whose build directory is below the archive root. Preparation already carries worksrcdir/cargo.dir knowledge, while comparison receives only archive paths and old/new version strings.

**Recommendation:** return a comparison report with inspected roots/files, supported surfaces, and explicit gaps. Resolve the project root from evaluated port facts where possible. For Git sources, obtain comparable files at the resolved commits, or record “upstream packaging assessment not performed” and hold unattended submission. For flat or ambiguous archives, support the layout or report it as unassessed.

Do not recursively flag every manifest in examples, tests, and vendored projects. The improvement is to identify the project being packaged and report the limits of that identification.

### 2. Some noise-reduction rules use stronger conclusions than their evidence supports

**Fix the version-only rule; refine relevance before extending it.**

[versionOnly](/Users/herby/Source/dockhand2/internal/sourcecompare/compare.go:250) treats any changed line as harmless if replacing the old project-version string with the new one reproduces it. This is textual substitution, not recognition of a version declaration.

A fixture changes both:

```cmake
project(demo VERSION 1.0)
find_package(SomeLibrary 1.0 REQUIRED)
```

to 2.0. The entire file is classified as changing only the project's version, without a hold. A dependency minimum has changed too. Restrict the exemption to narrowly recognized version declarations; otherwise retain the generic build-file finding. This preserves D12's conservative policy without trying to interpret all of CMake.

There is a related problem in [BuildSystems](/Users/herby/Source/dockhand2/internal/macports/buildsystems.go:53) and [compareUpstream's suppression](/Users/herby/Source/dockhand2/internal/engine/update.go:658). Finding a known PortGroup establishes that a system is used. It does not establish that every other system is unused. CMake can invoke another build system or package runtime components without loading their PortGroups.

A fixture with a CMake PortGroup and a newly added package.json runtime dependency has its hold suppressed. That demonstrates the rule; it does not demonstrate an actual hybrid port failure. Also, the same native selected-port classification is applied to every archive pair, even though the archive planner may have found different platform/variant contexts.

I agree with removing flatbuffers' irrelevant manifest noise. I would represent relevance as **used / demonstrated irrelevant / unknown**, carry the archive's applicable context, and suppress only with an explicit reason. A known primary build system is useful positive evidence, not an exhaustive inventory.

### 3. Runtime compatibility is assessed selectively, and unknown satisfaction can be accepted

**High-value next capability: explicit runtime and toolchain requirements.**

The new Python marker handling is a real improvement. It retains repeated declarations, evaluates macOS applicability, and recognizes a previously foreign-only dependency becoming applicable. Those earlier review issues are addressed.

However, [pythonPins](/Users/herby/Source/dockhand2/internal/engine/update.go:741) only examines requirements that changed, only where a dependency's port name matches its package name, only for the selected prepared port, and stops after the first matching provider. A failure to read or interpret the supplying port's version produces an advisory finding with Hold false. An unmatched name produces nothing. Poetry constraints are read into summary strings without the typed requirements consumed by this checker.

A local fixture confirms that a raised requirement against an unreadable MacPorts version is advisory. The existing test suite intentionally expects that behavior too. **I disagree with it for unattended submission:** uncertainty about a changed runtime requirement is precisely something a passing build may not resolve. An unmatched name should be “provider unresolved,” not automatically “missing dependency” and not silent success.

A better operation would associate requirements with providing ports, applicable Python versions/subports, variants, and environments, then record satisfied / unsatisfied / unknown. At minimum, report coverage gaps for changed requirements. Longer term, reevaluate the complete applicable requirement set for the candidate, rather than assuming unchanged upstream constraints remain satisfied after other branch changes.

I agree with D9's decision to avoid holding every Go/Rust dependency update. Compilation often settles those concerns and generic holds create noise. Scope that conclusion to what was actually built. It does not establish optional-feature compatibility, runtime package satisfaction, older compiler support, or dependency licensing. The Go minimum check is a good example of an additional obligation that a modern successful build cannot settle. Python interpreter requirements and Rust/Node compatibility metadata are useful next surfaces, not a request to build a universal dependency solver.

### 4. Upstream assessments describe historical edits, not necessarily the candidate being submitted

**Add applicability and supersession to assessment records.**

[upstreamComparisons](/Users/herby/Source/dockhand2/internal/engine/servesubmit.go:136) returns every stored edit's comparison. It does not compare the current files with Edit.Files, establish which releases now apply, or reevaluate dependency observations. The edit record already has before/after file identities, but this consumer does not use them.

A fixture updates a port through MIT → GPL → MIT. Submission assessment still retrieves both intermediate license holds, although the final upstream license matches the original. Conservatively retaining a hold is understandable, but this is history being replayed, not an assessment of the net contribution.

The opposite direction matters more: by inspection, a later manual source change can leave a prior clean comparison attached to the branch. A new build correctly proves the new tree, but it does not refresh that old licensing/dependency assessment. No live unattended publication was attempted to demonstrate this scenario.

**Recommendation:** bind assessment facts to the selected release/artifact identities, relevant Portfile and shared-code inputs, dependency observations, and assessment policy version. At submission, reuse facts only when applicable. Otherwise reassess or say the assessment is stale. Preserve intermediate records for history; derive a current assessment against the contribution's base.

This should not invalidate everything after a commit-message edit or an unrelated file change. Use relevant inputs, as the build-evidence design already does. Nor should the fix merely keep the latest comparison: 1 → 2 → 3 must still account for relevant changes introduced between 1 and 2.

### 5. Git source is resolved during preparation but is not fully bound to later build evidence

**Correctness gap established by the data flow; no live tag mutation was attempted.**

The repeated tag checks in [preparation](/Users/herby/Source/dockhand2/internal/preparation/preparation.go:171) are good. They protect the preparation interval.

But [planGitVersion](/Users/herby/Source/dockhand2/internal/macports/portedit/git_source.go:57) leaves git.branch as a tag unless the original Portfile already pins a literal commit. The build happens later. [guestTarget](/Users/herby/Source/dockhand2/internal/buildenv/tart/provider.go:367) carries no expected upstream commit, and [TargetInputs](/Users/herby/Source/dockhand2/internal/model/inputs.go:32) identifies the ports-tree inputs and active packages, not the actual fetched Git revision. The guest delegates fetch to MacPorts.

If the tag moves after preparation, the recorded Release.Commit can name one source while the build fetches another. An unchanged Portfile and environment are insufficient to establish identical source inputs for such a port. The archive path has the written checksums to bind bytes; the tag-based Git path lacks the equivalent later proof.

**Recommendation:** carry an expected source identity into verification, verify the fetched checkout where the provider can, and record the actual identity with the result. Include it in reuse eligibility. Alternatively, pin a commit where that is appropriate to the Portfile's convention. Do not silently rewrite every tag-based port's style, and do not claim that a pre-build tag lookup alone closes the fetch race.

Providers that cannot attest this should expose that limitation in the evidence rather than claiming a fully identified source.

### 6. Release-selection shortcuts should retain uncertainty

**Refine these heuristics, without discarding the useful discovery optimizations.**

Two local fixtures exercise assumptions in [latest.go](/Users/herby/Source/dockhand2/internal/upstream/latest.go:233):

1. **Commit time is treated as release order.** predates discards a version that compares newer if its commit predates the current release's commit. A fixture with published versions 1.0 and 2.0, where 2.0 names an older commit, reports Current and sets aside 2.0. Release branches, delayed tags, and timestamp anomalies make that possible without a malformed version. The rule is useful for historical tag misspellings, but the comment that a newer release never predates the current one is too strong.
2. **Two points are treated as proof of ordering.** evaluateNewest evaluates the two greatest captured versions and falls back to all candidates only when those expose an ordering reversal/tie. A synthetic mapping 2.0 → 20.0, 3.0 → 3.0, 4.0 → 4.0 still chooses 4.0. This proves the optimization's limit, not the prevalence of such Portfiles.

Use chronological oddity as a reason to mark selection uncertain or require an explicit version, rather than confidently saying current. Keep the fast evaluation path when the transformation's ordering is established; use the existing batched evaluator for arbitrary transformations. Avoid introducing another independently maintained version comparator.

## Validation choices I would make more deliberate

The linked update/check path uses configured environments and default variants. Archive-context modeling does not automatically choose corresponding build coverage, and explicit variant checks remain a separate operation. That is a reasonable affordable default, but it should not become an implicit claim of full support.

An update assessment could recommend a small set of checks based on the actual change:

| Observation | Useful follow-up |
| --- | --- |
| A raised toolchain or OS requirement | Check the oldest relevant supported environment, or explicitly narrow/confirm support. |
| A changed variant's source or requirements | Check that variant; do not demand all variant combinations. |
| A shared library or public interface changes | Surface direct dependents and recommend representative consumer builds; a successful library build alone is insufficient. |
| A Python runtime requirement changes | Resolve provider/version/applicability and consider an import or package-specific smoke test. |
| Declared tests fail or time out | Present that as an unattended-publication concern, even when the check's recorded policy makes it advisory. |

[TestsDeclared](/Users/herby/Source/dockhand2/internal/model/plan.go:74) intentionally allows a built target with failed tests to pass, and the unattended hold rules add no separate test-failure hold. I understand matching MacPorts CI for the default check. **I would consider a stricter unattended publication policy for observed test failures**, preserving D1's rule that each historical result retains its original test policy. This is a policy recommendation, not an implementation bug. Absence of tests should remain different from tests that ran and failed.

Keep [--revbump-dependents](/Users/herby/Source/dockhand2/internal/engine/diff.go:240) explicit. The base index's direct library dependents are a useful actionable set, not proof that all need revision bumps or that no other consumers matter. Static/header-only consumers, plugins, variant-only relationships, and API changes can need different treatment. Recommend impact checks separately from writing revision bumps; do not automatically bump every reverse dependency.

I would also retain optional baselines and the distinction between infrastructure failure and port failure. Runtime smoke checks are already deferred in the roadmap; they are an acknowledged coverage limit, not new unfinished work invented by this review.

## Efficiency and smaller decision improvements

**Move cheap publication preflight earlier.** Bump checks for existing local work and valid environments before editing. It does expensive preparation before the other-PR search; [submitChecked](/Users/herby/Source/dockhand2/internal/command/submit.go:228) then checks/builds before applying the unattended holds, even if its initial submission plan already names another PR. Batch preparation does not perform that early remote search. An unattended publication mode could stop before downloads/builds on a known duplicate, while an explicitly requested preparation/check still proceeds. Repeat the search before publishing because the world may have changed.

**Treat plan as reusable evidence with preconditions.** A full update --plan already downloads, evaluates, and compares; executing the update repeats this. A bounded prepared-candidate artifact could reuse local observations and artifact bytes if the source and release identities still match. Cache immutable data by digest and preserve the intentional final source/tag checks. Profile interpreter starts before changing evaluator behavior; sessions and workspace reuse already exist.

**Keep URL advice off the critical path.** [plainHTTP](/Users/herby/Source/dockhand2/internal/engine/https.go:28) probes URLs serially, with up to ten seconds each, even before returning Current. Bounded parallelism, deduplication, or optional cached advice would improve responsiveness without changing the update decision.

**Classify checksum intent from source facts.** [stealthUpdate](/Users/herby/Source/dockhand2/internal/macports/portedit/stealth.go:59) skips stealth handling whenever the Portfile differs anywhere from the branch base. A comment/homepage edit therefore changes whether a same-name source replacement gets a revision bump and dist_subdir. Compare version/source/fetch/checksum facts, or retain an explicit authoring intent, rather than inferring a version edit from any changed Portfile. This observation is from the code path, not a separate reproduced update.

**Avoid turning patch prechecks into absolute authority.** Keeping the prepared edit when a patch fails the host-side check is useful: a person can fix it, and the actual build runs the real hooks. For unattended work, expose “likely to fail before build” and allow a cheaper triage stop, but do not equate that limited precheck with authoritative MacPorts execution.

## A focused design improvement

The shared project reader and MacPorts update-assessment boundary proposed in the [helper review](2026-09-30-helper-ownership.md) are still the right direction. This review gives them concrete responsibilities.

A project reader should produce structured source facts, identified project roots, requirements, compatibility declarations, and limitations. A small MacPorts assessment operation should combine those facts with evaluated port contexts and available check evidence. It should not own Git operations, network transfers, branch creation, or the queue.

Its result needs four things:

1. **Applicability:** source/artifact identity, relevant port/dependency inputs, and context.
2. **Coverage:** what was inspected, skipped as irrelevant, or left unknown.
3. **Concerns:** structured reason, evidence, and whether human judgment or a particular check can settle it.
4. **Current disposition:** edit can be prepared, checks are still needed, human review is needed, or unattended publication is supported.

Keep human wording as presentation, not identity or a deduplication key. Keep the assessment decision deterministic. A small set of explicit records and functions is enough; there is no demonstrated need for a generic policy language or a new workflow framework.

Implement in this order: expose absent/incomplete comparisons and fix the unsafe suppression; bind Git source and assessment applicability; improve runtime/context requirements; then use those facts to recommend validation and avoid unnecessary work. Release-selection heuristics can be tightened independently.

## Validation and limits

The existing suites passed for sourcecompare, upstream, preparation, macports/portedit, and planning. Focused existing engine/command tests covering update, Python pins, relevance, bump, serve holds/submission, and passing branches passed, as did the Tart guest build-order test. HTTP fixture tests were rerun with loopback permission after the sandbox initially blocked their listeners.

Ten small [characterization probes](2026-09-30-update-workflow-probes.patch) passed in an isolated export. They **assert the current behaviors described above**, including the limitations; passing does not mean those decisions are desirable. They cover Git comparison absence, flat/nested archive coverage, Python compatibility omission, unknown Python constraint handling, relevance/version-only suppression, historical assessment accumulation, and the two selection heuristics.

Apply the patch to an isolated checkout of the reviewed commit, then run:

```sh
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh \
  go test -mod=vendor ./internal/sourcecompare ./internal/engine ./internal/upstream \
  -run '^TestUpdateReview' -count=1 -v
```

This was not a full repository test run, a live port build, or a publication attempt. Only this review, its reproduction artifact, and an activity note were added to the working repository.

