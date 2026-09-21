# Review: architecture and code organization

Date: 2026-09-21. Reviewed the clean working tree at `main` after the day's
command-surface changes. The question asked was whether a week of rapid feature
work has let the packages get out of hand, with `internal/macports` and its
sub-packages and `internal/cli` named as the suspects. The measurements answer
that the perception is half right: `macports` did not grow, and the growth that
did happen, in `cli`, `workflow`, and `app`, is of two specific kinds that have
known remedies. These observations are proposals for triage, not accepted
implementation work.

## What the measurements say

Sizes are non-test lines. The 2026-09-14 column is the tree before the
2026-09-16 structural split of the bump machinery.

| package | 2026-09-14 | now | files | exports | fan-out | fan-in |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `workflow` | 3,568 | 7,107 | 46 | 60 | 13 | 4 |
| `macports/portedit` | (new) | 4,236 | 27 | 32 | 13 | 6 |
| `cli` | 1,254 | 3,746 | 25 | 17 | 20 | 1 |
| `state/sqlite` | | 2,490 | 14 | 64 | 3 | 1 |
| `app` | 388 | 1,644 | 21 | 46 | 32 | 2 |
| `macports/portindex` | (new) | 1,678 | 8 | 24 | 7 | 8 |
| `record` | | 1,596 | 17 | 121 | 0 | 31 |
| `macports` root | 618 | 627 | 14 | 44 | 1 | 21 |
| other `macports/*` (nine) | (new) | 100 to 1,160 each | | | | |

The tree is 45,236 lines of code against 32,848 of tests. Fifty interfaces are
declared, ten of them in `state` and eight in `macports`, so the seams are
explicit. The two dependency-contract tests, `verify`'s and `assess`'s, were
joined by `cli`'s on 2026-09-20 and all three pass. No package has a cycle, and
no edge added in the last week runs against the layering described in
[components.md](../components.md) except the two the 2026-09-20 structural check
found and fixed.

**`macports` did not explode.** The root is the vocabulary package and has moved
nine lines in a week. Its eleven sub-packages were carved out on 2026-09-16, and
ten of them are small and single-purpose: `distfiles` 273, `fidelity` 357,
`patchcheck` 335, `source` 401, `survey` 154, `version` 100, `dependency` 1,160
for the Go and Cargo generators, `portindex` 1,678 for one concern. The eleventh,
`portedit`, is the subject of item 1.

**The growth is above `macports`.** `cli` tripled, `workflow` doubled, `app`
quadrupled. In `cli` the growth is repetition: four functions over 150 lines and
one sequence written thirteen times. In `app` it is orchestration that leaked
upward from `workflow`. In `workflow` it is size with shape, which is the least
worrying of the three.

## 1. `portedit` is four services wearing one struct

`portedit.Service` has six fields and 36 methods across 19 files. Grouping the
files by what they do and counting which fields each group touches:

| cluster | files | lines | `Service` fields used |
| --- | --- | ---: | --- |
| observation | `observation.go`, `platform_observations.go`, `profiles.go`, `unmodeled.go`, `host_access.go` | 974 | `Ports` only |
| archives | `artifact_plan.go`, `artifact_apply.go`, `artifact_assess.go`, `archives.go`, `download.go`, `checksums.go`, `patches.go` | 954 | `HTTP`, `MaxDownloadBytes`, `DownloadTimeout` only |
| dependencies | `dependencies.go`, `dependency_source.go`, `go_toolchain.go`, `git_source.go` | 679 | `DependencyTools`, `Manifests`, `Ports` |
| version editing | `version.go`, `version_edit.go`, `version_transform.go`, `probe.go`, `assessment.go`, `revision_reset.go` | 949 | `Ports` |
| core | `prepare.go`, `source.go`, `message.go`, `release_scope.go` | 666 | `Ports` |

The rest of the package reaches the observation cluster through three entry
points, `contextProfiles`, `observeProfiles`, and `tolerateExplainedProbes`,
called eleven times in total. The archive cluster's three fields are used by
nothing else. That is a package boundary already drawn in the field usage:
`portedit/observe` for modeled evaluation, host reads, and Darwin profiles;
`portedit/archives` for downloads, checksum refresh, and the archive plan;
`portedit/deps` for the manifest and toolchain work. Each becomes a small
service holding its own fields, and `portedit` keeps version editing and the
preparation orchestration at about 1,600 lines. The split is mechanical, changes
no behavior, and is the highest-value organizational change available.

## 2. `cli` repeats one sequence thirteen times

Every change and action command runs the same sequence: resolve the destination
and reference flags, build services, optionally adopt a branch, bind a request,
submit it, attach at a milestone. It appears as 13 `r.build` prologues inside 27
`RunE` closures. `changeCommands` already tables three commands over this
sequence and is 224 lines; `correctionCommands` repeats it at 151 lines,
`verifyCommand` at 95, and `publicationCommand` again. The table in
`changeCommands` is the right idea applied to a third of the commands.

One `run` type taking a flag set and, per command, a `bind` function from
services to a request would generalize that table to all of them. It removes
several hundred lines, and it makes the sequence testable once, where today the
milestone choice under `--detach`, the adopt-before-bind ordering, and the
publication-flag guard are each asserted per command or not at all.

The package's fan-out of 20 is the highest in the tree. Twelve of those imports
reach past `app`, and most are for constants: `verify.ProviderGitHub`,
`portedit.Blocked`, `portedit.Unsupported`, `upstream.Unknown`, used to decide
exit codes and hints. The CLI re-derives "needs attention" from the internals of
a result rather than asking the result. If `assess.Result` and its kin carried
that verdict, three imports go and the contract test tightens.

## 3. `app` has become a second intake layer

`app` is the composition root, and its `Services` struct, ten wired things, is
what a root should hold. Its 1,644 lines are not. `BindPreparation` is 99 lines
and `prepareOnto` 76; between them they look up the open contribution, fetch
master, run the continuation check, resolve the release, prepare onto an
existing change, and bind a correction. That is intake orchestration, and it
lives outside `workflow`, which [components.md](../components.md) says accepts
requests and binds their inputs. The 2026-09-17 architecture review deferred an
intake-and-driver split of `workflow` until its rules stopped moving; intake
grew in `app` instead.

The request types show the same thing from the other side. `app.Preparation`
has 14 fields, `workflow.PreparationRequest` 23, `record.JobSpec` 19, and they
carry one intent translated three times; the tree has 17 `Request` and 15
`Options` structs. The `app` type carries no meaning the `workflow` type lacks.
Moving `BindPreparation`, `prepareOnto`, and the continuation decision into
`workflow` as intake, and having the CLI build the workflow request directly
while intake fills the source, platform, and author, returns `app` to wiring
and removes one of the three translations.

## 4. `workflow` is large but shaped; do not split it into siblings

Its 46 files fall into five clusters: intake and binding (about 1,300 lines),
execution (1,600), lifecycle operations (1,700), retention and cleanup (600), and
status (280). Every cluster is dense with `Engine` methods on an engine of eleven
dependencies; `adopt.go` references it 64 times, `contribution_lifecycle.go` 48.
Sibling packages would mean passing the engine around or an interface with fifty
methods, which is the wrong cut. The right cut is the one the package already
uses: leaf sub-packages such as `policy`, `preparation`, and `view`. The retention
cluster is the next leaf, since it uses the engine mostly to reach the store and
the repository, and `status.go` with `scope.go` could follow. Neither is urgent.

The package's one real smell is not size. `Engine.Provider` and
`Engine.Providers` coexist, with a comment explaining that the map routes
persisted names and the single field is the fallback. That is one routing
decision written as two fields.

## 5. Evidence is rendered three times

`cli/status.go`'s `renderStatus` is 247 lines walking a job's evidence by hand:
the GitHub run, its jobs, the environment, the developer tools, the test policy.
`publish/body.go` walks the same evidence for the pull request's "Tested on"
section. `cli/summary.go` walks it a third time for the completion line. Three
renderings of one set of facts, each maintained separately; the pull request's
was reshaped on 2026-09-20 and the other two were not.

An evidence projection in `workflow/view`, one function that words the facts,
consumed by the three formats, is the move the phrasebook made for job words on
2026-09-19. It removes most of `renderStatus` and the duplication in `body.go`.

## 6. Smaller observations, not obviously worth acting on

`record` exports 121 names and has 31 importers. It is data, and the count is
what a shared vocabulary costs; nothing in it has grown logic.

`state/sqlite` is 2,490 lines with 64 exports behind ten interfaces in `state`.
It is one concern and its files are by table.

`verify/tart` is 1,710 lines with a fan-out of 15; it is a provider, and
providers are where concrete integrations concentrate by design.

`cli/status.go` at 479 lines holds `renderStatus` and the launch of the live
table, not the table itself, which is `internal/tui`; the two are not
duplicates.

## Suggested order

1. The `portedit` split (item 1): mechanical, no behavior change, and the
   package is not under concurrent work.
2. The command pipeline (item 2): mechanical in effect, large in lines removed,
   and it makes the run sequence testable.
3. Intake into `workflow` (item 3): a design change of moderate size; it should
   land before the rules in `app` grow further.
4. The evidence projection (item 5).
5. Results that carry their verdict (item 2, second part), which lets the `cli`
   contract test tighten.
6. The retention leaf and the provider-routing field (item 4), when `workflow`
   next needs touching there.

Items 2, 3, and 5 touch `cli`, `app`, and `workflow`, where the command-surface
work of 2026-09-21 is active, and need sequencing with it. Item 1 does not.
