# CLI surface

**Status: provisional.** This replaces §4 of the superseded design document,
which was written when `update` was the only intent. Companion documents:
`intents.md` (what dockhand can be asked to do), `reading.md` (how Portfiles
are read and edited), and `verification.md` (where verification runs). Claims
below marked with a source draw on `field-evidence.md` (fifty real port
updates) and `pr-evidence.md` (review feedback on `macports-ports` PRs).

---

## Grammar

Every command has the shape `dockhand <verb> <selector>`.

**Selectors are borrowed from `port`, wholesale.** `maintainer:me`,
`category:python`, and `depof:openssl` work exactly as they do in `port`. A
bare list of port names is just a selector that happens to enumerate. An
invocation with no selector at all resolves to `maintainer:me`. This gives
dockhand one targeting vocabulary, shared across every verb, and it is the
vocabulary users already know. Flags like `--mine` or `--all` or `--category`
are deliberately absent; inventing them is how a tool ends up with two
targeting vocabularies.

**Verbs come in two kinds, and they must not share a slot.** An *intent* names
a desired end state: bump this port, refresh these checksums. A *stage* moves
work through the pipeline: apply, verify, promote. The superseded design had
one intent, so both kinds fit in the verb position without visible conflict.
With ten intents they collide. The resolution:

> The intent takes the verb slot. Its output is always a plan, and the stages
> are separate verbs that consume the plan.

```
dockhand bump gcc14 --to 1.4.2           # plans, then applies
dockhand bump gcc14 --to 1.4.2 --plan    # emits the plan; changes nothing
dockhand apply <plan>                    # write a saved plan into the worktree
dockhand verify <plan> --depth=build     # run verification
dockhand promote <plan>                  # branch, commits, PR
```

An intent's output is still always a plan — that is what makes the change
verifiable, and applying it runs the same prediction check `apply` does. What
`--plan` decides is whether the plan is carried out or handed back.

**Amended (2026-08-31, D21).** The unit an intent produces is now a
**branch**, not a plan. A quick `dockhand bump jq` mints
`dockhand/jq-<new version>` directly in the object database — no worktree,
no checkout, the user's HEAD never moves — and submits verification against
the tip, staged straight from the objects; the plan survives as internal
interchange only, never as a file the user handles. A user wanting to add
changes checks the branch out themselves; `status` warns when a tip has
moved past the sha verification tested, `verify` cancels the stale run and
resubmits the tip, and `promote` refuses an unverified tip. The stage
verbs consume branches and shas rather than plan files — `verify` tests a
commit, whoever made it; `status` reconciles the `dockhand/*` namespace;
`promote` pushes the branch it finds, and first searches upstream's
open PRs by the `<port>:` title convention: an identical title is
refused as a duplicate (exit 20, `--no-pr-check` overrides), a
same-port PR is surfaced as a note, and a clean search checks the
template's other-open-PRs box. A branch whose own PR is already open
is re-promotion: the push updates that PR instead of opening a second. The deliberate opt-out is `--in-place`:
edit the Portfile where the user stands, uncommitted, minting nothing — for
the user folding dockhand's mechanical edit into their own workflow, and the
only write mode a non-git tree gets (with a loud warning). Lifecycle: an
intent finding an in-flight branch for its port refuses and names it;
`--replace` replaces it — verification canceled, notes removed — but
only a branch that is exactly the minted commit: work the user added
past the mint refuses, and `discard` stays the explicit act for
dropping it. Re-deriving a port at the version it already carries is a
separate question and now a separate flag, `bump --recheck`, which also
makes the verification build from source: the archive matching an
unmoved version predates the change. `promote --force` keeps its name
and is a different act — force-push the fork copy (with lease) and
refresh the open PR's title and body — because it moves a branch
dockhand published rather than destroying one it minted. `status` reports each
promoted branch's PR state and performs one deletion — a branch whose
PR merged is cleaned, announced, its fork copy deleted with it — while
`clean` remains the explicit sweep: PR state from
GitHub decides merged (the project's merge styles rewrite shas as commits
land, so ancestry proves nothing), confirmed by byte-comparing the touched
paths against upstream; closed-unmerged branches are kept and flagged. The
pipe below, and every plan-file argument in this document, describe the
superseded surface.

**Riders.** Every headline intent is examined for housekeeping it could
carry — one rule today, the editor modeline a Portfile opens without — and
a rider rides only on a double proof. The structural half is that the edit
touches comment and whitespace token spans only, in the tree it was
computed against *and* in the tree it produces: a boundary insertion must
write whole lines, so bytes cannot join the token before or after them,
and the bytes it wrote must still be occupied by no command once they are
there. The semantic half is that the shadow evaluation predicts exactly
what it predicted without the rider. Neither is enough on its own. A whole
command written into the gap between two commands has as innocent a span
as a modeline, and the prediction cannot see it either — the delta
observes the twenty metadata fields, and a `configure.args-append` is not
one of them — so the re-read of the new tree is what refuses it; a comment
line ending in a backslash continues onto the next line and swallows the
command below it, which no span can report and only an evaluation can; and
a rewritten literal that happens to leave the evaluation where it was has
an identical prediction and is plainly not housekeeping. A rider that
fails any of them is dropped, the headline change stands, and `--debug`
names the rule. The proofs certify that a rider is *inert*, never that it
is *right*: a rule that inserts something already present is a rule bug,
and no proof here will catch it. Riders never trigger a verification:
nothing a build could notice has changed, by construction. `--riders`
makes housekeeping the whole change — the headline is dropped, the plan is
named after the rules it carries, the verb's own parameters and its
caution are not read, and the branch is minted without a verdict being
asked for. `--no-riders` carries none and withholds none.

**Superseded (2026-08-30).** This section originally argued that emitting a
plan should be the default because it is both convenient and safe, so no flag
was needed for dry-run. The convenience half did not survive contact with the
tool: the overwhelmingly common case is one port, one bump, and it cost three
commands and a temporary file. Safety is preserved by verification rather than
by inaction — see D16.

For interactive single-port work, three commands is tedious, so `apply` reads
a plan from stdin:

```
dockhand bump gcc14 --to 1.4.2 | dockhand apply -
```

The pipe is available when you want it. Sweeps never want it.

## Intent verbs

```
bump <sel> [--to <version> | --latest] [--recheck]   # no flag: latest
bump-revision <sel>                # alias: revbump
bump-revision --for <branch>       # the plural invocation: accept a revbump proposal
bump-epoch <sel>
refresh-checksums <sel>            # never auto-promotable; carries its cause
vendor <sel>                       # regenerate vendored block (T3)
deps <sel> --add/--remove <spec> [--kind lib|build|run] [--variant <v>]
patches <sel> [--drop-obsolete]
modify <sel> --set <field>=<value> # scalar, no-cascade fields only
obsolete <sel> --replaced-by <port>
migrate <sel> --idiom <name>
```

Notes on naming and shape:

- **`bump-revision --for <branch>` is the same verb asked a different
  question.** The edit is bump-revision's edit; what changes is who chose the
  ports and who wrote the reason. On the single-port road a person names both,
  and with `--for` the proposal a verification measured holds both — which is
  why it needs no `--reason` and takes no port. It never mints: the members
  land as one more commit on the branch that already carries the change,
  because they move for one reason and it is the same reason.

- **`bump-revision` and `revbump` are the same verb.** The canonical name
  follows the family's shape (`bump`, `bump-revision`, `bump-epoch`); the
  alias honors the community's own vernacular — reviewers and commit
  messages say "revbump" — by the same borrowing principle that took
  `port`'s selectors. Both spellings are permanent; neither is deprecated.
- **`bump-revision` writes the line when the Portfile has none.** Near a
  fifth of the tree carries no `revision`, and such a port is at revision 0
  implicitly, so the value was never the question — the placement was. The
  positions in the tree do not agree, but the relation does: a revision sits
  under the line carrying the version. dockhand writes it there, in that
  line's own value column, and the shadow evaluation is the proof it landed
  where Tcl reads it. Every shape where the placement would have been a guess
  declines `revision-shape-ambiguous` (exit `10`) and the detail says which,
  and these are all of them: a Portfile with subports, whose one inserted
  line would move all of them; a port evaluating to a non-zero revision with
  no line to increment; a version carried by a `set` variable; a version
  carrier written inside a conditional rather than at the top level; a
  carrier sharing its line with something before it, so there is no column
  to write under; a carrier whose line is unterminated, so there is no line
  after it to write into; and a version carrier that could not be located at
  all, which folds the location decline's own type into the detail. A
  Portfile that does write a revision and writes it computed is not this
  case at all, and keeps its own location decline.
- **`refresh-checksums`, not `checksums`.** `port checksum` already exists,
  and it *verifies* checksums rather than refreshing them. Reusing `port`'s
  vocabulary to mean something different would be worse than not borrowing at
  all, so the longer name is the honest one.
- **`modify` is bounded by a rule, not by taste.** A field is admissible only
  if setting it has no cascade and no semantics dockhand would have to own
  (see the `modify` boundary in `intents.md`). That excludes `version`,
  `revision`, `checksums`, `depends_*`, `patchfiles`, and `variants`, each
  for a different reason. The name itself is unsettled: `modify` promises
  more generality than the rule allows, and `set` may be more honest.
- **`obsolete` targets a port, not a file.** A Portfile can obsolete its
  top-level port while its subports stay live (`libftdi` does), so the
  selector resolves to ports and the edit is sometimes the insertion of a
  guarded block rather than a whole-file rewrite. Obsoleting also cascades
  into `bump-revision` and sometimes `bump-epoch`.
- **`--variant` appears only where it means something.** `deps` takes it;
  `bump` does not. This is the variant-scope question surfacing as a flag:
  intents that touch variant-relative metadata need the scope, and the others
  do not.

### `latest` is a query, not a value

`bump maintainer:me` — latest by default — is the flagship invocation, and the update
signal underneath it is unreliable in both directions. Across fifty real
updates, Repology reported updates that did not exist for sixteen ports,
missed newer versions for three, and for one port returned a version belonging
to a different project entirely.

So `latest` cannot be sugar for a version string. Resolving it is a step that
can fail, can disagree across sources, and can name the wrong project — which
means it produces findings of its own before any Portfile is read. This
matters most at sweep scale: `plan` is the expensive stage, and an unreliable
resolver is the difference between one expensive pass and a third of one
wasted.

---

## The pipeline

### `plan` reads the world

An early draft kept `apply` off the network entirely. That line cannot hold:
computing a bump's checksums requires fetching the distfile, and regenerating
a T3 block requires `cargo2port` or `go2port` reaching their registries. If
those ran at apply time, `apply` would be a network operation for the flagship
intent.

The resolution is to move all reads into `plan`. It resolves `latest`, fetches
distfiles, computes checksums, and invokes the block generators. By the time a
plan exists, its diff is complete — which also settles an older question about
plans being partial above T2 (no diff exists until a generator runs; now the
generator has always run). `plan` is therefore the expensive stage, and that
is the correct place for the expense: its output is the artifact you keep.
`apply` then writes only to the working tree, and a plan whose base Portfile
has changed underneath it is rejected rather than applied.

### Rings of consequence

The old invariant — `promote` is the only stage that touches the network —
died when fork CI arrived, because `verify --where=fork-ci` must push a branch
before `promote` ever runs. What that instinct was actually guarding was never
the network. Pushing to your own fork notifies nobody and obligates nobody;
opening a PR pings maintainers and creates a review obligation. The guarded
resource is **other people's attention**.

| Ring | Contains | Written by |
|---|---|---|
| 0 | the plan, immutable once emitted | `plan` |
| 1 | the worktree | `apply` |
| 2 | things the user owns and can delete: ephemeral prefixes, the sandbox, VM images, fork branches, local caches, dismissal state | `plan` (caches), `verify`, `debug`, `dismiss` |
| 3 | other people's attention: the upstream PR, notifications, review obligations | `promote`, and only `promote` |

Each stage writes at most its own ring. This is the principle "never outrun
review capacity" enforced by structure rather than by policy, and it cleans up
two older imprecisions: `plan` "writes nothing" was always approximate (it
fills distcaches, which are ring-2 state under D8), and dismissal state now
has a named home instead of being an unacknowledged D8 violation.

Fifty ports of ordinary maintenance produced no case that wanted to violate
this model. The one apparent violation — fork pushes — is what forced the
model to be stated correctly.

Under D21 the rings hold with their contents renamed: ring 0's plan is
internal rather than emitted, and ring 1 is the branch with its worktree —
still things only the user's own repository holds. The boundaries themselves
are unchanged.

### A branch changes rings

`verify` force-pushes its branch freely. Re-runs are `push -f`, and the CI
workflow's `cancel-in-progress` setting keeps them cheap. That is fine while
the branch is ring 2. But when `promote` opens a PR from that branch, the
branch becomes the PR's head — it is now ring 3, and force-pushing it would
rewrite an open PR under a reviewer's feet.

> `verify` never pushes to a branch that backs an open PR. After `promote`,
> every update to that branch goes through `promote`, because updating it
> spends attention again.

Three consequences are settled alongside this rule:

- **Branch naming** follows the convention observed on a real fork:
  `<port>-<version>`, with no `dockhand/` prefix. The ref becomes the PR
  head, visible to reviewers, and provenance disclosure belongs in the PR
  body (per D9), not in the ref name. **Amended (D21):** the branch is
  `dockhand/<port>-<version>` locally *and* on the fork — one name
  everywhere. The observed no-prefix convention was habit, not policy; ref
  names are not policed in review, and the prefix in a PR head ref is honest
  provenance in D9's spirit. Identity means no refspec, a bare `git push`
  that works from the worktree, and fork-side pruning scoped to a namespace
  dockhand owns on both ends.
- **Branch pruning** needs no persistent state. Whether a fork branch is
  stale is derivable — its PR merged or closed — so sweeping ring-2 garbage
  is consistent with the "deliberately absent" list below.
- **Async shape.** `verify --where=fork-ci` waits and shows progress by
  default. `--no-wait` submits and returns, with results arriving later as
  findings via `status --source=fork-ci`. This reuses the findings model
  rather than inventing a job system.

---

## Verification

Two orthogonal axes, never one boolean:

- **Depth** — `--depth=patch|build|artifact|matrix|dependents`
- **Location** — `--where=local|vm|fork-ci|auto`

They are genuinely independent: build-depth verification can run on a laptop
or on a runner, and a boolean cannot express "build depth, but remotely,
because this port is heavy." The default is `--where=auto`, inferred from
observed build times rather than asked.

`local` means an ephemeral prefix, born clean for the run — not the user's
`/opt/local` and not a standing test prefix. `verification.md` covers the
backends in detail; the CLI-relevant facts:

**"CI" is two different things, and only one is a location.** The *buildbot*
runs post-merge on its own schedule and cannot be directed; it is a findings
source, never a `--where` value. *Fork CI* is drivable: the `macports-ports`
workflow triggers on any branch push (`push: branches-ignore: master`), so
pushing to a personal fork runs the full lint-and-build matrix on macos-14,
15, and 26, with `mpbb` selecting the changed ports and a fresh MacPorts
bootstrap on every runner. No PR is needed. This was verified against a real
fork: 405 runs, every one push-triggered on a feature branch. GitHub Actions
is free with unlimited minutes on public repositories for every account tier,
macOS runners included, and forks of public repositories are public.

Fork CI's limits, stated at plan time rather than discovered mid-batch:

- **Enablement.** Actions are off by default on a fresh fork until the owner
  enables them once. This is probe-able via the runs API — a fact about the
  machine, in D11's discovered tier.
- **Concurrency.** The free plan allows five concurrent macOS jobs and the
  matrix costs three per push, so fork CI verifies roughly one branch at a
  time. It is a per-change gate, not a sweep engine.
- **Coverage.** The standard runners are all Apple Silicon: three macOS
  versions, zero Intel.
- **No preserved environment.** The runner evaporates, so D7's
  debug-in-snapshot disposition never attaches to a fork-CI finding.

Three consequences for the design. Fork CI answers D4's third proposition —
declaration completeness in a pristine environment — for free, on three OS
versions, which is most of what the VM executor existed to provide; the VM's
remaining value narrows to interactivity, pinned environments, and freedom
from the concurrency cap. The branch pushed for verification is the same
branch `promote` turns into a PR, so a promoted branch arrives pre-verified.
And the `--where` values are not interchangeable executors: local is
synchronous and preserves its environment on failure, fork CI is asynchronous
and returns only findings. D5's single-interface assumption does not survive
that asymmetry; `verification.md` carries the re-examination.

One depth value is suspect. `artifact` — run the built binary and check its
self-reported state — may be a fourth verification *proposition* rather than a
rung; see the open questions.

---

## Findings

```
dockhand status <sel> [--source=local|ci|all]
dockhand bump-revision --for <branch>    # accept a revbump proposal: one more commit
dockhand dismiss <branch|port>           # record that you looked and said no
dockhand debug <finding-id>              # shell into the preserved environment
dockhand bump --from-finding <finding-id>
```

A finding is answered per **branch** and not per finding id. The two answers
are the two verbs above, and both are things a person types: `bump-revision
--for` builds the cohort the measurement put forward, and `dismiss` records
the refusal — which is an answer worth keeping rather than an absence, since a
finding that vanished when declined would be proposed again on the next pass.
Nothing else answers one, and `24` above is what holds an unattended
publication until something does.

`status` is the reflex command. It deserves its name early, even in a version
that reports only one kind of finding.

**One naming collision is avoided deliberately.** The design elsewhere calls a
finding's third disposition "promote to intent," but `promote` already means
"open the PR" — the one verb that touches someone else's repository. Two
promotions in one CLI, one of them that one, invites real mistakes. The
disposition is spelled `--from-finding` on the intent verb instead, which also
makes the propose-never-execute rule structural: a finding seeds a plan, and
that plan must still be applied and promoted deliberately.

Field evidence imposed three requirements here:

- **Findings originate off the local machine.** Three ports built locally and
  failed on the buildbot, discovered only after merge. "CI is the authority"
  always implied remote findings; `--source` gives them a place to land.
- **A CI finding needs a baseline before it can be attributed.** One port
  appeared to have regressed from that session's change; the failures dated
  from January, and telling those apart required per-builder history. Without
  a baseline, every pre-existing red builder gets charged to the most recent
  change.
- **A finding is not always about the port under intent.** `copilot` failed
  because its build dependency `packr` had been broken for months. The port a
  finding is *about* and the port *under intent* are different fields, and
  `status` must show both.

---

## Publication

### `promote` emits commits, not just a branch

Review culture polices the **commit**, not the diff — commit hygiene is a top
source of blocking feedback (`pr-evidence.md` §2–3). A plan headed for
promotion therefore carries a commit plan:

- **Grouping.** One logical change per commit. A bump and its dependent
  revbumps are different logical changes sharing one PR: the bump is one
  commit, the grouped revbump of N dependents is one more. Observed at N=9.
- **Messages.** `portname: short description`, enforced verbatim in review; a
  wrong format is a blocking verdict by itself. Multi-port commits join the
  names: `arm-binutils, m68k-binutils: …`. For bumps, the project convention
  (`<subport>: update to <version>`) is already what `port bump --patch`
  prints.
- **Titles.** The PR template auto-detects the change type from the title —
  an update must contain ": update to" — so titles are load-bearing for the
  project's own tooling.
- **Trailers.** Trac references go in the commit body as
  `Closes: https://trac.macports.org/ticket/NNNNN`, exactly. The trailer comes
  from `--closes <ticket>` at plan time — every intent verb takes it — because
  only a commit dockhand is about to write can carry one. `promote --closes`
  is the late spelling: it reaches the PR body alone, leaves the commit-message
  checklist box unchecked, and says so on stderr. Findings carry ticket URLs
  when they originate from one.

All of this is mechanical, and it is the cheapest goodwill available: the PR
evidence found review labour dominated by convention-incompleteness rather
than by wrong edits. That is precisely the labour a convention-aware tool
removes.

`promote` can also fill the PR template truthfully — it knows exactly which
verification rungs ran, which is what D9's provenance requirement asks for.
The bar is lower than feared: "Tested on: CI only." was accepted without
comment in a merged nine-port PR, and a maintainer's bot PR with a full
template merged without friction. Candour is the accepted currency;
unverified assertions are what draw "did you verify this?"

### The 72-hour window, and who actually merges

The project's published update policies (guide.macports.org, "Port Update
Policies") set the rules for the far side of `promote`: **nomaintainer** ports
may be updated by anyone; **openmaintainer** ports allow minor updates by
others, but the PR still waits 72 hours for maintainer review;
**maintained** ports require maintainer approval, with a 72-hour timeout
after which committers may proceed if the commit message documents the
timeout.

dockhand's primary persona is a maintainer *without* commit access. `promote`
opens the PR; a core-team committer merges it. That reading changes the shape
of everything downstream:

- **Window expiry enables a committer, never the user.** The policy's
  "proceed after a documented timeout" language is committer-only. For the
  dockhand user, the 72-hour window is merely a lower bound on waiting; the
  binding constraint is committer attention.
- **Fleet maintenance is a queue of PRs aging through windows.** The latency
  floor is set by policy, and no verification speed removes it. A sweep does
  not produce N merges; it produces N windows aging in parallel, most then
  waiting further for a committer to notice.
- **PR age belongs in `status`.** "41 hours into 72" — and past expiry,
  "window expired six days, no committer action" — sits alongside CI state as
  exactly what a maintainer checking in wants to know. A PR aged well past
  its window is a finding. Its remedy is a polite follow-up through the
  guide's own channels (macports-dev, or a PR comment), and pinging spends
  attention: ring 3, so dockhand may propose a ping but never sends one
  unattended, and never twice.
- **`promote` reads `maintainers` as a policy selector.** nomaintainer:
  proceed. openmaintainer: PR, 72-hour clock. maintained: notify and wait,
  and on timeout write the documentation line the policy requires — a
  provenance line humans forget and a tool never will. What remains
  judgement is only the edge case: what counts as a "minor" update on an
  openmaintainer port, where dockhand should classify conservatively,
  because tier alone does not decide it.
- **Autonomy has a time dimension.** Auto-PR was never auto-merge, and for
  this persona even wholly-owned ports end in a committer's merge. Every
  change dockhand produces terminates in someone else's attention, so the
  core team's review capacity is the global bottleneck — and
  convention-completeness is the throughput lever, not a courtesy. Each
  review round-trip costs days at PR latency. The fastest PR is the one a
  committer can merge without typing anything.

---

## Read-only commands

```
dockhand doctor
dockhand classify <sel> [--for <intent>] [--to <version>|latest]
```

`doctor` prints the capability report D11 describes — *T0–T2 available; T3
unavailable, no `cargo2port`; pristine verification unavailable, no `tart`* —
and the same probe runs implicitly at plan time, so a batch refuses before it
starts rather than forty minutes in. The probe list has grown with the
design: the tools (`port-tclsh`, `git` and its version floor, the block
generators, `tart`, `gh`), the fork (exists; Actions enabled, readable from
the runs API), the verdict-prefix template (present; base version;
staleness), and the pinned `(base, mpbb)` pair.

### `classify` is two questions with different costs

- **Static tractability** — *can dockhand edit this Portfile's version at
  all?* One evaluation per port, no network, genuinely sweepable across all
  20,033 Portfiles. This is what validates the recognizer set and produces
  the empirical tier distribution that everything else currently guesses at.
  It needs no `--to`.
- **Bump feasibility** — *will this particular bump work?* This needs the
  target version, a livecheck, and a read of the upstream tree at the target
  tag. It is per-port expensive and not sweepable in the same sense.

The distinction is not academic: `skopeo` was statically tractable and
infeasible at once. Its 1.22→1.24 bump was one literal on one line, and it
was wrong, because the Go module path had been renamed upstream — something no
amount of Portfile reading could reveal. Feasibility checks are reads of the
upstream tree: module path at the current tag versus the target tag, the
location of `package main`, the presence of the build driver the Portfile
invokes, `patch --dry-run` for each patchfile, whether ldflag targets still
exist, whether `vendor/` appeared or vanished. Each is cheap and needs no
evaluation. Every one of them predicted real breakage across the fifty-port
sample — though the sample was overwhelmingly Go, and whether the general
form (*diff the Portfile's structural assumptions against the new upstream
tree*) works beyond Go idiom is unproven.

Giving one command both jobs, distinguished only by flags, may conflate them;
they may want separate verbs.

---

## Exit codes

An exit status answers *whose problem is this*: the invocation, the plan
dockhand declined to make, the destination that would not take it, the
machine, the tree, upstream, the verification, or an operation that got
halfway. Refusal is a feature, so a decline must be distinguishable from
every one of those — and so must a queue, which is nobody's problem yet.

**The bands are decades, and that is the point of the numbering.** A caller
that wants the *shape* of the answer rather than the answer reads `$?/10`,
and keeps working when a code it has never heard of is added beside the ones
it knows:

```sh
dockhand bump jq; status=$?
case $status in
  0) ;;                       # done
  1) ;;                       # it went wrong, and nothing says whose fault
  2) ;;                       # the invocation is wrong
  *) case $((status / 10)) in
       1) ;;                  # declined
       2) ;;                  # refused
       3) ;;                  # environment
       4) ;;                  # tree
       5) ;;                  # upstream
       6) ;;                  # pending
       7) ;;                  # verdict
       8) ;;                  # partial
     esac ;;
esac
```

`0`, `1` and `2` predate the bands and keep the shell's own meanings: a
script written before dockhand had families still reads them right, which is
why they were not renumbered into a decade of their own. `3` through `9` are
unassigned and have no family: `3`–`6` were the old environment, tree,
declined and verify codes, and a code dockhand does not write should be
learned as unrecognized rather than guessed at from the nearest band.

`2` follows the near-universal usage-error convention (POSIX utilities,
bash, grep), which is why *declined* — an earlier draft had it at `2`, then
at `5` — kept moving away from it, and now has a decade of its own. The
distinction the codes have always preserved is remedy, and the decades are
that distinction stated at a scale that stops running out of room.

### The families

| Band | Family | Whose problem it is | What the remedy is about |
|---|---|---|---|
| `0` | success | — | proceed |
| `1` | failure | unattributed — the band of last resort, and every band below exists to take a case out of it | report it |
| `2` | usage | the invocation's | `--help`, never the machine or the tree |
| `10`–`13` | declined | the plan's: dockhand understood the request, could have carried it out, and judged it should not | nothing broke and nothing was written; the next move is the user's |
| `20`–`24` | refused | the destination's: the change is fine, the place it would go will not take it | the branch or the pull request, never the edit |
| `30`–`36` | environment | the machine's | installing or provisioning something |
| `40`–`44` | tree | where dockhand was pointed | a different path, branch or flag — never an install |
| `50`–`53` | upstream | somebody else's | waiting, or the port's livecheck |
| `60`–`62` | pending | nobody's yet: nothing failed and nothing finished | asking again later |
| `70`–`73` | verdict | the verification answered, and not with a pass | the log, or the port |
| `80`–`83` | partial | the operation did half its work, and that half stands | knowing what stands before re-running |

The families are the contract a script should branch on. The fine codes
below are the contract a script may branch on when the family is too coarse,
and they exist because the remedies inside one family are not
interchangeable.

### Declined — `10`–`13`

| Code | Name | What happened |
|---|---|---|
| `10` | `PlanDeclined` | a planner refused to produce a plan it cannot stand behind, or a field could not be located to edit |
| `11` | `BranchInFlight` | the port already has a change in flight; discard it, pick it up, or `--replace` |
| `12` | `AlreadyCurrent` | nothing to do — and riders went undone with it |
| `13` | `Ambiguous` | the target names several in-flight branches, or the branch changes several evaluation contexts; say which |

Every decline carries its remedy in the sentence, which is what keeps a
decline from reading as a failure. `12` is its own code so a sweep can tell
"nothing to do" from "nothing to do, and here is what that cost" — the
riders a change would have carried, named by rule, held back because there
was no change to carry them. Those names are what the structural proof
offered: a decline has no prediction to compare against, so the semantic
half has not been paid for, and a rule that would fail it would be named
here and then refused by the `--riders` run this code invites. Nothing can
reach that with one rule that inserts a comment at offset zero; a second
rule is where it becomes worth closing. `--riders` plans them as the
change instead, and `--no-riders` withholds nothing because nothing was
ever going to ride, which puts the decline back at `10`.

### Refused — `20`–`24`

| Code | Name | What happened |
|---|---|---|
| `20` | `DuplicatePR` | an open upstream PR already proposes this change; join it, `--title`, or `--no-pr-check` |
| `21` | `PRMerged` | the branch's own PR already merged — a dead end, not a conflict; `dockhand clean` retires it |
| `22` | `Superseded` | work a newer sibling has already replaced: a followed run whose branch moved out from under it |
| `23` | `Held` | *reserved:* a branch deliberately held back from publication |
| `24` | `MachineGate` | an unattended publication of a change still carrying an unanswered finding; a human asking for the same thing is told what they are publishing past and allowed it |

`23` is a held *branch*. A held lock file is an ordinary failure and stays
in band `1`; the names are one word apart and the bands are not.

`24` is the only code whose meaning depends on **who asked**, and that is
what it was reserved for. A finding proposes and never executes, so a change
carrying an unanswered proposal is carrying a question; an unattended road has
nobody to have read it and is refused, while a person promoting is looking at
the proposal on their own `status` output and publishing anyway is their
answer. There is no unattended publisher yet, so nothing reaches `24` from a
verb today — what a person meets is the advisory naming the open finding.

`22` is the destination refusing in the sense that matters: the answer the
superseded run was about to give is about bytes that are no longer the tip.
Nothing failed and the port is fine, which is why it is not in the verdict
band with the runs that ended without one.

### Environment — `30`–`36`

| Code | Name | What happened |
|---|---|---|
| `30` | `NoMacPorts` | no MacPorts installation to read |
| `31` | `EvalStartup` | the Tcl evaluator would not come up |
| `32` | `RootRefused` | dockhand declining to run as root |
| `33` | `ToolMissing` | a tool the work needs is not on this machine |
| `34` | `NoVerifyEnv` | a synchronous ask with no environment to answer it |
| `35` | `ProvisionFailed` | provisioning ran and did not finish |
| `36` | `VerifierBusy` | a synchronous ask refused for want of a slot |

`33` is reached only from the verbs that were *asked* to verify — `verify`,
`log`, `shell`, `exec`. The implicit submit inside a write intent meets the
same missing provider, says on stderr that the branch is unverified, and
exits `0`: the contract narrowing rather than failing.

`34` and `36` are the synchronous halves of a pair. Met by a submit that
defers instead, the same two facts are `61` and `60` — the difference is
whether anyone is still standing there, and whether a run was recorded for
`status` to start.

Four asks wait for their answer and then leave, so four stamp `36`: the
`--verify` gate, `verify <portdir>`, `exec`, and `provision`. The provider
counts slots and cannot know who asked, which is why the caller says so.
`exec` returns the refusal rather than counting it as a release whose
command failed — the command never ran — and ends there, because the cap is
machine-wide and the next release would meet the same wall.

### Tree — `40`–`44`

| Code | Name | What happened |
|---|---|---|
| `40` | `NotPortsTree` | not a MacPorts ports tree |
| `41` | `PortNotFound` | the tree does not carry that port |
| `42` | `NotARepo` | the branch workflow needs a git checkout; `--in-place` edits the tree directly |
| `43` | `Drift` | the Portfile is no longer the one that was planned against |
| `44` | `BranchNotFound` | the target names no in-flight branch; `dockhand status` lists what is |

### Upstream — `50`–`53`

| Code | Name | What happened |
|---|---|---|
| `50` | `FetchFailed` | no URL would serve the distfile |
| `51` | `WitnessUnreachable` | a witness could not run at all: a livecheck whose site is down, an `ls-remote` the forge refused, a git that is not there |
| `52` | `WitnessAPI` | *reserved:* a forge or registry API that answered an error or a rate limit |
| `53` | `LatestUnresolved` | the witnesses ran and left no trustworthy newest version between them; name it with `--to`, or fix the port's livecheck |

`50` is a sentinel the ruled table did not name and the bands claim anyway:
a distfile no URL would serve was an unattributed failure, band `1`, which
is the answer this whole band exists to take cases out of. It is a
renumbering like the rest, said here so it is not read as one that happened
by accident.

A witness that fails because the *machine* failed keeps the machine's band:
an evaluator that will not start, or a refusal to run as root, surfaces
through livecheck and still exits `31` or `32`. Relabelling those "upstream
unreachable" would send a user to look at a website.

`52` is reserved rather than produced. A forge API error or a rate limit
today falls back to the tag witness silently and the bump still lands;
making the code reachable is a change to that fallback, not a renumbering.

`53` covers only the verdicts that left nothing to act on: no signal at all,
and the four shapes of a livecheck the forge does not stand behind (rotted,
behind, ahead with nothing to corroborate it, or standing alone with nothing
corroborating it). A judgment over *sound* witnesses — the newest tags are
all prereleases — is dockhand's own refusal and exits `10` with the other
declines. The verdicts that resolve (agreement, one witness only, a tag
without a release, a prerelease that is lateral or superseded) set a version
and exit `0`; if one of them ever stops setting a version, it is a judgment
over sound witnesses too, and it stays at `10` rather than sliding into this
band by default.

### Pending — `60`–`62`

| Code | Name | What happened |
|---|---|---|
| `60` | `VerifyQueued` | a run deferred for want of a slot, or a followed run the settle found still queued; `dockhand status` starts it when one frees |
| `61` | `VerifyAwaitingSlot` | a run queued for an environment this machine has not provisioned yet |
| `62` | `PromotionPending` | *reserved:* a published destination still awaiting its verdict |

Nothing here failed. These must never share a band with a refusal, because
the remedy is to ask again rather than to fix anything.

### Verdict — `70`–`73`

| Code | Name | What happened |
|---|---|---|
| `70` | `VerifyFailed` | the run completed and the port does not build — and `promote` refusing over one, which is that same answer being enforced |
| `71` | `VerifyBlocked` | the run never reached the change: a dependency failed first, so the port is untested rather than disproven |
| `72` | `VerifyUnsupported` | the provider cannot run what was asked for |
| `73` | `VerifyErrored` | the verification ended without a verdict: the environment could not answer, or a person stopped the run |

`73` is a fact about the machine and exits here anyway, because what
happened is that the verification ended without a verdict — which is what a
caller waiting on one needs to hear. All three of `71`, `72` and `73` used
to come back as "no environment available", which sent a user whose
neighbour was broken off to provision a machine that was fine.

A cancel shares `73` and not its sentence. None of the ruled numbers names
a person stopping their own build, so it lands in the band that says the
verification ended without a verdict — but "could not answer: canceled" is
a sentence that contradicts itself, and the twin's `reason` is what tells
the two apart: `verification-errored` against `verification-canceled`. The
other two ways a followed run can end without a verdict leave this band
entirely: a superseded run is `22`, and one still waiting for a slot is
`60`.

`72` is not a port declining a platform. That is the record's *unsupported*
state, it is frequently the change working exactly as intended, and the
verbs say so and exit `0`.

The `verify.ErrUnsupported` sentinel moves here from the old `3`, the
retired environment code, for the reason the row gives: nothing is missing
that provisioning would supply. Like `50`, it is a renumbering the ruled
table did not enumerate and the bands require.

### Partial — `80`–`83`

| Code | Name | What happened |
|---|---|---|
| `80` | `MintedSubmitErrored` | the branch is minted; the verification submit broke |
| `81` | `PushedPRFailed` | the branch is pushed; the pull request would not open |
| `82` | `PRRefreshFailed` | the branch is pushed; its pull request still describes the change it used to carry |
| `83` | `SweepHardErrors` | a sweep finished with rows that were not declines |

Re-running is not free and not always safe, so these can never be folded
into `1`: a script must be able to tell "nothing happened" from "the branch
is pushed and the PR is not".

### Where the mapping lives

A typed error owns its band **where it is defined**, by implementing
`DockhandExit() int` — so the band cannot be forgotten in a table two
packages away, which is the trap every new error type used to walk into.
`internal/cmd/exit.go` holds only the other half: the sentinels, which cannot
carry a method, and which name a dozen packages `internal/exitcode` would
have to import to see them. Typed errors are consulted first, so a sentinel
wrapped by an error that knows better keeps the better band.
`internal/exitcode` holds the constants and `Family`, which is the only thing
that may name a decade. Nothing anywhere is mapped by message text.

The method is named `DockhandExit` and not the obvious `ExitCode` because
the obvious name is not dockhand's to claim: `*exec.ExitError` answers
`ExitCode()`, so an interface asking for it is satisfied by every child
process dockhand runs. Typed errors are consulted first, so a chain that
wrapped a raw `git` or `tart` failure would hand the child's status straight
to `$?` — past the sentinel that knew better, and into a band the child has
never heard of. A guest exiting `66` made `case $?/10` conclude "nobody's
problem yet, ask again later" about a verification environment that had
failed. The odd name is the fix: nothing outside this repository writes it.

### The status, said inside the document

Every JSON document dockhand emits carries its own exit status, as the last
key, so a consumer that captured stdout through a pipe and lost `$?` still
knows how the run ended:

```json
{
  "repository": "/opt/mports/macports-ports",
  "branches": [],
  "exit": { "code": 0, "family": "success", "reason": "" }
}
```

`reason` is a stable machine token — `already-current`, `duplicate-pr`,
`verification-blocked` — for a caller that needs *which problem* rather than
*which kind*. It is not the prose with the spaces taken out: the message may
be reworded and these bytes may not.

**A reason names exactly one code.** Several reasons may share a code — two
producers of the same outcome name themselves the same way — but a reason
that spanned two codes would be the coarser of the two fields, which is
backwards, and a consumer filtering on it would have to read the code anyway
to learn what it had filtered. So the wrapper that bands an unresolved
verdict as upstream's says `witness-unresolved` where the decline it carries
says `latest-unresolved` (`53` against `10` — the reason is the only thing
that could have told those two apart), and a decline that withheld riders
says `already-current-withheld` where one that withheld nothing says
`already-current` (`12` against `10`).

Sentinel-classified outcomes carry a reason too — `no-macports`,
`not-a-repo`, `drift` — even though the sentinels themselves cannot carry a
method. Without that, a third of the contract's codes could publish no
reason at all, which is harmless while the only documents are the plan, the
status report and the decline, and wrong the first time a verb emits one on
a machine that has no MacPorts.

`--plan` with nothing to plan emits the decline itself, on the stream the
plan would have used, with the two things a decline knows that a bare status
does not:

```json
{
  "exit": {
    "code": 10,
    "family": "declined",
    "reason": "already-current",
    "detail": "1.8.2",
    "remedy": "nothing needs doing here; ask for a different state if this is not the one you meant"
  }
}
```

Before this, a declined `--plan` wrote nothing at all to stdout and left its
reason in an English sentence on stderr, so every consumer had two parsers
or one blind spot. `--diff` gets no such document: its stdout is a patch
somebody pipes into `git apply`, and one flag with two output languages
breaks the consumer that trusts it.

`status --json` publishes the twin on its failure paths for the same
reason. A pass that never reached a report has nothing to report, so the
document is the twin and nothing else:

```json
{
  "exit": { "code": 42, "family": "tree", "reason": "not-a-repo" }
}
```

The twin is built from the same error the process exits on, never derived a
second time. A document that could disagree with `$?` is worse than no
document.

### Two asymmetries the codes carry

**Contract against progress.** A bump whose branch minted but whose
verification could not start leaves the branch standing — the git
commit/push shape, where a failed push never deletes the commit — and the
exit says which of four things happened, because they do not share a remedy:
every slot busy, `60`, and `dockhand status` starts it when one frees; the
release not provisioned, `61`, and it starts when someone provisions it; the
provider unable to run the request at all, `72`, which nothing will free;
the submit broken after the mint, `80`. The message names the follow-up
(`dockhand verify <branch>`), and `--no-verify` narrows the contract to
minting alone, restoring exit `0`.

**Point against sweep.** A point intent that declines exits in the declined
band — `10` for an ordinary refusal: the user asked for one thing and did
not get it. A sweep that declines on 40 of 340 ports exits `0`; that is a
success with a tail, and the declines are output, not failure. If both
exited alike, every CI wrapper around a sweep would be wrong.

`83` is produced rather than reserved. Five roads reach it: `bump`,
`refresh-checksums` and `bump-revision` under a selector, and `outdated`,
and the sweep grammar's own abandonment. All five agree on the rule — exit
`0` when every port was either handled or declined, `83` when some rows were
neither — and all five decide it the same way, by band rather than by
enumerating codes: `hardBand` in `internal/cmd/intentsweep.go` for the write
verbs, `Outcome.Hard` in `internal/upstream/staged.go` for the report. What
is deliberately *not* in it: a port that is outdated, a port that declined,
and a host that refused dockhand and was left alone. The first is the
report's subject, the second is the commonest outcome of a real sweep, and
the third is somebody else's problem — a walled sweep exits `0` and the
census tail says how many ports were not examined and that running again
finishes them.

---

## What is deliberately absent

There is no `dockhand init`, no `dockhand db`, no `dockhand sync`. D8 holds
that everything persisted is re-derivable from the ports tree plus GitHub, so
there is no store to initialize, migrate, or reconcile. The moment one of
those commands looks necessary, D8 has been violated. The empty space in the
verb list is the invariant made visible.

---

## Open questions

1. **Is `artifact` a depth rung or a fourth proposition?** Field evidence
   produced a case D4's three propositions do not span: a faithful edit, a
   green build, declared dependencies, and a wrong result — four ports whose
   binaries still reported the old or no version. The check that caught them
   (run the binary, read its self-reported version) costs less than the build
   it follows, so it does not belong on a cost-ordered depth ladder; and it
   is only *sometimes available*, which none of the existing propositions
   are. This bears on D4, which is a committed decision.

2. **What counts as a "minor" update on an openmaintainer port?** The
   72-hour policy machinery is otherwise mechanical, but this edge is
   judgement, and tier alone does not decide it. dockhand should classify
   conservatively until the boundary is understood.
