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
With eleven intents they collide. The resolution:

> The intent takes the verb slot. Its output is always a plan, and the stages
> are separate verbs that consume the plan.

```
dockhand bump gcc14 --to 1.4.2           # emits a plan; changes nothing
dockhand bump maintainer:me --to latest  # a sweep of bumps; still only plans
dockhand apply <plan>                    # write the edit into the worktree
dockhand verify <plan> --depth=build     # run verification
dockhand promote <plan>                  # branch, commits, PR
```

Emitting a plan is both the convenient default and the safe one, so no flag is
needed to make dry-run the default behavior. The shape also matches `port`
itself (`port upgrade`, `port uninstall`), the same familiarity argument that
justified borrowing the selectors.

For interactive single-port work, three commands is tedious, so `apply` reads
a plan from stdin:

```
dockhand bump gcc14 --to 1.4.2 | dockhand apply -
```

The pipe is available when you want it. Sweeps never want it.

## Intent verbs

```
bump <sel> [--to <version>|latest]
bump-revision <sel>                # alias: revbump
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

- **`bump-revision` and `revbump` are the same verb.** The canonical name
  follows the family's shape (`bump`, `bump-revision`, `bump-epoch`); the
  alias honors the community's own vernacular — reviewers and commit
  messages say "revbump" — by the same borrowing principle that took
  `port`'s selectors. Both spellings are permanent; neither is deprecated.
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

### `--to latest` is a query, not a value

`bump maintainer:me --to latest` is the flagship invocation, and the update
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
  body (per D9), not in the ref name.
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
dockhand dismiss <finding-id> [--reason ...]
dockhand debug <finding-id>              # shell into the preserved environment
dockhand bump --from-finding <finding-id>
```

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
  `Closes: https://trac.macports.org/ticket/NNNNN`, exactly. `promote` takes
  an optional `--closes <ticket>`, and findings carry ticket URLs when they
  originate from one.

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

An exit status answers *whose problem is this*: the invocation, the machine,
the tree, or the operation. And refusal is a feature, so a decline must be
distinguishable from all of them:

- `0` — success (a plan produced; a sweep completed, even with declines)
- `1` — the operation failed
- `2` — usage: bad flag, unknown command, invalid arguments
- `3` — environment: MacPorts missing, tclsh broken, running as root
- `4` — ports tree: not a tree, port not found
- `5` — declined: a point intent refused to produce a plan

`2` follows the near-universal usage-error convention (POSIX utilities,
bash, grep), which is why *declined* — an earlier draft had it at `2` — moved
to `5`, out of the error band entirely. The distinction the codes preserve is
remedy: `2` says reread `--help`, `3` says fix the machine, `4` says fix the
tree or the name, `1` says the operation itself went wrong.

The table and its lookup live in `internal/cmd` (exit.go), mapped from typed
errors by identity — never by message text.

The point/sweep asymmetry lands here directly. A point intent that declines
exits `5`: the user asked for one thing and did not get it. A sweep that
declines on 40 of 340 ports exits `0`: that is a success with a tail, and the
declines are output, not failure. If both exited alike, every CI wrapper
around a sweep would be wrong.

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
