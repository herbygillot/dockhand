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
commit, whoever made it; `status` observes the `dockhand/*` namespace and
any other branch carrying a verify note, and `cycle` acts on what it found;
`promote` pushes the branch it finds, and first searches upstream's
open PRs by the `<port>:` title convention: an identical title is
refused as a duplicate (exit 20, `--no-pr-check` overrides), a
same-port PR is surfaced as a note, and a clean search checks the
template's other-open-PRs box. A branch whose own PR is already open
is re-promotion: the push updates that PR instead of opening a second.
`promote --body` prints the body that push would carry and publishes
nothing — no push, no pull request, no audit row, and GitHub is not
asked, so the other-open-PRs box reads unchecked and a note on stderr
says why. The body is measured against GitHub's 65536-character bound
before anything is pushed: one over it is declined (exit 10,
`pr-body-too-long`) naming the size and the limit, never trimmed,
because the member lines are the evidence a reviewer is asked to
accept. The deliberate opt-out is `--in-place`:
edit the Portfile where the user stands, uncommitted, minting nothing — for
the user folding dockhand's mechanical edit into their own workflow, and the
only write mode a non-git tree gets (with a loud warning). Lifecycle: an
intent finding an in-flight branch for its port refuses and names it;
`--replace` replaces it — verification canceled, notes removed — but
only a branch that is exactly the minted commit: work the user added
past the mint refuses, and `discard` stays the explicit act for
dropping it. Re-deriving a port at the version it already carries is a
separate question and now a separate VERB, `refresh-checksums` (`refresh`)
— the flag spelling `bump --recheck` was deleted outright for duplicating
it — which also makes the verification build from source: the archive
matching an unmoved version predates the change. `promote --force` keeps its name
and is a different act — force-push the fork copy (with lease) and
refresh the open PR's title and body — because it moves a branch
dockhand published rather than destroying one it minted. `status` reports each
promoted branch's PR state and deletes nothing (D27): a branch whose PR
merged is reported as merged, and its line names `dockhand cycle`, which
retires it — locally, and its fork copy with it. PR state from GitHub
decides merged (the project's merge styles rewrite shas as commits land, so
ancestry proves nothing); closed-unmerged branches are kept and flagged. The
pipe below, and every plan-file argument in this document, describe the
superseded surface.

**Auto mode is declared, never inferred (2026-09-02).** The invoker is a
**person** for every verb unless the invocation says otherwise.

**Amended (2026-09-07): THE VERB IS THE DECLARATION, and it is the only
one.** `--auto` and `DOCKHAND_AUTO` are both gone — the binary rejects the
flag with exit `2` and reads the variable nowhere — and what replaced them
is a verb that can only be the machine:

```
dockhand dispatch              # the resident scheduler: the machine, by construction
dockhand dispatch --once       # one unattended pass, for a cron entry
dockhand cycle                 # one pass as a PERSON; publishes nothing
```

A flag beside `dispatch` would be a second way to say one thing, so
`dockhand dispatch --auto` is a usage error. The reasoning below survives
the change unaltered — nothing infers the invoker from a terminal — and is
now enforced by there being no ambient value to read at all: `record.Driver`
is a constant of each road, set where the operation is built.

Nothing asks whether a terminal is attached. `tool.IsTerminal` exists and is
one import away, and reaching for it would make the answer depend on how the
process was started rather than on what the operator said — a pipe, a CI
runner or a `script` wrapper would each silently move a person's authority
onto a machine, or the reverse, with nothing in the invocation to read. The
command line is the nearer declaration — and with the verb carrying it,
there is no standing declaration to withdraw and no value to misparse.

`AI_AGENT` names which agent was driving, if any. It is recorded beside the
declaration and read by no gate, so setting it can neither grant nor
withhold anything.

What the declaration is FOR is **provenance**: the mint writes it onto the
record as `asked_by`, so a later question about how a change reached review
is a query rather than an estimate. It is never an input to a gate. The one
gate that turns on an invoker takes one as a parameter at its own call site;
reading a driver back off a record to decide what the unattended road may do
would let a change authorize itself by claiming its own history.

**`dockhand status`** observes and settles (D27). It reads every branch
dockhand has something to say about — the `dockhand/*` namespace and any
other local branch whose tip carries a verify note — polls their workers,
writes what they said into the ledger, releases the guest of a run whose
verdict says so, and renders. It makes no change anybody else can see: no
branch is deleted here or on the fork, no queued run is started, nothing is
published. Where work is waiting the report says so and names `dockhand
cycle` beside the finding — a queued run reads "`dockhand cycle` starts
it", a merged pull request "`dockhand cycle` retires the branch" — because
with the split nothing begins on its own. **`--no-update`** is the pure
read: the ledger as written, polling nothing, writing nothing, taking no
locks and asking no forge or provider, so the pull request standings and
the worker audit are not shown and the report says so on its first line.
`--no-clean` is gone: it withheld a deletion `status` no longer performs.

**`dockhand cycle`** does what `status` reports. It runs the same pass and
then retires the branch of a merged pull request, locally and on the fork
(**`--keep-merged`** withholds it, and each kept branch's line says why it
stands); removes the branches a newer sibling replaced when asked
(`--superseded`, as `clean` had it); reclaims the untracked workers this
checkout may claim when asked (**`--reclaim-unattributed`**, renamed from
`--reclaim-orphans` — an environment another checkout started is named and
left to that checkout's own pass); and starts what was deferred. Only a branch dockhand minted is ever deleted: a
hand-made branch carrying a verify note is shown, settled and left alone,
whatever its pull request did. `clean` is retired; `cycle` is `clean` plus
the rest.

**`dockhand dispatch`** is the unattended reconciler — the launchd
entrypoint, and what `cycle --auto` and the `auto` verb before it used to
be. It is the same pass run as the machine, on a timer (`--every 5m`), and
being that verb is what hands it the one thing a person's `cycle` must
never carry: a **publish slot**, paced at `--publish-max 20` per
`--publish-every 6h` and withheld entirely by `--no-publish`. **`dockhand
dispatch --once`** performs one pass and exits, which is the shape a cron
entry wants. Publication through the slot is refused on this build; see
`24` below. A person's `cycle` publishes nothing.

**`--keep-env`** on `verify` and on the `bump` family (`bump`,
`bump-revision` including `--for`, `refresh-checksums`) keeps a passing
run's environment the way a failure's is kept by rule (D27): recorded on
the run when it is submitted, carried through a deferral, and honoured
when the run settles — `status` then says "environment kept" beside the
pass, and `dockhand shell` reaches it until `cancel` or `discard` gives it
back. It rides a submitted run, so the five deliveries that produce none —
`--no-verify`, `--plan`, `--diff`, `--in-place` and `--riders` — refuse it
rather than drop it, beside `--test` and `--timeout`, which ride one for the
same reason. Not a flag on `status` or `cycle`: by the time either settles,
the release is in the same pass. (The `--verify` gate this paragraph used to
name is gone with always-enqueue: nothing waits for a verdict and releases
in the same breath any more.)

**`--no-fetch`** on the `bump` family declines the fetch that keeps a
change current. By default `bump`, `bump-revision` and
`refresh-checksums` fetch upstream's primary branch and cut the change
from the resulting remote-tracking ref rather than from the local
branch, so the pull request is based on the newest tip the project has
(D29). It updates that one tracking ref and nothing else: no local
branch moves, no working tree is read, no index is touched. When
upstream is ahead, one line on stderr says by how much and that the
change carries those commits.

**Which remote is upstream is asked, not assumed** (D30). `origin` is a
convention — `git clone <your fork>` makes it the fork — so dockhand
asks the forge, which is the only party that knows which repository is
the project and which is a copy of it. Set `git config dockhand.upstream
<remote>` to answer it yourself; that wins outright and asks nothing,
which is what makes it the answer for a mirror, a private tree, or a
host that is not GitHub. The same lookup decides where `promote` sends
the pull request.

A lookup or fetch that cannot run — offline, a proxy, no `gh`, every
remote a fork — is not a planning error: the base falls back to the
local primary, and a line says what could not be established and that
the base may be behind. `--no-fetch` asks for that base on purpose and
says nothing, because nothing was concealed.

The cost is that drift has two causes now. The plan is made from the
working tree and held against the base's bytes, so `ErrDrift` (exit 43)
used to mean "you edited this Portfile on your primary branch". It can
now also mean "the port moved upstream since you last pulled", which a
clean `git status` will not explain — so the message names both
remedies rather than guessing which is yours.

**`dockhand hold <branch> [--reason ...]`** stops a change: nothing will
publish, verify or retire it until `dockhand unhold <branch>` releases it.
Holding an already-held branch is refused rather than silently overwriting
the reason somebody wrote. A change minted against a prerelease-style target
is **born held**, announced at the mint — dockhand will plan an rc a
maintainer asks for by name, but will not carry one onward on its own; the
verification that same invocation asked for still runs.

**`dockhand cancel <branch>`** stops a running verification and gives its
environment back, recording on the note why the run stopped. It is a
person's verb: a machine never cancels, and the one cancellation an
unattended pass may cause is a supersede, which happens at another branch's
mint and is about the commit having been replaced rather than about
anybody's patience.

**`dockhand cycle --superseded`** is the intentional removal of branches a
newer sibling replaced. It is the only thing in the tool that removes a
branch for having been superseded: the ordinary pass, the report, the drain
and the machine's publish slot all leave one exactly where it is.

**`--to-pr`** asks a write intent to carry the change through to a pull
request **in that one invocation**, on every host. It used to mean two
different things chosen by a property of the machine — a person who
installed tart found their `--to-pr` silently stop opening pull requests and
start queueing them for a `dispatch` they were probably not running — and a
flag whose semantics depend on host state is a seam in the wrong place. What
differs by host now is only what has to happen in between.

On a machine that cannot verify there will never be a pass, so the evidence
is the person who typed it: the ring-3 prechecks are asked first and
**before anything is minted** (an own PR already merged is `21`, a duplicate
title is `20`), and then the change is minted and published. On a machine
that can verify, the invocation **stays for the build** — the pass is what
authorizes the publication, and `waitFor` reads `--to-pr` as an unbounded
wait — and publishes on the verdict. A failure, a reap or any other
non-pass publishes nothing and exits in its own band.

`--no-verify` composes with it rather than contradicting it: the two are
different axes — `--to-pr` says where the change is bound and `--no-verify`
how much evidence goes with it — so `--no-verify --to-pr` is the road that
returns a pull request at once, with a body that says it was not
pre-verified. They were refused together on the reasoning that both wrote
`Destination`; only one of them ever should have.

A selector naming more than one port is still refused, and so is an
unattended run on the publishing road — it has no authority to lend.

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
bump <sel> [--to <version> | --latest]             # no flag: latest
bump-revision <sel>                # alias: revbump
bump-revision --for <branch>       # the plural invocation: accept a revbump proposal
bump-epoch <sel>                   # NOT SHIPPED
refresh-checksums <sel>            # alias: refresh; carries its cause
vendor <sel>                       # NOT SHIPPED — regenerate vendored block (T3)
deps <sel> --add/--remove <spec>   # NOT SHIPPED
patches <sel> [--drop-obsolete]    # NOT SHIPPED
modify <sel> --set <field>=<value> # NOT SHIPPED
obsolete <sel> --replaced-by <port> # NOT SHIPPED
migrate <sel> --idiom <name>       # NOT SHIPPED
```

**Amended.** Three intents ship — `bump`, `bump-revision` (`revbump`) and
`refresh-checksums` (`refresh`) — and they are one catalogue rather than
three commands, so every shared flag below is registered once and means the
same thing on all three. The rest of the list is the design's shape for
what a fourth would look like, and is marked so nobody types one.

Notes on naming and shape:

- **`bump-revision --for <branch>` is the same verb asked a different
  question.** The edit is bump-revision's edit; what changes is who chose the
  ports and who wrote the reason. On the single-port road a person names both,
  and with `--for` the proposal a verification measured holds both — which is
  why it needs no `--reason` and takes no port. It never mints: the members
  land as one more commit on the branch that already carries the change,
  because they move for one reason and it is the same reason. A branch
  with no proposal to accept — no verification record on its tip, or a
  proposal already answered — exits `10` (`no-proposal`); the message
  names the verb that measures or shows it.

- **`--exclude` and `--force-withheld` shape which members the cohort
  carries, and both belong to `--for` alone.** `--exclude=<member,...>`
  takes members out of the change entirely — not bumped, not built, and
  listed among the ports examined and not bumped so a reviewer can
  disagree; a name it does not put forward exits `10` (`unknown-member`),
  and excluding everything exits `10` (`empty-cohort`, where `dismiss` is
  the verb for turning a proposal down outright). `--force-withheld=<member,...>`
  is the person overriding D24: a withheld member is one MacPorts will
  not activate beside a sibling the cohort seats, so it is bumped and not
  built — this seats it anyway, **last**, after every member that might
  need the sibling active, with `port -f deactivate <sibling>` run in the
  guest immediately before its own lint, test and install. The sibling's
  own verdict stands as already measured; the forced member's outcome is
  judged like any built member's; and a reviewer is told, wherever the
  member is reported, that the environment it was proven in is not the
  cohort's — the report line and the pull request body say it was *built
  with `<sibling>` deactivated, at the maintainer's request* (or, short
  of a pass, that it was *to be built* so), and the cohort commit's
  candidate line says it was *forced into the build at the maintainer's
  request, with `<sibling>` deactivated first*. The override is recorded
  on the note as well as on the run, so a cohort accepted with
  `--no-verify` and verified by hand later is built the same way. A
  member the proposal knows but does not withhold exits `10`
  (`not-withheld` — there is nothing to force); a name the proposal does
  not put forward at all exits `10` (`unknown-withheld`, listing the
  members it does withhold); a withheld member with nothing to
  deactivate — its sibling taken out of the change by `--exclude`, or a
  record that does not say which member it conflicts with — exits `10`
  (`cannot-force`); two forced members that conflict with each other
  exit `10` (`forced-conflict` — nothing can seat both); and a member
  named by both `--exclude` and `--force-withheld` is a usage error.
  Neither flag is the default: the tool informs and stops, and these are
  the person answering.

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
  attention: ring 3, so dockhand may propose a ping and never sends one.
  **Amended (2026-09-02, S14).** "Never twice" reads as a promise about the
  drafting and it is not one: `status` drafts the follow-up on every pass,
  with no memory that it drafted one before. Never-twice is a property of
  the *sending*, which dockhand does not do — that is a fact about the tree
  rather than a rule anything enforces: `internal/gh` has no comment method,
  no ping, no review and no merge, and the only pull-request writes anywhere
  are `pr create` and `pr edit` on the publish road. A record that a draft
  had been shown would be state kept to suppress a line, and the line is how
  the reader knows the pull request is still waiting.
- **`status` lists in attention order, not in the namespace's (2026-09-02,
  S14).** `for-each-ref` order is alphabetical order of a slug nobody chose
  for reading, so the one branch that failed sat wherever its port name put
  it. A fleet's report is scanned, not read, and what it is scanned for is
  the handful of changes that want a person. The ruled sequence is:
  failures; open PRs past the 72-hour window, oldest first, with their age;
  queued runs; passed-but-unpromoted; held; the two quiet end states (a PR
  closed without merging, a branch a newer sibling replaced); everything
  else in the order it arrived. Within a band the enumeration order
  survives. The ordering is imposed in the renderings — `status`'s two and
  `cycle`'s — and nowhere lower: a sort applied to the pass itself would
  reorder what the pass did before it was said, and what `cycle` did to a
  branch travels with the branch. The window is the same 72 hours for every tier — a literal
  reading would give nomaintainer ports, 63.5% of the tree, no window at all
  — and what the tier decides is what the elapsed window *means*, which is
  what the follow-up draft can honestly say.
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
| `40`–`46` | tree | where dockhand was pointed | a different path, branch or flag — never an install |
| `50`–`53` | upstream | somebody else's | waiting, or the port's livecheck |
| `60`–`62` | pending | nobody's yet: nothing failed and nothing finished | asking again later |
| `70`–`74` | verdict | the verification answered, and not with a pass | the log, or the port |
| `80`–`84` | partial | the operation did half its work, and that half stands | knowing what stands before re-running |

The families are the contract a script should branch on. The fine codes
below are the contract a script may branch on when the family is too coarse,
and they exist because the remedies inside one family are not
interchangeable.

### Declined — `10`–`13`

| Code | Name | What happened |
|---|---|---|
| `10` | `PlanDeclined` | a planner refused to produce a plan it cannot stand behind, a field could not be located to edit, or `promote` composed a pull request body longer than GitHub takes |
| `11` | `BranchInFlight` | the port already has a change in flight; discard it, pick it up, or `--replace` |
| `12` | `AlreadyCurrent` | nothing to do — and riders went undone with it |
| `13` | `Ambiguous` | the target names several in-flight branches, or the branch changes several evaluation contexts; say which |

`13` is reserved rather than produced. `change.Resolve` is ruled to
*resolve* an ambiguous target rather than refuse it — the `Bound()` record
wins where two carry one name, and the newest change wins for a port —
so making this code reachable is a change to that ruling, not a
renumbering.

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
| `21` | `PRMerged` | the branch's own PR already merged — a dead end, not a conflict; `dockhand cycle` retires it |
| `22` | `Superseded` | work a newer sibling has already replaced |
| `23` | `Held` | a branch deliberately held back: `dockhand hold` placed it, a prerelease target was born under it, or a publication-time re-witness found upstream serving other bytes |
| `24` | `MachineGate` | a road refused this invoker where a person asking for the same thing would be allowed it — see the four reasons below |

`23` is a held *branch*. A held lock file is an ordinary failure and stays
in band `1`; the names are one word apart and the bands are not.

`23`'s reasons are `held` (a hold being obeyed, whoever placed it) and
`stealth-suspected` (the publication-time re-witness found the distfiles no
longer hashing to what the change records, so the change is held and a
proposed finding asks a person about it). A hold refuses **both** invokers:
it is the human's own instrument, often placed to stop themselves, and one a
`dockhand promote` walked past would be note-keeping rather than a brake. It
withholds the publication, the verification, the deletion and the superseded
sweep, and it never changes a verdict.

`24` is the only code whose meaning depends on **who asked**, and that is
what it was reserved for. Its four reasons:

| Reason | What happened |
|---|---|
| `open-proposal` | an unattended publication of a change still carrying an unanswered finding; a person promoting is told what they are publishing past and allowed it |
| `no-positive-evidence` | an unattended publication of a tip with no passing verification; a person publishes an unverified change with a complaint, and absence of evidence is not a reason for a machine to spend a reviewer's attention |
| `promote-is-human` | *retired with the declaration.* `dockhand promote` in auto mode — and nothing can declare that any more: a typed promote is a person by construction, `Grants.Invoker` being a constant of the road. The rule it protected still holds, and holds structurally: there is exactly one machine publish path, the dispatcher's slot |
| `machine-publish-disabled` | this build does not let a machine spend ring 3 at all. The permission is a build-time constant and it is false; flipping it is the trust ladder's ruling to make |
| `machine-publish-no-verifier` | *retired with the declaration,* for the same reason: the run that could declare itself unattended is gone. `--to-pr` on a verifier-less host is now a person sequencing `promote` after the mint, in the one invocation that asked |
| `machine-publish-disabled` | *retired with the 2026-09-06 grant.* It named a build-time constant that was false; the machine road is `GrantSimpleBumps` now, and what it refuses it refuses by the grant rather than by a switch. Nothing in the tree has written this reason since |
| `machine-republish` | an unattended publication met a pull request already open for the branch. The slot decides this a phase earlier and calls it work done; reaching the verb with it is a bug above the verb, and the funnel refuses rather than force-updating a review it did not open |

A finding proposes and never executes, so a change carrying an unanswered
proposal is carrying a question; an unattended road has nobody to have read
it and is refused, while a person promoting is looking at the proposal on
their own `status` output and publishing anyway is their answer.

The machine's grant is asked of the **machine** even when a person typed the
verb, wherever what is being bound is the machine's road. That used to
include `bump --to-pr` on a machine that can verify; it no longer does,
because that invocation now walks the road itself and publishes as a person
with a pass in hand. What remains on the machine's road is the dispatcher's
slot, whose candidates are the `--to-pr` changes a person started and did
not stay for.

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
`cycle` to start.

**Amended: after always-enqueue there is one such ask left, not four.** A
change road meeting `verify.ErrNoVacancy` leaves its attempt queued and
exits `60`, so `36` is now only the verbs that need a guest in the
invocation that asked — `exec` above all, with `shell` and `provision`
beside it. The provider counts slots and cannot know who asked, which is why
the caller says so. `exec` returns the refusal rather than counting it as a
release whose command failed — the command never ran — and ends there,
because the cap is machine-wide and the next release would meet the same
wall.

### Tree — `40`–`46`

| Code | Name | What happened |
|---|---|---|
| `40` | `NotPortsTree` | not a MacPorts ports tree |
| `41` | `PortNotFound` | the tree does not carry that port |
| `42` | `NotARepo` | the branch workflow needs a git checkout; `--in-place` edits the tree directly |
| `43` | `Drift` | the Portfile is no longer the one that was planned against |
| `44` | `BranchNotFound` | the target names no in-flight branch; `dockhand status` lists what is |
| `45` | `BranchMoved` | the record's tip and the ref disagree, or a dockhand ref moved between a road's resolve and its commit — a person's own git on a dockhand branch. `dockhand verify <branch>` follows the commit, and `git branch -f <branch> <recorded tip>` puts it back |
| `46` | `BranchCheckedOut` | a batch would move or delete a branch some worktree has it checked out in — an accept, a discard, a replace, a retirement. Measured rather than assumed: `git branch -f` refuses such a branch and the update-ref batch does not. Switch away first |

`44` is `change.ErrNoRecord` and nothing else, and it is deliberately not
`41`: a wrapper reading `41` runs `portindex` for a tree that does not
carry the port, where the remedy here is a different branch name.

### Upstream — `50`–`53`

| Code | Name | What happened |
|---|---|---|
| `50` | `FetchFailed` | no URL would serve the distfile |
| `51` | `WitnessUnreachable` | a witness could not run at all: a livecheck whose site is down, an `ls-remote` the forge refused, a git that is not there |
| `52` | `WitnessAPI` | a forge or registry API that answered an error or a rate limit — and `forge-lookup-failed`, the unattended pass declining to guess at a pull-request question the forge would not answer |
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
| `60` | `VerifyQueued` | a run deferred for want of a slot, or a followed run the settle found still queued; `dockhand cycle` starts it when one frees, and `dockhand status` names it |
| `61` | `VerifyAwaitingSlot` | a run queued for an environment this machine has not provisioned yet |
| `62` | `PromotionPending` | an unattended pass left publication work unfinished: a verification still running (`promotion-pending`), a forge that would not answer (`forge-lookup-failed`, which exits `52`), or the pass's own per-pass cap and pacing (`pass-limit`) |

Nothing here failed. These must never share a band with a refusal, because
the remedy is to ask again rather than to fix anything.

`62` is what `dockhand dispatch --once` exits with when its publish slot has
work left over — a person's `cycle` publishes nothing and cannot reach it,
and a resident `dispatch` never exits —
and it deliberately reports only the **waiting**. A refusal is stated
on the branch it is about and does not become the pass's status: on this
build every candidate is refused with `machine-publish-disabled`, and a cron
entry that exited non-zero every ten minutes because a road it was never
asked to walk is closed would read as a broken machine in every log watching
it.

A forge lookup the unattended pass could not get an answer to exits `52`
(`forge-lookup-failed`) rather than `62`: nothing local is wrong, the
question may answer in an hour, and reading an unanswered lookup as "no pull
request" is what would make a pass open a second one beside somebody's
first.

### Verdict — `70`–`74`

| Code | Name | What happened |
|---|---|---|
| `70` | `VerifyFailed` | the run completed and the port does not build — and `promote` refusing over one, which is that same answer being enforced |
| `71` | `VerifyBlocked` | the run never reached the change: a dependency failed first, so the port is untested rather than disproven |
| `72` | `VerifyUnsupported` | the provider cannot run what was asked for |
| `73` | `VerifyErrored` | the verification ended without a verdict: the environment could not answer, or a person stopped the run |
| `74` | `VerifyFaulted` | the verification ended without a verdict because **dockhand's own tooling** did not produce one, in an environment that was working |

`74` is apart from `73` because the remedy is not the same. `73` says the
machine could not answer, and the answer to that is another guest. `74`
says the machine answered everything it was asked and dockhand's apparatus
inside it did not deliver a verdict — the runner that never started, the
cohort member the guest never announced. Another guest will reproduce it
exactly, so a script that reads a fault as an environment problem retries
forever against a machine that was never the problem.

Both used to be `73`, which told a person to go and fix a machine that was
working, and released the one environment that could have shown them what
dockhand did wrong. A faulted run now KEEPS its environment: it is healthy,
`dockhand shell` reaches it, and it is the only place the defect exists.

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
one still waiting for a slot leaves this band entirely and is `60`.

**Amended.** A followed run that was SUPERSEDED exits `73` with the reason
`superseded`, not `22`: what the caller asked for was a verdict, and the
run ended without reaching one, which is what this band is for and what
somebody waiting on it needs to hear. `22` names the destination refusing
work a newer sibling replaced, and it has no producer today — a superseded
change is closed and its branch demolished, so a verb naming it meets a ref
that is gone (`45`) rather than a record that says why.

`72` is not a port declining a platform. That is the record's *unsupported*
state, it is frequently the change working exactly as intended, and the
verbs say so and exit `0`.

The `verify.ErrUnsupported` sentinel moves here from the old `3`, the
retired environment code, for the reason the row gives: nothing is missing
that provisioning would supply. Like `50`, it is a renumbering the ruled
table did not enumerate and the bands require.

### Partial — `80`–`84`

| Code | Name | What happened |
|---|---|---|
| `80` | `MintedSubmitErrored` | the branch is minted; the verification submit broke |
| `81` | `PushedPRFailed` | the branch is pushed; the pull request would not open |
| `82` | `PRRefreshFailed` | the branch is pushed; its pull request still describes the change it used to carry |
| `83` | `SweepHardErrors` | a sweep finished with rows that were not declines |
| `84` | `PassNeedsAttention` | a pass whose summary holds a row addressed to a person. A partial-completion code because a pass is N outcomes rather than one refusal |

Re-running is not free and not always safe, so these can never be folded
into `1`: a script must be able to tell "nothing happened" from "the branch
is pushed and the PR is not".

`80` is `app.MintError` and `81`/`82` are `publish.StepError`, each carrying
what already completed as DATA rather than in its sentence — the branch that
now exists and the attempt left on it, the step that failed and the steps
that finished before it. The classifier asks for them **before** the
sentinel table, which is the one place it does not read in band order: a
partial identity wraps the failure that caused it, that cause may carry a
band of its own, and if the cause won, the remedy printed would be "run it
again" — the one thing a caller holding a pushed branch must not do.

`84` reaches a process status in exactly two places, `dockhand dispatch
--once` and a person's `dockhand cycle`. A resident dispatcher never exits,
and one that exited non-zero because a branch needs a person would, under
launchd `KeepAlive`, be a restart loop; `status` is the attention channel
there instead.

### Where the mapping lives

A typed error owns its band **where it is defined**, by implementing
`DockhandExit() int` — so the band cannot be forgotten in a table two
packages away, which is the trap every new error type used to walk into.
`internal/cli/exit.go` holds only the other half: the sentinels, which cannot
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

`status --json` carries no `cleaned` key (D27): `status` cleans nothing, so
the key could never have been true, and a key that is always false is a
promise the document cannot keep. Each branch says `minted` instead —
`false` is a hand-made branch observed for the verify note it carries — and
a `--no-update` read marks the whole document `as_recorded: true`, so a
running run's stale state and a promoted branch's missing pull request are
read as unasked rather than as answers.

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
every slot busy, `60`, and `dockhand cycle` starts it when one frees; the
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
