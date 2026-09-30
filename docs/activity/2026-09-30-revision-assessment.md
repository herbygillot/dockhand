# 2026-09-30: a revision's assessment (item 9, step 3)

The [assessment design](../assessment-design.md)'s third step: what upstream's change means for a port is the revision's, not an edit's. Each port a revision's files change is assessed against the base the revision was captured on. It's recorded by the revision's tree, base, and port, and a submission's gate reads it through typed concerns.

## What changed

- **A revision's assessment** is `model.Assessment`, kept in the store (schema 25, `assessments`). One recorded again for the same tree, base, and port replaces it.
- **Collecting:** `engine.revisionAssessments` works through a revision's changed directories. It reads each directory's ports at the revision and assesses each port, a subport as itself, against the base, as this Mac's context fetches it:
  - the port is evaluated at both ends, and its fetch plan read, through `ArchivePlanner`, which the evaluator implements;
  - the archives are paired alike by name first, then in the order the plans name the rest; one only the revision fetches is read beside nothing, as new;
  - each is read through the reading cache (below) where a reading of its content is kept, by the sha256 its Portfile declares, and otherwise fetched as `diff --archive` fetches it, from upstream or MacPorts' mirror;
  - `assess` judges the pair with the ports' facts at both ends and the Python providers observed in the base's tree and the revision's.

  Some ports have no two sides:
  - a new port, or a subport new to its directory, has no base, and is assessed alone, with no missing old archive said;
  - a port that fetches no source has nothing to compare, which its coverage says;
  - a removed port is skipped;
  - a Git-fetched port's comparison says it isn't assessed yet, and holds (D4). The forge's archive of each commit comes next.
- **Who collects:** only commands acting on a request, as the principles have it.
  - **`update`:** where the branch was at its base before the edit, as a new branch is, and it compared archives or tried to, it records its own comparison as the assessment of the files it leaves. It compared every context that fetches them, so bump's and serve's branches arrive at their check with their assessment made.
  - **A check's driver:** once its builds finish, it collects what isn't recorded for the revision it checked. It never fails the check; what it couldn't do, the assessment says.
  - **`submit`'s plan, and so serve's candidates:** each collects what's missing for the commit it submits. A person's own submission shows what was found; only one nobody looks over is held.
  - **`status`:** reads what's recorded and collects nothing. It says, for a branch serve prepared, whether its files' assessment is available, incomplete, or pending, and shows "passed; what upstream's change means isn't assessed yet, which submitting does" where it's pending. `--json` carries `held` and `assessment`.
- **The gate:** `SubmitPlan.Concerns` gathers typed concerns (`model.Concern`), each with its identity, origin, port, rule, path, and subject, apart from its words:
  - what the assessments hold for, and what they couldn't compare;
  - commit-rule findings;
  - other open pull requests, or a failed search for them.

  `held` is `Blocking` and the concerns' words, so serve's and bump's holds read as before, and nothing a person submits is stricter. Edits' recorded comparisons are no longer replayed: an update that went MIT to GPL and back holds nothing.
- **Readings are kept** in `project.Cache`, a disposable directory: `$DOCKHAND_READING_CACHE`, else `dockhand/readings` in the user's cache. A reading is known by the archive's sha256, the subdirectory read, and `project.ReaderVersion`. `update` keeps what it reads, so an assessment made later downloads nothing it already has. A failure to read is never kept, and the zero cache, an engine given no place, keeps nothing.
- **Smaller changes:**
  - `assess.Policy` versions the rules;
  - `assess` reads the Go requirement from the new version's go.mod where no edit said it, and words a minimum below it that no edit looked at as "which doesn't gate on it";
  - `macports.PortInfo.GitFetched` replaces `portedit`'s private helper;
  - `engine.ArchivePlan` and `planOf` take `FetchArchives`' evaluation apart from its fetching.

## Decided along the way

- **An assessment recorded incomplete stays recorded for its files,** as an update's comparison always did. Their next version is assessed afresh. Trying again for the same files, where the failure was a network's, is a follow-up.
- **This Mac's context only:** a revision is assessed in this Mac's context, while an update's own comparison covers every context. Where the branch was fresh the update's stands, which is serve's and bump's case.
- **An evaluator that can't read a directory** leaves the assessment recorded for it standing. An engine given no evaluator, a test's, collects nothing.

## Proven

- **Engine:**
  - the net change: MIT to GPL and back holds nothing, whatever the edits recorded, and a later revision whose archives are the same fetches nothing again;
  - status collecting nothing, and reading "pending" then "available";
  - a new port assessed alone;
  - each subport assessed for itself, a Python-version marker asking of py312-demo's provider and nothing of py313-demo's;
  - a Git-fetched port held as not assessed, and a port that fetches nothing covered.
- **Elsewhere:**
  - the store: an assessment kept and replaced;
  - `project`: the cache, by content and root, never keeping a failure;
  - `assess`: the Go requirement read where no edit said.
- **Changed tests:** serve's and submit's existing tests pass with the gate reading assessments. The Go serve tests now fetch archives, as a Go port with archives does. A check that would have collected with a real evaluator reads the update's recorded assessment where the fixture's Portfile doesn't evaluate.
- **Mutation testing:** every mutant of the collection, the reuse of what's recorded, the pairing, the cache, the state `status` reads, the concerns, and the update's priming is killed.
- **Checks:** the full suite, `vet`, `fmt-check`, `vendor-check`, `deadcode`, and `lint` pass.
