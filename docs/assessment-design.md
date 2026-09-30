# Assessing an update: a design addition

Accepted 2026-09-30, with both behavior changes in §6 (the roadmap's D14). It adds the assessment to [Design v3](design-v3.md) §3 and §11, and to [architecture](architecture.md); the [roadmap](roadmap.md) takes it as item 9, in the order §7 below sets out.

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
- **It has no identity.** Nothing records which source it compared, so nothing can say whether it still applies.
- **It has no coverage.** A Git-fetched port returns `nil`, which holds nothing. An archive laid out other than the comparison assumes is read as no change.
- **Policy is set at the lowest level.** `sourcecompare` sets `Hold` itself (`proven`). The engine then overrides it for build systems, adds Python pins through a side channel (`[]pythonRequirement`), and adds the Go toolchain check. Holds reach the gate as English strings (`UpstreamComparison.Holds`, `SubmitPlan.held`), and some are de-duplicated by comparing their messages.
- **A project's manifests have three readers:**
  - `sourcecompare`, whose readers keep only names and specifiers;
  - `newport.Declare` and `newport.Binaries`, which decode Cargo.toml themselves;
  - `dependency.GoRequirement`, `GoBinary`, and `Manifest`, which parse go.mod and know about `worksrcdir`, which `sourcecompare` doesn't.

## 3. The additions

Five, each with one owner. None is a framework: each is a record and a function or two.

### A. A port's source identity (`model`, computed by `macports`)

This is what a port's version fetches:
- **archives:** each distfile by name and checksums, from MacPorts' fetch plan, which `portedit/archives` already computes;
- **a Git fetch:** the URL and the resolved commit.

The Portfile's checksums already bind an archive's bytes. A tag doesn't bind a Git commit, which is batch 20.

It's worth having because three consumers need the same fact:
- **batch 20:** a build's inputs record the commit it fetched, and reuse requires it;
- **the assessment's key** (D below);
- **the stealth-update classification** (batch 19): a change to the source identity with the version unchanged is a stealth update, whatever else the Portfile changed. The update-plan cache in Later would use it too.

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

**It reads through the tree interface, for:**
- an archive, through `archive`'s walk;
- the forge's archive of a commit, for a Git-fetched port;
- a registry's package, for `create` from `pypi:` and `crates:` later.

**Its coverage is part of what it returns.** It says:
- the root it read;
- whether the layout was one it understands: one enclosing directory, flat, or a root given;
- what it couldn't parse.

**The root comes from the caller**, from evaluated port facts: `worksrcdir`, and a PortGroup's own subdirectory setting where it has one. `dependency.Manifest` already does this for go.mod. `project` doesn't evaluate Portfiles.

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

This decides what an upstream change means for this port. It's pure, as `planning` is: it reads nothing itself.

**Input:**
- the two readings' differences, from `sourcecompare`, which becomes a diff of two project readings: typed changes and both sides' coverage, and no `Hold`;
- the candidate port's evaluated facts: build systems, the Python version, variants;
- the versions MacPorts has of the ports that provide a requirement: the index at the base, plus the branch's own updates;
- a policy version. Like Tart's `VerifierProtocol`, raising it makes earlier assessments no longer stand.

**Output:**
- **coverage:** what was inspected, what was set aside and why, and what's unknown;
- **concerns:** typed, each with its evidence, and whether it holds unattended submission (E below).

**It moves here:**
- build-system relevance, as used / demonstrated irrelevant / unknown, per archive context;
- D9's and D12's rules;
- the version-only rule, recognizing only version declarations;
- license years;
- Python pins, with provider unresolved and version unreadable kept apart;
- the Go toolchain check.

`compareUpstream`'s tail of about 180 lines in the engine goes here.

### D. An assessment belongs to a revision, not to an edit (`engine`)

For each port directory a revision changes, dockhand assesses that port at the revision against its base.

**Two tiers:**
- **The comparison is expensive:** it fetches both sides and reads and diffs them. It's cached by the pair of source identities and the reader's version, as archives are kept by digest. `update` fills the cache as it runs, since it already has both sides' archives.
- **The policy is cheap.** C is re-run wherever it's needed, on the revision's current facts.

**This settles, in one mechanism:**
- **finding 4:** the net change from the base is assessed, so MIT to GPL to MIT holds nothing, and 1 to 2 to 3 still sees every change between 1 and 3. A hand edit after a clean comparison gets a new assessment, since its source identity changed. An unrelated file or a commit message changes neither identity nor facts, so nothing is fetched again;
- **batch 13's hand-changed version**, and a port dockhand never edited;
- **batch 11:** `review` assesses the pull request's head against its base;
- **batch 19's Git ports:** each commit's forge archive feeds B. This is GitHub's and GitLab's documented archive endpoint, and a Git fetch without a forge is "not assessed", which holds. Submodules are said as a coverage gap.

`Edit.Upstream` stays as the record of what the update said when it ran. Stored records aren't migrated.

### E. Concerns, and one gate (`model`, `engine`)

**A concern** is typed:
- where it came from: the assessment, the release selection, the commit rules, the forge's search for other pull requests, or the check;
- its kind;
- its port and path;
- its detail;
- whether it holds unattended submission.

**Holds become one filter over concerns.** `SubmitPlan.held` stops gathering strings. Words are the command's, as elsewhere: the preview's `!`, batch 8's legend, JSON, and the attention list read the same records. De-duplication compares records, not messages.

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
- **A cache of whole update plans.** The review's "plan as reusable evidence" waits: D's comparison cache takes the most expensive part, the downloads and reading.
- **One table for build evidence and assessments.** They have different keys, lifetimes, and readers.
- **A policy language, a workflow engine, or the evaluation report,** which stays declined.

## 6. Costs and risks

- **Behavior changes, deliberately:**
  - unattended holds follow the net change from the base, not the edit history;
  - a hand edit that changes the source is assessed again;
  - a Git-fetched port on GitHub or GitLab is assessed rather than passed;
  - `check` and `submit` assess a version changed by hand.
- **One schema migration:** a table of comparisons keyed by source-identity pair. It's said and a copy kept, as decided.
- **GitHub's budget:** a Git port's two archives cost two API requests, one per commit. The download itself is a redirect off the API, but that should be confirmed against GitHub's documentation before relying on it.
- **Moves:** about 1,500 lines, from `sourcecompare`, `newport`, `dependency`, and the engine. Each step keeps behavior, and its tests move with it; behavior changes land afterwards, each with a probe.

## 7. Order

Each step lands in its own commits and is pushed once it passes.

1. **`project`:** the manifest readers, PEP 440 and 508, and the build-system file vocabulary move. Newport's and dependency's readers join them. Layout coverage is reported. No behavior changes but the one batch 19 asks for: a layout it can't read becomes a gap.
2. **`sourcecompare` as a diff of readings,** and **`assess`** with the typed concern. Hold policy moves out of `sourcecompare` and the engine. Batch 19's version-only and Python-unknown items land here. Build-system relevance becomes three-valued per context, which keeps batch 9's scoping and states its reason (the review's finding 2).
3. **Source identity, and a revision's assessment:**
   - the comparison cache;
   - the gate reading concerns;
   - the Git forge archive.

   This takes batch 19's Git item, finding 4, and batch 13's hand-changed version.
4. **Batch 20,** on the same source identity.
5. **Batch 19's remainder:**
   - release uncertainty, beside item 7's single livecheck pipeline;
   - the URL probe in parallel;
   - the early search for other pull requests;
   - the stealth classification from source identity.
6. **Batch 13's remainder:**
   - workspaces, as a reader in `project`;
   - `requires-python`, as a concern in `assess`;
   - the older-Python note.

   Then **batch 11:** `review` uses step 3.
7. **Batches 10, 12, 14, and 15, and item 7's coverage,** unaffected by the above, in the roadmap's order.

Steps 1 to 3 are the investment. After them, batches 13, 19, and 11 are mostly readers and rules added in the right places, not new paths.
