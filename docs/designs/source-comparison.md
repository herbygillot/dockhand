# One judgment of a check's resolved source

A core item for v0.9.0, [#192](https://github.com/herbygillot/dockhand/issues/192). The issue says where it stands; this file is the design, architecture's of 2026-11-11, read at main ded03e2d5. It is a draft: Codex reviews it against the code before the person sees it for adoption (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread). Nothing here is built before the person opens v0.9.0's core work.

## In short

- A check's plan records, for each target in each environment, the commit a Git source or a GitHub tag named when the check was planned. Seven places then compare one such reading with another, each in its own words.
- They become one judgment in `internal/model` with four answers: same, moved, other, not known.
- Every reader keeps the outcome it has today. What is shared is the reading of the facts. One table says what each reader does with each answer, and one table test holds the readers to it.
- No stored record changes, and no message.

## What the code does today

| Where | What it compares | How |
| --- | --- | --- |
| `checks.SourcesCover` (admission.go:54): joining a queued or running check (`PlanCovers`, queue.go:168, :170) and counting a finished one for a plan (`coversPlan`, engine/evidence.go:424) | two plans, per environment and target, Git and tag | `GitSource.Same`, and the source present in both or in neither |
| `reuse.Carry` (current.go:77) | the plan's Git source, and the commit an earlier build fetched | `BuiltBy`; a build that fetched with Git stands for no plan that doesn't |
| `reuse.Target.TagMoved` (choose.go:42), read by `Choose` (:85) and `builtAgainstMovedTag` (:231) | the plan's tag, and the tag an earlier build's check planned | one repository and ref, both commits known, and they differ |
| `reuse.builtAgainst` (choose.go:210) | a dependency's Git source, and the commit that built the archive a dependent had active | `BuiltBy` |
| `evidence.Counts` (evidence.go:982) | the newest plan's Git source and what a result's build fetched; the result's own plan and what it fetched | `BuiltBy`; `Moved` |
| `checks.judgeSource` (runner.go:1366), `FetchedWords` (words.go:83) | the run's plan, and what its own build fetched | `Moved`, `BuiltBy` |
| `engine.movedSources` (servesubmit.go:272), `preparedSources` (:341) | the plan a submission rests on, and the source read again now; an update's release, and that plan | commits compared in place, once for Git and once for tags |

Three methods on `model.GitSource` (`Same`, `BuiltBy`, `Moved`, source.go:53-78), one on `reuse.Target`, and the comparisons written in place in the engine hold one idea between them. #71 and #114 were each one reader missing what another already knew.

## The judgment

In `internal/model/source.go`, beside the facts it reads:

```go
// SourceStanding is what one reading of a source says of another.
type SourceStanding int

const (
	SourceUnknown SourceStanding = iota // one repository and ref, and one of the two names no commit
	SourceSame                          // one repository and ref, one commit
	SourceMoved                         // one repository and ref, another commit
	SourceOther                         // another repository or ref, or only one of the two is fetched this way
)

// SourceAgainst judges two resolutions; nil is a target not fetched this way.
func SourceAgainst(have, want *GitSource) SourceStanding

// AgainstFetched judges a resolution and the commit a build says it
// fetched. It never answers other.
func (s GitSource) AgainstFetched(fetched ObjectID) SourceStanding
```

- Another repository or ref is decided first, whatever the commits: an older release's `v1.7.1` and a newer one's `v1.9.2` are other, never moved. That is `TagMoved`'s rule, and it becomes everyone's.
- An abbreviated commit and a whole one are the same where the whole one begins with the abbreviation, as `BuiltBy` has it.
- Two readings that both name no commit are not known. `Same` answers true for them today; the table below keeps what follows from that, by name.
- It reads nothing but its arguments. Reading a repository's refs stays in the engine (`resolveGit`, `resolveTag`), and following a moved source through the builds that had its archive active stays in `reuse.AgainstOtherSources`.

`Same`, `BuiltBy`, `Moved` and `TagMoved` go once their callers ask the judgment.

## What each reader does with each answer

The readers differ for a reason the code holds and nowhere states:

> For a source fetched with Git, the Portfile's files don't say what was built, so "not known" stands for nothing. For an archive fetched by a tag, the Portfile's checksums say what was built, so only a move that is shown unseats a result.

| Reader | same | moved | other | not known |
| --- | --- | --- | --- | --- |
| Join a queued or running check | joins | doesn't | doesn't | joins where neither plan could resolve it; doesn't where one could |
| Count a finished check for a plan | counts | doesn't | doesn't | as joining, and each Git result must then pass the next row |
| Count a result as evidence, Git (`Counts`) | counts | doesn't | choice 2 below | doesn't |
| Reuse a build, Git (`Carry`) | reuses | builds | builds | builds |
| Reuse a build, tag (`Choose`) | reuses | builds | reuses | reuses, unless another build of the target shows the tag moved |
| A build that had the source's archive active | follows it | builds | | Git: builds. Tag: follows it |
| The run's own verdict (`judgeSource`) | stands | fails at fetch | | stands for its own check alone, and says so |
| Submit, Git | says nothing | concern `source-moved` | | read now and unreadable: concern `source-unknown`, which holds serve and a person sees. Unknown in the plan: says nothing |
| Submit, tag | says nothing | concern `tag-moved` | | the same, except a ref that is an abbreviated commit |

Two places where I am choosing, and the reviewer should push:

1. **Joining with neither side resolved.** Two requests whose plans both failed to read the repository join today, because `Same` compares two empty commits as equal. I keep it and give it its name: joining compares requests, and claims nothing about the source. The evidence is asked for at the result (`Counts`), where a Git source that isn't known counts for nothing. For a tag, two plans that couldn't read the repository count as the same, by the rule above.
2. **A change between Git and an archive.** `Carry` refuses a build that fetched with Git for a plan that doesn't (current.go:84). `Counts` has no such clause (evidence.go:985). I have not shown that a result can reach `Counts` in that state with the same files. The table test below settles it; if one can, `Counts` takes reuse's answer.

## Tests

- **The judgment**, a table in `internal/model`: equal commits, moved commits, another repository, another ref, one side unresolved, both unresolved, a record from before plans kept the commit, an abbreviation against a whole commit that begins with it and against one that doesn't, and a source present on one side only.
- **The readers agree**, one table over the readers themselves, each row a pair of plans or a plan and a build: the rows above, a difference between two environments of one plan, a target `--only` left out, and a dependent built against a moved source's archive, through one build and through two. Each row names the judgment's answer and each reader's outcome from the table in this file. A reader that answers otherwise fails by name.
- The regressions of #71 and #114, and `TagMoved`'s test that another release's tag is not a moved tag, stay as they are.

No failure exposes this item, so the fault sweep ([#129](https://github.com/herbygillot/dockhand/issues/129)) doesn't cover it.

## How it is built

1. The judgment and its table. `Same`, `BuiltBy` and `Moved` are written on it, and nothing else changes.
2. One reader a commit: `SourcesCover`; reuse (`Carry`, `TagMoved`, `builtAgainst`); `Counts`; `judgeSource` and `FetchedWords`; `movedSources` and `preparedSources`, whose two loops become one over Git and tags.
3. The readers' table, and the old methods deleted.

After the publication record ([#191](https://github.com/herbygillot/dockhand/issues/191)) and before the check's lifecycle ([#193](https://github.com/herbygillot/dockhand/issues/193)), whose reuse changes read it.

## Not checked

- Whether a result built with Git can reach `Counts` for a plan that fetches an archive (choice 2).
- `preparedSources` past its first lines, and every caller of `coversPlan`.
- A case where two environments of one plan resolve one ref to different commits other than through a Portfile's platform conditions.

## Usage docs

None: no command, flag or message changes. `docs/architecture.md` names the judgment where it describes a plan's sources.
