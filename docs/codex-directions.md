# Dockhand: future directions

An exploration by Codex, October 1, 2026.

Dockhand's strongest future is to become **the place where a contributor turns a maintenance problem into a well-understood, reproducible, reviewable change**.

Its current foundations are unusually well suited to that: branches, immutable snapshots, identified build environments, reusable evidence, upstream assessments, and recoverable operations. Those could support much more than keeping versions current.

This exploration draws on the current v3 implementation, earlier discussions about its CLI, and three independent investigations of workflows, community needs, and broader directions. The existing roadmap was deliberately treated as out of scope. Community observations come from particular published discussions, not a representative survey. These are proposals, not commitments or documentation of implemented commands.

## 1. Demonstrate that the installed software works

This is the highest-priority addition.

Dockhand already answers whether a port builds and installs, and it runs declared upstream tests. The next question is whether the resulting package does what a user needs:

- Can the Python module be imported and perform a small operation?
- Can the CLI transform a fixture into the expected output?
- Can a tiny program compile, link, and run against the installed library?
- Are the required plugins, certificates, schemas, documentation, and data files present?
- Can a service start, answer a request, and stop inside the guest?

This would close a concrete gap: the current `submit` workflow still accepts a contributor's assertion that binary functionality was tested. MacPorts' own PR checklist explicitly asks for basic functionality and important-variant testing. [MacPorts PR template](https://github.com/macports/macports-ports/blob/master/.github/PULL_REQUEST_TEMPLATE.md)

Start with small, readable test recipes that maintainers can inspect and share. A successful `--version` invocation should receive less credit than a real operation with an expected result. Recipes should eventually be suitable for use outside Dockhand, too.

The particularly valuable extension is **upgrade testing**:

> Install the released port, establish a representative working setup, upgrade to the candidate, and repeat the checks.

That can reveal configuration loss, activation conflicts, missing files, broken plugins, and consumers that stop working. Record any automatic rebuilds so a successful repair does not conceal an incomplete revision-bump plan.

This gives users a stronger answer than a green build: the transition they will actually experience has been exercised.

## 2. Turn failures and bug reports into portable investigations

Dockhand already has baseline checks, useful log selection, and exact-snapshot retries. Build on those with a first-class investigation.

An investigation could begin with a failed check, a Trac ticket, a PR, or a user's build log. It would collect:

- The precise source and ports-tree state.
- macOS, architecture, Xcode/CLT, variants, and relevant dependencies.
- The failing phase and supporting files, including `config.log`, CMake diagnostics, test output, and patch rejects.
- What has already been tried and what each experiment established.

The immediate missing workflow is: **let me inspect the environment that failed**. Tart currently deletes the check's clone during cleanup. An opt-in debug session could retain it or reconstruct it and open a shell at the relevant point. Exploratory changes would remain distinct from a subsequent clean verification.

The next step is an exportable reproduction bundle. Another maintainer should be able to reproduce the case without reconstructing the author's terminal history. The export should identify its contents and avoid indiscriminately collecting credentials, unrelated files, or enormous source trees.

This has particular community value for old macOS and Intel testing. A July 2025 discussion describes compiler PRs exceeding CI time limits despite successful testing in older macOS VMs, alongside difficulty obtaining review. That suggests an opportunity to make such testing easier to request, perform, and assess. [PR review discussion](https://www.mail-archive.com/macports-dev%40lists.macports.org/msg11864.html)

Start with portable requests and attributed reports through the existing command provider. A contributor with unusual hardware could help by running one specific test. A central build farm would be a much larger undertaking. Imported results should retain their provenance and should not silently authorize unattended submission.

Eventually, investigations could support bounded experiments and bisection: find the first ports-tree change that breaks a recipe. Historical dependencies and unavailable sources would need to remain explicit; an old Portfile alone does not recreate an old system.

## 3. Preserve the reasoning behind a contribution

Dockhand already detects substantial upstream changes. What it could preserve better is the human response to those findings.

Suppose it reports a changed license file, a removed patch, and a new dependency. The contributor investigates and concludes:

> The license change only affects a bundled example we do not install. The patch is replaced by upstream commit X. The new dependency is optional, and our configuration disables that feature.

Those explanations are valuable maintenance knowledge. They should survive an interrupted session and be available when preparing the PR.

Add a way to review each finding, inspect its evidence, and record a decision:

- Resolved by a particular edit.
- Reviewed and acceptable, with a reason.
- Awaiting information.
- Requires additional testing.

Decisions should be tied to the relevant evidence and invalidated when that evidence changes. The existing assessment model provides a foundation. A human decision records judgment; it does not turn an uncertain observation into proof.

This could improve reviews in both directions. A reviewer should see **what changed since their previous review**, which concerns were addressed, and which tests still apply. A contributor should see unresolved review threads beside the relevant files and checks.

The important product outcome is less repeated investigation. Dockhand could carry the reasoning from author to reviewer to future maintainer.

## 4. Explain what a library update does to its consumers

`impact`, dependent revision bumps, and `check --also` are already useful. The next step is to connect their suggestions to observed consequences.

Compare the old and new installed artifacts:

- Library install names and linked libraries.
- Exported symbols and public headers.
- `pkg-config` and CMake package metadata.
- Installed commands, files, and resources.
- The behavior of representative consumers.

An especially useful report would say:

> This library install name disappeared. These existing binaries reference it. These consumers work after rebuilding; this one still fails.

MacPorts' guide specifically asks contributors to compare library install names before and after updates and rebuild linked ports when those names change. Dockhand could make that investigation substantially easier. [MacPorts version guidance](https://guide.macports.org/#reference.keywords.version)

Begin with installed-file and Mach-O comparisons, plus one small consumer test. Unchanged symbols would remain an observation, not a claim of complete ABI compatibility.

This also improves revision-bump decisions: contributors gain evidence about what needs rebuilding and what needs an actual source fix.

## 5. Help people choose useful work and finish work underway

The natural extension of `outdated --mine` is a maintenance inbox that considers more than releases.

Useful entries might include:

- A broken port whose failure someone has already reproduced.
- A PR waiting for a test on hardware the user owns.
- A patch now removable because its upstream fix shipped.
- A dependency transition blocking several other updates.
- A port with a broken livecheck.
- A nearly finished contribution waiting for one small correction.

For a newcomer, reproducing a failure and attaching the result may be a much better first contribution than becoming responsible for a package.

Make task suggestions concrete and bounded:

> This PR needs a Sequoia test. You have that environment. Expected work: one check and a report.

For maintainers, the same information could become a handoff dossier: recurring failures, important variants, patch rationales, testing recipes, unresolved tickets, and upstream contacts.

Maintainer status needs careful interpretation. MacPorts distinguishes timeout from abandonment; a tool should preserve those distinctions when suggesting work. [MacPorts update policies](https://guide.macports.org/#project.update-policies)

The product hypothesis is that **reviewer attention and contributor continuity are at least as important as update throughput**. Validate that by measuring how much investigation and review time Dockhand saves.

## 6. Support coordinated maintenance campaigns

A branch can already contain related changes across ports. A campaign would operate one level above that: a collection of related branches, experiments, and contributions.

Examples:

- Move an ecosystem to a newer Python release.
- Prepare ports for a new Xcode or macOS release.
- Update a shared library and repair affected consumers.
- Test a PortGroup change across representative users.
- Exercise a candidate MacPorts base release against a selected corpus.

A campaign would show prerequisites, shared failures, test coverage, owners, and which pieces can proceed independently. It could answer:

> Which five failures are consequences of the same dependency problem?

The first slice could be modest: named groups of existing branches, a dependency order, and one progress report. Larger campaigns could later support separate PRs and coordination between maintainers.

This is a promising way for Dockhand to benefit MacPorts infrastructure development as well as individual port maintenance.

## 7. Track patches through their whole lifetime

Dockhand already checks patch applicability and notices dropped patches. Extend that into patch stewardship.

For each patch, retain:

- Why it exists.
- Which platforms need it.
- Its upstream issue, PR, or commit.
- Whether it has been forwarded.
- What would justify removing it.

On an update, Dockhand could identify an upstream fix included in the release, propose removing the downstream patch, and rerun the scenario that originally required it.

The same information could help prepare a useful upstream report: minimal reproducer, environment, failure, proposed fix, and validation.

Debian's DEP-3 patch metadata offers useful precedent for recording origin and upstream status, without requiring MacPorts to adopt Debian's format. [DEP-3](https://dep-team.pages.debian.net/deps/dep3/)

The long-term benefit is fewer patches to carry and less knowledge lost when maintainers change. A patch that still applies can nevertheless be obsolete; a patch that stops applying can still represent an unresolved requirement. An upstream issue being closed does not establish that a released version contains its fix.

## Improving existing workflows

| Current workflow | Proposed improvement |
| --- | --- |
| `status` / `watch` | Preserve the concise next action, but expose every outstanding issue, grouped by common cause. Distinguish needing a decision, waiting on someone, and waiting on infrastructure. |
| Failure → logs → edit → check | Add the diagnostic workspace, supporting files, and a suggested next experiment. Make the distinction between retrying captured files and checking current edits unmistakable. |
| Upstream assessment → submit | Let people resolve and document findings before submission, then carry those decisions into the PR. |
| Changes requested → edit | Show the actual review threads and affected lines, with the new diff and relevant checks beside them. |
| Choosing checks | Allow a reusable statement of intended coverage: important variants, representative consumers, environments, and runtime recipes. Show missing coverage and estimated cost before running. |
| `init` → first contribution | Continue from capability discovery into the user's goal: update a port, fix a report, test a PR, or maintain a collection. Provide only the setup and guidance needed for that task. |

These build on substantial functionality already present in v3. Baseline comparison, individual-variant testing, resumable checks, intelligent log selection, duplicate-PR detection, upstream license/dependency analysis, and next-command status are already implemented; they should not be mistaken for new proposals.

Preserve progressive disclosure: a newcomer gets guidance, while an experienced maintainer can use the same operations directly.

## Further experiments

### Dependency hygiene and bundled-component security

Trace-mode checks and runtime-only environments could expose accidental dependencies. Existing manifest readers could support advisory matching for bundled components, with exact package identities and an explanation of uncertainty. OSV already provides relevant ecosystem and lockfile tooling. [OSV-Scanner supported artifacts](https://google.github.io/osv-scanner/supported-languages-and-lockfiles/)

A dependency unused in one configuration may be required in another. A lockfile entry does not establish that affected code ships or is exploitable. Start with evidence for maintainer investigation rather than automatic dependency removal or uncertain submission blockers.

### Reproducibility investigations

Build the same recorded inputs twice, compare installed artifacts, then vary time, path, or locale to identify causes of differences. Dockhand's environment and input records make this a plausible extension. [Reproducible Builds documentation](https://reproducible-builds.org/docs/)

Report differences in signing and packaging metadata separately rather than hiding substantive differences through normalization. A repeatable successful build and a byte-reproducible artifact are different claims.

## Recommended priorities and measures

The next three investments should be:

1. **Installed-product and upgrade checks.** Demonstrate behavior users depend on.
2. **Portable failure investigations.** Make failures easier to understand and reproduce across contributors and machines.
3. **Recorded review decisions.** Preserve the explanation another person needs to assess and maintain the change.

They reinforce each other: discover a real problem, make it reproducible, demonstrate the fix, and preserve the explanation another person needs to review it.

Judge their success by time to reproduce a failure, review rounds per contribution, regressions caught before merge, and whether occasional contributors return to finish another task. Those are outcomes Dockhand could materially improve for both its users and MacPorts.
