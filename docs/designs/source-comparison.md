# One judgment of a check's resolved source

A core item for v0.9.0, [#192](https://github.com/herbygillot/dockhand/issues/192). The issue says where it stands; this file is the design. Architecture wrote it on 2026-11-11, Codex reviewed that draft against the code the same day (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread), and this is the draft revised for that review and for its short second pass, read again at main f45d5b7a3. "What the review changed" lists each correction. It is built once the person adopts it.

## In short

- A check's plan records, for each target in each environment, the commit a Git source or a GitHub tag named when the check was planned. The places that then compare one such reading with another each do it in their own words.
- They become one judgment in `internal/model` with five answers: same, moved, other, unresolved, not known.
- What is shared is the reading of the facts. One table says what each reader does with each answer, and one table test holds the readers themselves to it.
- Most readers keep the outcome they have today. Four don't, because today they disagree with the reader beside them. Each of the four makes dockhand build where it now reuses a result or reports a pass. None makes it build less.
- No stored record changes, and no flag.

## What the code does today

| Where | What it compares | How |
| --- | --- | --- |
| `checks.SourcesCover` (admission.go:54), read by joining (`PlanCovers`, queue.go:168, :170) and by `coversPlan` (engine/evidence.go:424) | two plans, per environment and target, Git and tag | present in both or in neither, and `GitSource.Same`: one repository, one ref, and the same `Expected()` string. That string is empty on both sides where neither plan could resolve the ref, so two unresolved readings are "the same" |
| `engine.Covered` (evidence.go:405), which `QueueCheck` asks first and submit reaches too (`coversAsked`, engine/submit.go:1771) | a branch's finished checks, and a new request's plan | `coversPlan`. The newest check's own results are never asked what they fetched: `evidence.Judge` (evidence/evidence.go:395) runs only earlier checks' cells through `Counts` |
| `evidence.Counts` (evidence.go:982), from `fill` (:1131) | an earlier check's result, against the newest plan's Git source and against its own plan's | `BuiltBy`, then `Moved`. Where the newest plan has no Git source for the target, the first is skipped. Tags are not compared: `readSources` (:1081) builds the reuse rule's targets with no tag |
| `evidence.Plan` (evidence.go:548) | nothing | an environment only an earlier check planned comes with that check's own resolutions, whatever the newest check resolved |
| `reuse.Carry` (current.go:77) | the plan's Git source, and the commit an earlier build fetched | `BuiltBy`; a build that recorded a fetched commit stands for no plan that doesn't fetch with Git (:84) |
| `reuse.Target.TagMoved` (choose.go:42), read at choose.go:90 and :245 and at checks/reuse.go:102 | the plan's tag, and the tag an earlier build's check planned | one repository and ref, both whole commits known, and they differ |
| `reuse.builtAgainst` (choose.go:210) | a dependency's Git source, and the commit that built the archive a dependent had active | `BuiltBy` |
| `checks.judgeSource` (runner.go:1366), `FetchedWords` (words.go:83), and the guest's own test (guest/guest.tcl:1187) | the run's plan, and what its own build fetched | `Moved`, `BuiltBy`; the guest by prefix |
| The plan's note that a target builds again (engine/policy.go:105-124) | the plan's tag, and the tag of the last finished check that built it | in place: one repository and ref, both commits known |
| `engine.movedSources` (servesubmit.go:272) | the plan a submission rests on, and the source read again now, once for Git (:278) and once for tags (:301) | commits compared in place |
| `engine.preparedSources` (servesubmit.go:341) | an update's release, and that plan's Git sources only | the newest edit whose release names the ref by its tag or by its commit (`Release.Names`, model/release.go:146, which refuses an archive release), then commits in place |
| `engine.assessedSources` (servesubmit.go:369) | the commit an assessment read, and the plan's | whole commits, exactly; a plan that holds only an abbreviation is passed over |

Three methods on `model.GitSource` (`Same`, `BuiltBy`, `Moved`, source.go:53-78), one on `reuse.Target`, and the comparisons written in place hold one idea between them. #71 and #114 were each one reader missing what another already knew, and the review found three more of the same kind (the changes below).

## The judgment

In `internal/model/source.go`, beside the facts it reads:

```go
// SourceStanding is what one reading of a source says of another.
type SourceStanding int

const (
	SourceUnknown    SourceStanding = iota // one repository and ref; one side names a commit and the other names none
	SourceUnresolved                       // one repository and ref; neither side names a commit
	SourceSame                             // one repository and ref, one commit; or neither is fetched this way
	SourceMoved                            // one repository and ref, two commits
	SourceOther                            // another repository or ref, or only one of the two is fetched this way
)

// SourceAgainst judges two plans' resolutions of a target's source. nil is
// a target the plan doesn't fetch this way.
func SourceAgainst(have, want *GitSource) SourceStanding

// AgainstFetched judges a resolution and the whole commit a build says it
// fetched. It never answers other.
func (s GitSource) AgainstFetched(fetched ObjectID) SourceStanding
```

`SourceAgainst`, two plans:

| The two readings | Answer |
| --- | --- |
| Neither plan fetches the target this way | same: there is nothing to differ |
| Only one does | other |
| Another repository or another ref | other, whatever the commits. An older release's `v1.7.1` and a newer one's `v1.9.2` are other, never moved. That is `TagMoved`'s rule, and it becomes everyone's |
| Two whole commits | same where they are equal, moved where they aren't |
| Two abbreviations | same. Under one ref an abbreviation is that ref (git/remote.go:476), so the two can't differ |
| A whole commit and an abbreviation, in either order | not known, even where the commit begins with the abbreviation. `Same` compares the strings today, and a prefix proves nothing of two plans' readings |
| One side names a commit or an abbreviation and the other names nothing | not known |
| Neither names anything | unresolved |

`AgainstFetched`, a plan and a build:

| The plan | The build fetched | Answer |
| --- | --- | --- |
| A whole commit | a whole commit | same where equal, moved where not |
| An abbreviation | a whole commit | same where the commit begins with it, moved where not, as `BuiltBy` has it |
| A commit or an abbreviation | nothing it reported | not known |
| Nothing | a whole commit | not known |
| Nothing | nothing it reported | unresolved |

- Unresolved means the same in both tables: neither side names a commit. A plan that names nothing against a build that names its commit is not known, as a commit against nothing is in the first table. `BuiltBy` and `Moved` are both false there today, and stay so.
- "Not fetched this way" is what a plan recorded, not a fact about its build. A plan recorded before plans kept tags has no tag. So a tag reader's "other" column is written knowing the record may only be old.
- It reads nothing but its arguments. Reading a repository's refs stays in the engine (`resolveGit`, `resolveTag`), and following a moved source through the builds that had its archive active stays in `reuse.AgainstOtherSources`.
- Every reader compares per environment, as `SourcesCover` does. One freshly made plan reads each repository and ref once (engine/plan.go:436-449, :461-479), so its environments agree; the merged plan evidence is judged against need not (change 4).

`Same`, `BuiltBy`, `Moved` and `TagMoved` go once their callers ask the judgment.

## What each reader does with each answer

The readers differ for a reason the code holds and nowhere states:

> For a source fetched with Git, the Portfile's files don't say what was built, so what isn't known stands for nothing. For an archive fetched by a tag, the Portfile's checksums say what was built, so only a move that is shown unseats a result.

| Reader | same | moved | other | unresolved | not known |
| --- | --- | --- | --- | --- | --- |
| Join a queued or running check, Git and tag | joins | doesn't | doesn't | joins | doesn't |
| Count a finished check for another request, Git | counts, where each Git result's own fetch also reads same | doesn't | doesn't | **doesn't (change 1)** | doesn't |
| Count a finished check for another request, tag | counts | doesn't | doesn't | counts | doesn't |
| Take an earlier check's result into evidence, Git (`Counts`) | counts | doesn't | **doesn't, where the newest plan builds the target another way (change 3)** | doesn't | doesn't |
| Take an earlier check's result into evidence, tag | counts | **doesn't (change 2)** | counts | counts | counts, unless another build of the target shows the tag moved |
| Take a result on a release that only an earlier check planned, the newest plan's reading against the earlier one's | counts | **doesn't (change 4)** | counts | counts | counts |
| Reuse a build, Git (`Carry`) | reuses | builds | builds | builds | builds |
| Reuse a build, tag (`Choose`) | reuses | builds | reuses | reuses | reuses, unless another build of the target shows the tag moved |
| A build that had the source's archive active, in reuse and in evidence | follows it | builds, or doesn't count | | Git: builds. Tag: follows it | Git: builds. Tag: follows it |
| The run's own verdict (`judgeSource`) | stands | fails at fetch | | stands for its own check alone, and says so | stands for its own check alone, and says so |
| Submit, Git | says nothing | concern `source-moved` | | says nothing | read now and unreadable: concern `source-unknown`, which holds serve and a person sees. Not known in the plan: says nothing |
| Submit, tag | says nothing | concern `tag-moved` | | says nothing | the same, except a ref that is an abbreviated commit |

Two rows need a sentence each. An earlier result is also judged against its own check's plan: one that fetched another commit than its own plan named counts for nothing (`Counts`' second test, kept). And a result with no execution behind it, an unmet need, is the plan's fact and counts whatever the source (evidence.go:961, kept).

## The four changes

Each is a place where two readers disagree today.

1. **A finished check whose Git source couldn't be resolved no longer stands for another request.** Two requests whose plans both failed to read the repository join one running check, and that stays: joining shares a request and claims nothing of the source. But `Covered` then counts the finished check for a second request the same way, because the two empty readings compare as equal and the check's own results are never asked what they fetched. The comment on `resolveGitSources` already says such a result "stands for no later check's" (engine/plan.go:430-431). Finished coverage asks for same, and asks each Git result what it fetched, as `Counts` asks an earlier check's. The check's own report is untouched: it stands for its own check and says what wasn't known. Codex's probe showed the pass through `QueueCheck`.
2. **An earlier result built when a tag named another commit no longer fills in evidence.** Reuse builds again for a moved tag (`TagMoved`), and the plan says so. Evidence assembled from several checks compares no tag, so the old pass fills a cell the newest check left empty. Evidence takes the tag row above, and `readSources` gives the reuse rule its tags, so a dependent built against the old archive is found there as it is in reuse. Codex's probe showed the fill.
3. **A result built with Git no longer counts where the newest plan builds that target another way.** `Carry` refuses it; `Counts` skips the question. It is reachable through `EvidenceFor`, which groups checks by tree and takes older cells with no `SourcesCover` between their plans. A target the newest plan doesn't build at all (an earlier check's variant build, which evidence shows beside it) is not this case: nothing newer asks anything of it, and it is judged against its own plan as today.
4. **An environment taken from an earlier check doesn't count where that check resolved the ref to another commit than the newest one did.** A check narrowed to one release leaves the others' results to an earlier check, with that check's resolutions. If the ref moved between the two, the evidence holds two commits for one repository and ref, and submit compares what it reads now with the newest alone (`plannedBy`, servesubmit.go:415, keeps one reading per target, repository and ref). Only a shown move unseats anything: where the newest check couldn't resolve the ref at all, the earlier environment stands as it does today.
   - **What stops counting is a result, not the environment.** On the earlier release, the result of the target whose ref moved, and the results of targets built against it there (the dependents' row above). Every other result on that release stands.
   - **Nothing leaves what the evidence requires.** The release stays in the plan the evidence is judged against. A result that no longer counts reads as not checked, as it does where no check built it: `status` shows it, and submit judges it as it judges any missing result. A new check of that release resolves the ref afresh.
   - Architecture found this while checking the review. Codex's second pass confirmed it by running it: a pass on macOS 15 at one commit, the tag moved, a pass on macOS 26 at the other. `EvidenceFor` kept both releases with their two commits, and `movedSources` raised no concern. It held for a Git source and for an archive fetched by a tag.

## Submit's adapters

- `movedSources`' two loops share one traversal over Git and tags. They keep their order: a source read now and unreadable raises `source-unknown` only where the plan knew its commit, and a plan that never knew it gains no concern.
- `preparedSources` keeps `Release.Names`: a release names the ref by its tag or by its commit, and an archive release names none. A second `GitSource` built from the tag alone would lose the match by commit.
- `assessedSources` asks the judgment with the assessment's whole commit. Its pass over an abbreviated plan is the "not known" column.
- The concerns keep their names, words and subjects: `source-moved`, `tag-moved`, `release-moved`, `assessment-moved`, `source-unknown`.
- The guest's own test (guest.tcl:1187) stays where it is, in Tcl. The verdict recorded is the driver's, and one test holds the two to the same prefix rule.

## What the review changed

| Codex's point | What was wrong in the first draft | Taken |
| --- | --- | --- |
| S1 | It said a finished check's Git results pass `Counts`. They don't | Change 1, stated as a change, with a test through `QueueCheck` in both modes |
| S2 | No row for an earlier archive-tag result filling evidence | Change 2, and the dependents' row reads "in reuse and in evidence" |
| S3 | "`Same`, `BuiltBy` and `Moved` are written on the judgment with nothing changing". `Same` is exact and `BuiltBy` is by prefix, so a single prefix rule would change `Same` | The two tables above, which keep each predicate as it is. The first commit proves the old methods equal the judgment before any caller moves |
| S4 | `preparedSources` described as a third loop over Git and tags | It keeps `Release.Names`; only `movedSources`' loops are shared |
| Its note on `assessedSources` | Missing from the survey | In the survey and in the adapters, with the plan's note in policy.go |
| Choice 1 | I kept joining and leaned on `Counts` for the finished check | Joining kept; the stricter finished row adopted |
| Choice 2 | Left to a test | Reuse's refusal, and the path it is reached by |
| Section 3 | nil against nil undefined; one "not known" for two cases; abbreviations in one order only | Five answers, and both tables |
| "Every reader keeps its outcome" | Not true of the changes | Said in "In short" |
| The line about v0.9.0 not being open | Stale | Gone |
| Second pass: `SourceUnresolved` | Defined as "neither side names a commit", and returned for a plan that names nothing against a build that names its commit | That row answers not known. No reader's outcome changes: the Git readers treat the two answers alike, and `BuiltBy` and `Moved` stay false there |
| Second pass: change 4's unit | "An environment doesn't count" didn't say whether unrelated results there fall too, or whether the release leaves what is required | The unit is the result and its dependents; the release stays required and the result reads as not checked |

## Tests

- **The judgment**, a table in `internal/model` with every row of the two tables above, in both argument orders, and a record from before plans kept the commit or the tag.
- **The old methods equal it**: `Same` is same or unresolved; `BuiltBy` is `AgainstFetched` same; `Moved` is `AgainstFetched` moved, and both are false for a plan that names nothing, whatever the build reported; `TagMoved` is moved between two whole commits. This lands with the judgment and goes with the methods.
- **The readers agree**, one table that calls the readers themselves, each row a pair of plans or a plan and a build. A reader that answers otherwise than this file's table fails by name. It includes:
  - joining against finished coverage: two unresolved Git requests join, and a finished unresolved check doesn't stand for another request, through `QueueCheck` both ways;
  - evidence from several checks through `EvidenceFor`: a newer check missing a cell and an older Git pass, an older pass at a moved tag, a plan with no Git source anywhere and one with a Git source elsewhere, so both branches of `ReadsSources` run;
  - a fetch-mode change named apart from another repository and another ref, and an earlier check's variant build the newest plan lacks;
  - dependents of a moved tag, through one build and through two, with the archive recorded and with it unrecorded and another build showing the move, in evidence and in `reuse.Choose`;
  - presence: both absent, one absent, an old record with no fetched commit, an old record with no tag, and an unmet need, each with its reader's own outcome;
  - change 4, for a Git source and for a tag: one check on one release, the ref moved, a second check on another release. On the earlier release the moved target's result and a dependent's read as not checked, an unrelated target's still counts, the release is still required, and `status` and submit say so. Codex's probe is its failing case;
  - submit: an unreadable current source, a shown move, an abbreviation, another release's tag, a release matched by its commit, and an assessment's commit, each by its concern and by what an attended and an unattended submission do.
- The regressions of #71 and #114, and `TagMoved`'s test that another release's tag is not a moved tag, stay as they are.
- A last search for `Same`, `BuiltBy`, `Moved`, `TagMoved` and commits compared in place (servesubmit.go:294, :312, :354, :381; policy.go:117-122) leaves each one gone or explained. It is a check on the work, not a test.

No failure exposes this item, so the fault sweep ([#129](https://github.com/herbygillot/dockhand/issues/129)) doesn't cover it.

## How it is built

1. The judgment, its table, and the test that the old methods equal it. No caller changes.
2. One reader a commit, each with its rows of the readers' table: `SourcesCover` for joining; `Covered` (change 1); evidence's `fill`, `readSources` and `Plan` (changes 2, 3 and 4, a commit each); reuse (`Carry`, `TagMoved`, `builtAgainst`); `judgeSource`, `FetchedWords` and the plan's note; `movedSources`, `preparedSources` and `assessedSources`.
3. The old methods deleted, and the last search.

After the publication record ([#191](https://github.com/herbygillot/dockhand/issues/191)) and before the check's lifecycle ([#193](https://github.com/herbygillot/dockhand/issues/193)), whose reuse changes read it.

## Not checked

- Change 4 was run by Codex through `EvidenceFor` and `movedSources` on test fixtures. A whole submit and a real Portfile were not run.
- Codex's probes used test fixtures. They show the engine accepts differing source maps for one tree; they don't show a real Portfile changing how it fetches with no edit. Changes 1 and 2 don't rest on that.
- Every caller of `coversPlan` beyond `Covered`, and `preparedSources` line by line.

## Usage docs

- `docs/usage/checks.md`, where it says when an earlier check stands for a new one: a check whose Git source couldn't be read stands for itself alone, and a result built before a tag moved, or on a release an earlier check built from another commit, is built again. No flag or message changes: the plan already says a source couldn't be read and that a moved tag builds again.
- `docs/architecture.md` names the judgment where it describes a plan's sources.
