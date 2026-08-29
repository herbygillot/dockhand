# Verification backends

**Status: provisional.** This is the re-examination of D5 that the fork-CI
discovery forced, incorporating the four-backend enumeration from design
discussion. D4's propositions are taken as given (plus the candidate fourth);
this document is about *where* they get answered.

---

## Prefixes have roles before backends have names

A local MacPorts prefix was about to do three jobs with incompatible state
requirements. Separating them is the design:

- **Evaluation host.** `port-tclsh` must come from *some* healthy MacPorts
  installation. Evaluation is read-only with respect to the prefix — installed
  ports do not change what a Portfile evaluates to — so warm state is harmless
  here, and the live `/opt/local` or any standing prefix serves.
- **Verdict environments are born clean and die after.** The CI principle,
  adopted wholesale: no environment that has run a previous verification may
  testify about this one. Locally that means **ephemeral prefixes** — created
  per verification, destroyed on pass.
- **Sandbox.** A standing prefix where the human iterates — trial builds,
  Portfile development, the place `debug` drops you. It accumulates state
  freely because it emits **no verdicts**. It is D8 state: a cache whose loss
  costs time.

The persistent "test prefix" as a verification backend is therefore removed.
What remains as backends:

| | Ephemeral prefix | VM (`tart`) | Fork CI | Upstream PR CI |
|---|---|---|---|---|
| Example | `/opt/dockhand/e/xxxx`, per run | pinned guest image | branch push to fork | `pull_request` on `macports-ports` |
| Pristine | yes (born clean; host contamination remains) | yes | yes | yes |
| Platforms | host only | pinned, ARM guests | macos-14/15/26, arm64 | same as fork CI |
| Interactive (D7 handle) | on failure, the prefix survives — the handle *is* the undeleted prefix | snapshot/restore | none; runner evaporates | none |
| Capacity | cores; ephemeral prefixes multiply | 2 guests (Apple licence) | ~1 branch at a time (5-macOS-job cap) | not yours to spend |
| Visibility | private | private | private in practice | **public** |
| Cost | base install + all-source dep builds, every run | local CPU + disk + tart present | free minutes, one-time enable | social: a red X, maintainer notifications |
| Position | pre-promote | pre-promote | pre-promote | **post-promote** |

Two properties compose for free. An ephemeral prefix at a generated path is
**nonstandard by construction**, so the prefix-cleanliness check (hardcoded
`/opt/local`) needs no separate machinery — every local verdict environment
performs it incidentally. And D7's preserved-environment handle stops being an
abstraction: pass → delete the prefix; fail → keep it, and the Finding's
handle is its path. One mechanism, two exit dispositions, exactly as D7
wanted.

### Clean is specified, not empty

Born-empty is unaffordable: a nonstandard prefix gets no binary archives from
`packages.macports.org`, so a literal fresh-start builds every dependency from
source on every run — hours per verification for a real dependency tree. But
born-empty was never the requirement. The contamination that undermines
verdicts is state nobody *accounted for* — port A's leftovers masking port B's
missing declaration. An environment containing **exactly the declared closure
and nothing else** testifies as well as an empty one that built its way up.

The decisive precedent is the authority itself. The fork-CI workflow checks
out `macports/mpbb` — the buildbot's own scripts — and mpbb's discipline is
not empty environments: it is a standing prefix with an archive store, where
each build **deactivates everything and activates exactly the declared
dependency closure**, installing from previously-built archives and paying the
source build once per dependency. The tree's authoritative verdicts are
produced this way. Being *cleaner* than the oracle would not make dockhand's
verdicts stronger; it would make them diverge from the verdict that counts.

So the local verdict backend replicates the discipline rather than exceeding
it:

- **A fixed, dockhand-owned, nonstandard prefix path** (say
  `/opt/dockhand/verify`). Fixed, because MacPorts archives are keyed to the
  prefix path — a stable path is what makes the archive store reusable.
  Nonstandard, so the hardcoded-`/opt/local` check still happens incidentally.
  Randomness was never the point.
- **A local archive store.** Every dependency built once is archived; later
  verifications install from archives in seconds. Derived state under D8 —
  `rm -rf` costs rebuild time, nothing else. Across a fleet the amortisation
  is strong precisely where dockhand aims: forty Go ports share nearly one
  closure.
- **Per verification: wipe, unpack the base template, install the declared
  closure from archives, build, verdict.** Wipe-and-repopulate is strictly
  stronger than mpbb's deactivate (it also clears files a build scribbled
  outside its registry) while costing only an untar plus archive installs —
  near-instant on APFS with clonefile. The *instance* is still born clean;
  what is cached is specified, content-addressed, derived state.
- **Only declared deps enter.** The closure comes from evaluation, which is
  the point: a dependency the Portfile fails to declare is absent from the
  environment and the build fails — declaration completeness, preserved.

Whether dockhand implements this discipline or literally drives `mpbb` (it is
open source and designed to be scripted) is an implementation question worth
holding open — driving it would put the environment semantics on the
authority's side of the "own the edit, never own the semantics" line.

## Two observations that reshape D5

**1. "Local" never meant the live prefix — or should never have.** D5's local
backend, as originally framed, hands you "warm shared state — deps installed,
populated distcache." That state lives in `/opt/local`, and using it as a test
bed *mutates the user's real installation*: installing dependencies, activating
ports, upgrading things the test needed. Worse, warm state undermines the
verdict itself — dependencies installed while verifying port A mask port B's
missing declaration, so the environment's testimony degrades with every run.
The resolution is the role separation above: evaluation may use any standing
prefix because it is read-only; verdicts come only from environments born
clean; and the standing sandbox prefix exists for the human, not for verdicts.
The local verdict backend is the ephemeral prefix, whose profile the table
gives — source-builds-only (binary archives are keyed to `/opt/local`),
host-contaminated but prefix-clean by construction, and parallel up to cores
rather than capped by the `/opt/local` singleton (a second narrowing of D1's
"parallelism is capped at one or two," alongside D13's evaluation pool).

**2. Upstream PR CI is not a backend you choose; it is one you incur.** The
other three are *directed*: dockhand decides to spend them. Opening a PR
triggers verification as a side effect of publication — after `promote`, in
macports' own repo, with `macportsbot` notifying maintainers and any failure
standing as a public red X. It cannot be selected as `--where`; it is what the
pipeline flows into. Two things follow:

- Its results are the only **shared** evidence. Reviewers see them; "CI is the
  authority" refers to this backend (and the post-merge buildbot). Backends
  one through three exist to *predict this backend's verdict privately*.
- Letting it be the *first* build verification is legitimate exactly when D9
  says so — a clean low-tier change on your own port, where "Tested on: CI
  only" is accepted practice (`pr-evidence.md` §8). The autonomy policy and
  the backend choice are the same decision viewed from two sides.

## Escalation, not selection

The backends order themselves: cheap → expensive, private → public, and each
stage exists to make the next one safe to enter.

```
evaluate (fidelity)        free        seconds     private   any prefix
ephemeral prefix           local CPU   slow (all   private   born clean
  (viability, artifact                 source)
  state, prefix-clean,
  ~declaration compl.)
fork CI (declaration       free        ~1 hour     private   clean runner
  completeness × 3 OSes)                           in practice
promote → PR CI            social      —           public, authoritative
```

A plan does not pick a backend; it declares **requirements** — which
propositions, which platforms, interactive or not — and the scheduler matches
requirements against what D11's probe found. *Pristine + interactive* → VM or
nothing. *Pristine + multi-version* → fork CI. *Artifact state* → any backend
with a destroot, cheapest first. A missing backend is a fact about the
machine, reported at plan time — never silently substituted by a weaker one.

## What remains of D5

The principle survives: define the interface before the second backend exists,
and keep `Pristine` unsatisfiable where it is unsatisfiable. What does not
survive is the assumption that backends share one shape. The test prefix is
synchronous and stateful; fork CI is asynchronous and returns only findings;
PR CI is not invocable at all. The honest interface is therefore small —
*submit work, await findings, maybe hold a debug handle* — with capability
flags, not a uniform `Executor` pretending a runner and a prefix are the same
kind of thing. The VM backend's unique remaining value, post-fork-CI: pristine
**and** interactive at once, plus OS versions the runners no longer offer.

## Open questions

1. ~~Does warm state undermine verdicts?~~ *Answered: strongly yes.* Resolved
   by the role separation above — verdict environments are born clean and die
   after; standing prefixes host evaluation and the human sandbox only.
2. Is prefix-cleanliness (hardcoded `/opt/local`) worth naming as a
   proposition beside D4's three-plus-one, now that every local verdict
   environment checks it incidentally?
3. Fork CI runs `mpbb`'s changed-ports logic; multiple ports pushed on one
   branch verify together and muddy attribution (`field-evidence.md` §5). One
   branch per port, or accept batch attribution?
4. Does the verdict prefix's base come from the template tarball at the
   version the user runs, or pinned to what the buildbot runs? Skew between
   the two is a fidelity question with no obvious owner.
5. ~~Implement mpbb's discipline, or drive `mpbb` itself?~~ *Examined
   2026-08-24; answer: drive its core, skip its lifecycle.* mpbb is less
   buildbot-coupled than its name suggests: `--prefix` and `--work-dir` are
   first-class options, buildbot env vars are guarded with defaults, no root
   is assumed, and the mirror/upload machinery sits in subcommands dockhand
   never calls. The reusable core is `tools/dependencies.tcl` (712 lines):
   "all dependencies (and only dependencies) active", implemented on
   MacPorts' *internal* API — `mportopen`/`mportdepends`/`portimage` — i.e.
   the real resolver, which is precisely what makes it worth driving rather
   than reimplementing. Three couplings to manage: (a) results are text logs
   — exit codes do not distinguish fetch/checksum/build failure, and parsing
   log lines violates the D11 rule, so dockhand keeps its own staging via
   `port` directly (matching mpbb's flags: `-dkn install --unrequested`) and
   uses mpbb only for the activation discipline; (b) the Tcl tools are
   version-coupled to base's internal API, so `(base, mpbb)` is pinned as a
   pair — which folds into open question 4; (c) `mpbb-checkout` generates a
   buildbot-flavoured `macports.conf` that blacklists the MacPorts mirrors —
   dockhand bypasses it and writes its own two-file conf. `tools/` carries no
   interface guarantee: this is D6-shaped coupling — pin a commit, absorb
   drift with a small adapter.
