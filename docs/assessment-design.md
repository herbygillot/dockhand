# Assessing an update: a design addition

Accepted 2026-09-30, with both behavior changes in §6 (the roadmap's D14). Revised the same day with [Codex's critique](reviews/2026-09-30-assessment-design-critique.md), all of which it takes (§8). It adds the assessment to [Design v3](design-v3.md) §3 and §11, and to [architecture](architecture.md); the [roadmap](roadmap.md) takes it as item 9, in the order §7 below sets out.

## 1. Why now

The work left in the queue keeps landing on one flow: what upstream's change means for a port, and whether a submission nobody reviews may go ahead. These items all touch it:

| Item | What it adds to that flow |
| --- | --- |
| Batch 9's remainder, batch 13 | manifests below the top level, yarn workspaces, `requires-python`, a pinned Python older than the PortGroup's |
| Batch 13 | an assessment of a version changed by hand, from `check` or `submit` |
| Batch 11 | `review` giving what `update` finds, for someone else's pull request |
| Batch 19 | a Git-fetched port assessed; archive layouts it can't read said; version-only lines recognized; Python requirements it can't settle held; release selection's uncertainty kept |
| Batch 20 | a Git port's source bound to its build |
| Item 7 | `create` from `pypi:` and `crates:`, one livecheck pipeline |
| The helper-ownership review | a project reader, and the update assessment in `macports` |
| The update-workflow review, finding 4 | an assessment that applies to the candidate, not to the edit history |

Taken one at a time, each adds a branch to `compareUpstream`, another string to `held`, or another reader of a manifest. That is the disjointed result this note is meant to prevent.

## 2. What the architecture has, and what it lacks

**The build side is modelled.** A revision is captured and scoped. A plan decides from what evaluation observed (`planning`). A run's results are keyed by what each build read (`model.TargetInputs`). Evidence cells gather those results per target and environment, and a gate reads the evidence. Each step has an owner, a typed record, and an identity, and a result is reused only while its inputs still apply.

**The upstream side isn't modelled.** Design v3 has no noun for it. What exists is a side effect of the `update` command:

- **It belongs to an edit, not to a revision.** `compareUpstream` runs inside `Update` and is stored on the `Edit`. Serve's gate replays every edit's comparison (`upstreamComparisons`). So an update that went MIT to GPL and back to MIT holds twice, and a hand edit made after a clean comparison leaves that stale comparison standing. Neither a version changed by hand nor someone else's pull request gets a comparison at all: there's no edit to hang it on.
- **It has no identity.** Nothing records which source it compared, or which part of it, so nothing can say whether it still applies.
- **It has no coverage.** A Git-fetched port returns `nil`, which holds nothing. An archive laid out other than the comparison assumes is read as no change.
- **Policy is set at the lowest level.** `sourcecompare` sets `Hold` itself (`proven`). The engine then overrides it for build systems, adds Python pins through a side channel (`[]pythonRequirement`), and adds the Go toolchain check, whose judgment `portedit`'s `raiseGoToolchain` makes as it edits. Holds reach the gate as English strings (`UpstreamComparison.Holds`, `SubmitPlan.held`), and some are de-duplicated by comparing their messages.
- **A project's manifests have three readers:**
  - `sourcecompare`, whose Python and Cargo readers keep markers, repeated declarations, and Git sources since batch 17, but which imports `macports` and `dependency`, the direction a reader of upstream's files shouldn't have;
  - `newport.Declare` and `newport.Binaries`, which decode Cargo.toml themselves;
  - `dependency.GoRequirement`, `GoBinary`, and `Manifest`, which parse go.mod and know about `worksrcdir`, which `sourcecompare` doesn't.

## 3. The additions

Five, each with one owner. None is a framework: each is a record and a function or two.

### A. A port's source, as declared, observed, and built (`model`, computed by `macports`)

What a port's version fetches, at three strengths, kept apart in one small record:
- **declared:** what the Portfile asks for. For archives, each distfile by name and checksums, from MacPorts' fetch plan, which `portedit/archives` already computes. For a Git fetch, the URL and the tag, branch, or commit as written;
- **observed:** what dockhand saw when it fetched. An archive's bytes, by the digest of the file that passed the Portfile's checksums, and whether upstream or MacPorts' mirror served it; a Git ref's resolved commit, and when it was resolved;
- **built:** what a build fetched, which only the provider can say (batch 20).

The Portfile's checksums bind an archive's bytes, so for archives declared and observed agree by construction. A tag binds nothing, so for Git they can differ, and an observation made now never stands in for an earlier one:
- once a commit is recorded for a tag, resolving it again to another commit is a different source, not the same one again;
- the base of a hand-edited or someone else's branch may name a tag that has since moved. Where no recorded resolution of the base exists, the base is resolved now and said to be, as a limit of the assessment.

Three consumers need these facts:
- **batch 20:** a build's inputs record the commit it fetched, and reuse requires it;
- **the reading's key** (B below);
- **the stealth-update classification** (batch 19): a stealth update is a distfile whose name and the port's version are unchanged and whose content is not. Checksums modernized alone, sha256 added to the same bytes, is none; nor is a renamed distfile or a new mirror by itself. Content that couldn't be observed is said as unknown, not guessed. The update-plan cache in Later would read the same facts.

### B. Reading a project (`internal/project`, new)

This reads the typed facts of one source tree, at a root, through one small tree interface.

**What it reads:**
- license files;
- build files, by build system;
- each ecosystem's manifest, as its own record, not one universal schema:
  - Cargo, with its Git sources and optional dependencies;
  - go.mod: directives, direct and indirect requirements;
  - Python: `project.dependencies`, optional groups, `requires-python`, `build-system`, Poetry, requirements files, and PEP 508 markers;
  - Node: `package.json` and its workspaces.

What the readers keep today, markers, repeated declarations under different conditions, and Cargo's Git sources, moves with them intact.

**It reads through the tree interface, for:**
- an archive, through `archive`'s walk;
- the forge's archive of a commit, for a Git-fetched port;
- a registry's package, for `create` from `pypi:` and `crates:` later.

**Its coverage is part of what it returns.** It says:
- the root it read;
- whether the layout was one it understands: one enclosing directory, flat, or a root given;
- what it couldn't parse, and what isn't there.

**The root comes from the caller**, from evaluated port facts: `worksrcdir`, and a PortGroup's own subdirectory setting where it has one. `dependency.Manifest` already does this for go.mod. One archive can hold several projects, a program in `cli/` and its Python bindings in `bindings/python/`, each a subport's: those are two readings, and the root may differ between the base and the candidate.

**A reading is kept by what it read:** the observed source (A), the read specification (its root and options, never a temporary extraction directory), and the reader's version. Reading the same source the same way again reuses it; another root is another reading.

**It imports nothing of MacPorts.** It owns the ecosystem and build-system identities and what "manifest missing" means; `macports` maps its PortGroups and evaluated facts onto them. That reverses today's direction, where `sourcecompare` imports `macports` for its build-system names.

**It moves here:**
- `sourcecompare`'s manifest readers;
- PEP 440 and 508;
- `newport`'s manifest decoding;
- `dependency.GoRequirement` and `GoBinary`;
- the shared build-system file vocabulary.

**It stays where it is:**
- creation's detection order, category, and destroot guesses (`newport`);
- dependency-block policy (`macports/dependency`);
- comparison policy (C below).

### C. The assessment (`internal/macports/assess`, new; the noun *assessment* in Design v3 §3)

This decides what an upstream change means for one subject (D below). It's pure, as `planning` is: it reads and evaluates nothing itself.

**Input:**
- both readings in full, the base's and the candidate's, and their diff from `sourcecompare`, which becomes a diff of two readings: typed changes and both sides' coverage, and no `Hold`. The full candidate reading is needed because a rule can find a problem the diff doesn't show: upstream still requires `requests>=2`, and a hand edit removed the Portfile's dependency on its provider;
- the evaluated facts of the port at the base and at the candidate, in the subject's context: build systems, the Python version, variants, dependencies;
- what provides each requirement, and at what version, from the candidate revision's own port index, so a hand edit, a rename, or a deleted port counts as much as dockhand's own updates. What couldn't be read, or matched to a provider, stays so;
- a policy version. Like Tart's `VerifierProtocol`, raising it makes earlier assessments no longer stand, without fetching or reading anything again.

**Output:**
- **coverage,** with the evidence of relevance kept apart from its treatment. Evidence: used, observed to be irrelevant, or unknown. Treatment: inspected, or set aside under a named policy with its reason. Batch 9's scoping stays as a policy, not a proof: a manifest of a build system the port's PortGroups don't name is "relevance unknown, set aside under PortGroup scoping", and a later rule can revisit it;
- **concerns** (E below), each classed by comparing the base's assessment with the candidate's:
  - **introduced** by the candidate;
  - **resolved** by it;
  - **already present** at the base;
  - **unknown baseline,** where the base couldn't be assessed.

  Only introduced concerns, and those whose baseline is unknown, since what couldn't be checked holds (D4), count toward holding. One already present at the base is said, not held: an update doesn't become an audit of everything the port already was.

**It moves here:**
- build-system relevance, per archive context;
- D9's and D12's rules;
- the version-only rule, recognizing only version declarations;
- license years;
- Python pins, with provider unresolved and version unreadable kept apart;
- the Go toolchain judgment: whether the final `go.toolchain_min` meets what go.mod requires, whatever edit made it. `portedit` keeps raising the declaration with its fidelity checks, asking the same judgment which value to write, and the result is assessed again as any candidate is.

`compareUpstream`'s tail of about 180 lines in the engine goes here.

### D. An assessment belongs to a revision, not to an edit (`engine`)

**Its subject is a target in a context.** The revision's scope finds the changed directories and their targets, the subports, as it does for a check; each target is assessed in each archive context its fetch plan models, since one subport can fetch different sources by platform or variant. Sources are paired by their role and context, as `portedit`'s archive plan already pairs them, never by name or position, and what's added, removed, or unpaired is kept as such.

**Its base is the revision's captured base,** the one its scope was taken from. For someone else's pull request that's the base `review` captured, not master as it is when the assessment runs; a rebase moves it explicitly.

**Ports without two sides:**
- a new port has a candidate and no base. Its candidate is assessed, license and requirements included, with nothing reported as a missing old archive;
- a port with no upstream source, such as a metaport, has nothing to compare, and says so; that isn't a failure to fetch one;
- a removed or renamed port is assessed where it now is, if anywhere.

**Who collects, and who only reads.** Collecting means fetching, reading, and evaluating; only paths acting on a request collect, and `status` never does, as the principles keep it:
- `update` collects both sides as it runs, since it already has their archives;
- a check's driver collects what's missing beside the build. An assessment that can't finish, an old archive gone, doesn't fail the check: the check completes, its assessment is incomplete, and that holds unattended submission;
- `submit` and serve collect what's still missing before publishing, and check that the assessment applies to exactly the candidate the evidence does, since the files, base, or policy may have changed while work ran;
- `review` collects for the pull request's head.

`status` reports what's recorded: not requested, pending, available, incomplete, or stale.

**Two tiers:**
- **Readings are expensive** and kept (B). A network failure or a rate limit is never kept as a reading: it's said with when and why, and tried again. A parse the reader can't make is deterministic, and is kept with the reader's version.
- **The assessment is cheap** once its readings and facts exist, and is made again wherever it's read. An assessment records its fingerprint: its readings' keys, the evaluated facts' identity, and the policy version. Publication binds that fingerprint to the same candidate as its build evidence.

**This settles, in one mechanism:**
- **finding 4:** the net change from the base is assessed, so MIT to GPL to MIT holds nothing, and 1 to 2 to 3 still sees every change between 1 and 3. A hand edit after a clean comparison gets a new assessment, since its source changed. A hand edit to a provider gets one too, without fetching the application's source again. An unrelated file or a commit message changes nothing an assessment reads, so nothing is collected again;
- **batch 13's hand-changed version**, and a port dockhand never edited;
- **batch 11:** `review` assesses the pull request's head against its base;
- **batch 19's Git ports:** each commit's forge archive feeds B. This is GitHub's and GitLab's documented archive endpoint. It shows a commit's source, not what a build fetched, which is batch 20's to record, and submodules it leaves out are a coverage gap. A Git fetch without a forge is "not assessed", which holds.

`Edit.Upstream` stays as the record of what the update said when it ran. Stored records aren't migrated.

### E. Concerns, and what the gate does with them (`model`, `engine`)

**A concern** is typed:
- **its identity:** the rule that raised it, its subject and context, and references to its evidence. Two concerns are the same only when these are; the same concern in two contexts stays two;
- where it came from: the assessment, the release selection, the commit rules, or the forge's search for other pull requests;
- its class (C above);
- what it asks of a submission, below;
- its detail, for display only, never part of its identity.

Words are the command's, as elsewhere: the preview's `!`, batch 8's legend, JSON, and the attention list render the same records, and de-duplicate by identity.

**One gathering, not one verdict.** The gate gathers concerns in one place, and each submission mode keeps its own rules over them and over the evidence:
- a ready `submit` still needs its evidence under §3's publication rule; a failed required target blocks it, as now;
- a concern asks a person's look before a submission nobody reviews, serve's or `bump`'s. A person's own `submit` shows it and goes ahead, as D4 has it;
- `--draft`, `--no-check`, and `--accept` keep their rules.

So `SubmitPlan.held` becomes a filter over concerns for unattended submission, and nothing a person submits is stricter than today. Failed advisory tests are no concern that holds (D13).

**Batch 19 adds its holds as concerns:**
- release selection uncertain;
- a Python provider's version unreadable;
- not assessed.

### And one small change: release selection says when it's uncertain (`upstream`)

Discovery gains `Uncertain`, with its reason, beside `Current` and `UpdateAvailable`. Batch 19's finding 6 needs it. Item 7's single livecheck pipeline gives both discovery paths that one result. Uncertainty reaches the gate as a concern.

## 4. What needs no new architecture

These stay in their owners, as the roadmap has them:

- **Batch 10:** status, clean, adopt, and old branches. It needs one operation in `git`, patch-ids by `git cherry`, and one classification in the engine: landed, superseded, or unfinished, read by status, clean, and adopt;
- **Batch 11's dependents under variants:** `portindex`;
- **Batch 12:** GitHub's environment identity (`buildenv.IdentityProvider`);
- **Batch 14:** speed and logs;
- **Batch 15:** provenance;
- **Batch 19's small items:**
  - the URL probe in parallel;
  - bump's search for other pull requests before the edit, reusing the call `update --plan` already makes.

## 5. What not to add

- **Recommended checks,** such as "check `+doc` since its source changed" or "check the oldest release since the toolchain floor rose". A concern has room for what would settle it, but choosing checks from it is Later.
- **A cache of whole update plans.** The review's "plan as reusable evidence" waits: kept readings take the most expensive part, the downloads and reading.
- **One table for build evidence and assessments.** They have different keys, lifetimes, and readers.
- **A policy language, a workflow engine, or the evaluation report,** which stays declined. Collecting an assessment is the existing driver's work, not a second workflow.

## 6. Costs and risks

- **Behavior changes, deliberately** (D14):
  - unattended holds follow the net change from the base, not the edit history, and only what the candidate introduces, or what couldn't be checked, holds;
  - a hand edit that changes the source, or a provider, is assessed again;
  - a Git-fetched port on GitHub or GitLab is assessed rather than passed;
  - `check` and `submit` assess a version changed by hand.
- **Schema:** kept readings, by reading key, and each revision's assessments with their fingerprints, so status can say what's recorded. The migration is said and a copy kept, as decided.
- **GitHub's budget:** a Git port's two archives cost two API requests, one per commit. The download itself is a redirect off the API, but that should be confirmed against GitHub's documentation before relying on it.
- **Moves:** about 1,500 lines, from `sourcecompare`, `newport`, `dependency`, `portedit`, and the engine. Each step lands as a move that keeps behavior, its tests moving with it, and then the behavior changes that step takes, each in its own commit with its fixture.

## 7. Order

Each step lands in its own commits and is pushed once it passes. The contracts steps 1 to 3 rest on, the reading's key, the subject, the base, and what the gate does, are settled above, before any schema is written.

1. **`project`:** the manifest readers, PEP 440 and 508, and the build-system identities move, without their MacPorts imports; newport's and dependency's readers join them. Then the change: layout coverage is reported, and a layout it can't read becomes a gap (batch 19). Fixture: one archive, two subports rooted in `cli/` and `bindings/python/`, two readings. Done 2026-09-30 ([note](activity/2026-09-30-project-reader.md)). A subdirectory the archive lacks is read at its top and named in the reading, holding nothing yet, since a port's other distfiles lack it too; whether it's a gap is `assess`'s to say, in step 2. Keeping readings, and the fixture that the same read is reused, go with step 3, which keys them.
2. **`sourcecompare` as a diff of readings,** and **`assess`** with the typed concern. Hold policy moves out of `sourcecompare`, the engine, and `raiseGoToolchain`'s judgment. Then the changes: batch 19's version-only and Python-unknown items; relevance as evidence and treatment, keeping batch 9's scoping as a named policy; concerns classed against the base. Fixtures: a CMake file changing `project(… VERSION)` and `find_package(… 1.0)`; a provider removed by hand under an unchanged manifest. Done 2026-09-30 ([note](activity/2026-09-30-assessment.md)). Findings keep `model.UpstreamChange` as their record, gaining a rule, a subject, and a class, and a comparison gains its coverage, so a stored edit's findings read as before; the gate's own concern record, gathering the other origins, is step 3's. Resolved concerns are classed but not yet said. Relevance is per archive context, from the port as that context evaluates it; a finding carries no context yet, a change the contexts share being said once as before.
3. **Source facts, and a revision's assessment:**
   - declared and observed source (A), and kept readings;
   - subjects by target and context, and the base as captured;
   - collection by update, the check's driver, submit, serve, and review, and status reporting what's recorded;
   - the gate reading concerns;
   - the Git forge archive.

   This takes batch 19's Git item, finding 4, and batch 13's hand-changed version. Fixtures: MIT to GPL to MIT holds nothing; a new port with no fictitious missing archive; a directory with two Python subports of different applicability. Done 2026-09-30 ([note](activity/2026-09-30-revision-assessment.md)), with a Git-fetched port compared through its forge's archive of each commit and a tag moved since the check a concern ([note](activity/2026-09-30-git-ports-assessed.md)), in this Mac's context, where an update's own comparison, recorded when its branch was fresh, covers every context. `review` collects with batch 11. An assessment recorded incomplete stays for its files; trying again for the same files is a follow-up.
4. **Batch 20,** on the same source facts, what a build fetched. Fixture: a moved tag doesn't let earlier build evidence stand for the newly resolved commit.
5. **Batch 19's remainder:**
   - release uncertainty, beside item 7's single livecheck pipeline;
   - the URL probe in parallel;
   - the early search for other pull requests;
   - the stealth classification from observed content. Fixture: checksums modernized alone are no stealth update.
6. **Batch 13's remainder:**
   - workspaces, as a reader in `project`;
   - `requires-python`, as a concern in `assess`;
   - the older-Python note.

   Then **batch 11:** `review` uses step 3.
7. **Batches 10, 12, 14, and 15, and item 7's coverage,** unaffected by the above, in the roadmap's order.

Steps 1 to 3 are the investment. After them, batches 13, 19, and 11 are mostly readers and rules added in the right places, not new paths.

## 8. The critique, reconciled

[Codex's critique](reviews/2026-09-30-assessment-design-critique.md) read the design at `278547e9`. Its claims were checked against the code: `sourcecompare` imports `macports` and `dependency`; `raiseGoToolchain` judges and edits in one function; the principles keep `status` observational; and the readers have kept markers, repeated declarations, and Git sources since batch 17. Every point is taken:

| Its point | Where it went |
| --- | --- |
| 1. a reading's key includes what was read | B, "A reading is kept by what it read"; D's two tiers |
| 2. the candidate's full facts, and concerns classed against the base | C's input and output |
| 3. the subject as a target in a context; new, removed, and sourceless ports; the captured base | D's first three parts |
| 4. declared, observed, and built source; stealth from content | A |
| 5. who collects, status only reporting, transient failures not kept, publication bound to the fingerprint | D, "Who collects" and "Two tiers" |
| 6. one gathering, each mode's rules kept; a concern's identity | E |
| 7. relevance evidence apart from treatment | C's coverage |
| 8. the Go judgment apart from its edit; `project` free of MacPorts | C's moves; B |
| the two wording corrections | §2's readers; §6's moves and §7's steps |

None of it changes D14 or D13, or the build-system scoping kept at batch 9.
