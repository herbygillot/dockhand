# Decision log

Each entry records what was decided, why, and what it would cost to undo. The reversibility column matters more than the decision — it says which of these to defend and which to change freely once code exists.

---

## D1 — Go, with the CST retained

**Decided (2026-08-25, third and final pass).** Implement in Go, on Go 1.27.
The four-level CST (D14) is retained exactly as designed; only its host
language changes. Enacted: the repository is initialized as
`github.com/herbygillot/dockhand`, and `internal/tcl/syntax` passes the concatenation
invariant on all 20,037 Portfiles in the tree with zero issues.

**History, kept because the log exists to keep it.** Originally Rust, primarily
on reach. Re-litigated when reach was found to conflate MacPorts users with
dockhand users; reaffirmed as Rust on two new legs — CST exhaustiveness and
the terminal-toolchain ratchet. Reversed to Go when both legs were priced
honestly: exhaustiveness can be engineered in Go, and the ratchet reduces to
an acceptable rolling floor.

**Why Go.** Fluency, tooling, and ecosystem (the author's words: Go "wins in
terms of tooling, familiarity, and ecosystem"), for a solo project whose
binding constraint is speed-to-feedback. The ~4,700-line `mports` library
(tclsh JSON-pipe driver, tclsyntax, prefix, sysinfo, phase, log — all tested)
revives instead of being discarded, and `go2port` shares the idiom. Four
fifths of the system is orchestration — subprocesses, GitHub, CI polling,
queues — where Go is at home.

**How the CST keeps its guarantees without sum types.** Three mechanisms,
plus the armor that never depended on the type system:

- *Sealed interfaces* (unexported marker methods) close the node taxonomy.
- *Visitors with generic methods* (Go 1.27) give compile-time exhaustiveness
  for total consumers: adding a node kind breaks every visitor implementation
  at compile time.
- *Linted type switches* (`gochecksumtype`, `exhaustive`) guard the rest at
  CI time, with `default:` banned in taxonomy switches.
- The primary armor is language-neutral: the concatenation invariant and
  differential testing against `port-tclsh` over the checked-in fixtures.

**Known cost accepted.** Exhaustiveness is CI-time rather than compile-time
wherever a type switch is used instead of a visitor. Nil is representable.
Deep pattern matching is absent (mitigated by Tcl's shallow, four-level
taxonomy). The rolling floor: Go 1.27 requires macOS 13, and each macOS
release reaches a terminal Go within a few years — accepted as a
`min_darwin` floor if dockhand enters the tree (see D15), with its cultural
visibility owned rather than denied. Fluency in Rust, foregone.

**Cost to reverse.** High — a rewrite. The CST behind its pure-function
boundary (bytes in, spans out) is the partial escape hatch: it could be
re-hosted without disturbing the orchestrator.

---

## D2 — Read as Tcl, write as text

**Decided.** Portfiles are read by evaluating them through `port-tclsh`, and written by byte-span replacement. Never round-trip through an AST.

**Why.** `version` is frequently computed rather than literal, so textual reads are unreliable. But the output is a diff a human reviews, so any transform that reflows the file destroys reviewability. The two requirements point in opposite directions; the asymmetry is the resolution.

**Cost to reverse.** Highest in the project. Recognizers, spans, the re-read and the whole verification story hang off this.

---

## D3 — No embedded Tcl interpreter

**Decided.** A CST parser, yes. A constrained evaluator over `set` / interpolation as a *locator hint*, cautiously. A Tcl interpreter, never.

**Why.** The re-read is evidence only because the oracle is not dockhand. Reading with an in-house interpreter degrades "my edit did what I intended" into "my interpreter agrees with my interpreter" — which stays true precisely when the interpreter is wrong. Separately, evaluating a real Portfile means implementing the MacPorts port API and executing PortGroups that are themselves substantial arbitrary Tcl.

**If the motivation was subprocess cost:** use a persistent `port-tclsh` helper, one long-lived process per run, speaking JSON over a pipe.

**Note (2026-08-28).** The persistent-helper idea is now implemented as `internal/tcl/shell` (`Proc` + `Session`), but "JSON over a pipe" did not survive design review: Tcl 8.6 has no built-in JSON, so the shim would hand-roll an encoder inside the oracle. The protocol is instead length-framed in both directions — no quoting or escaping logic on either side — with replies as Tcl lists built by Tcl's own `list` command and parsed in Go by the verified list lens. The oracle serializes in the oracle's native format; every reply doubles as a differential exercise of the parser.

**Cost to reverse.** High, and reversing it silently disables the verification model rather than breaking it loudly.

---

## D4 — Verification answers three independent propositions

**Decided.** *Edit fidelity* (re-read), *port viability* (build), *declaration completeness* (build in a pristine environment). Not a ladder — independent questions.

**Why.** A faithful edit can produce a broken port; a wrong edit can produce one that builds fine against a cached distfile. Treating build as strictly stronger than re-read is the tempting error. The disagreements carry the information: failed fidelity alongside a passing build means dockhand is broken and should halt the batch, not fail the port.

**Cost to reverse.** Moderate. Shapes the `Verifier` trait and the gate policy.

---

## D5 — `Executor` interface in v1, VM backend later — **superseded by D17**

**Decided.** Define the execution interface now; implement only the local backend. Shape it against the VM case regardless.

**Why.** Local hands you warm shared state — deps installed, populated distcache, incremental build products. A VM hands you nothing. An interface designed around local's conveniences bakes in "the deps are already there" and turns the second backend into a rewrite. `PrepareRequest.Pristine` must exist and be unsatisfiable locally from day one.

**Cost to reverse.** Deceptively high — invisible until the second backend exists, by which point assumptions have spread.

**Superseded (2026-08-30).** Two of its predictions held and one did not. Defining
the interface before the second backend existed was right, and Pristine as an
explicit, sometimes-unsatisfiable property survives as verify.Capabilities.
What did not survive is the uniform Executor itself: the backends do not share
one shape. A VM is slow-but-pollable, CI is asynchronous and returns only
findings, upstream PR CI is not invocable at all. The built interface (D17) is
submit/poll/release with capability flags, and — contrary to this entry's
ordering — the VM backend came first, because measurement showed it cheaper
than the "local" case this entry assumed would lead.

---

## D6 — T3 blocks delegated to external generators, opaque

**Decided.** `go2port`, `cargo2port` and peers generate vendored dependency blocks. dockhand owns the block boundary and nothing inside it.

**Why.** These tools are already what maintainers trust. More importantly, the `go.vendors` format is not stable long-term — it breaks Go module mode and the PortGroup may move. Opacity means a future scheme costs an adapter, not a rewrite.

**Note (2026-08-24).** The instability is ecosystem-specific, which strengthens rather than weakens the decision. `cargo.crates` is stable and cooperates with cargo's default resolution, so it is likely to persist. `go.vendors` is under pressure for a structural reason: it fights its own build tool, disabling Go module mode in order to build from MacPorts-mirrored sources, and a mechanism that breaks the toolchain's default behaviour accumulates reasons to be replaced. (A few ports have upstreams that ship complete `vendor/` directories, which makes the block unnecessary for those ports — but that is incidental, not a trend, and not the driver.) The two block schemes are heading toward *different* futures: one persists, one is likely to be reworked. Opacity prices both at one adapter each, which is exactly the property that justified it.

**Cost to reverse.** Low.

---

## D7 — Findings are the universal unit of triage

**Decided.** Recognizer declines, verification failures, gate rejections and red CI all become typed `Finding` records. A Finding may carry a handle to a preserved environment.

**Why.** One vocabulary across discovery and verification means one view is the whole UI. Snapshot handles collapse "automated test" and "environment to debug in" into one code path with two exit dispositions.

**Cost to reverse.** Moderate.

---

## D8 — State is a cache

**Decided.** Everything persisted must be re-derivable from the ports tree plus GitHub. `rm -rf` costs only time.

**Why.** GitHub already holds open PRs, branch names, CI results and review state. Anything that cannot be rebuilt is a design bug.

**Cost to reverse.** Asymmetric — cheap to violate, expensive to restore. Let non-derivable state creep in during month one and you have quietly acquired a database.

---

## D9 — Autonomy is a function of tier and evidence

**Decided.** No global `--yes`. Clean T0/T1 changes on ports you maintain may auto-PR; everything else parks for batch review. PRs publish provenance including what was *not* checked.

**Why.** Reviewers meet automated PRs with suspicion by default; candour about the limits of testing buys goodwill silence does not.

**Cost to reverse.** Low. Policy, not structure.

**Amended (2026-08-30).** RefreshChecksums is the standing exception, by
construction rather than by gate: it is T0 and its evidence is perfect, and it
still never auto-promotes, because a checksum moving at an unchanged version is
a supply-chain question wearing maintenance's clothes. The exception lives in
the intent, where it is visible, not in the gate, where it would not be — the
built command warns on every run. Local application stays fine under D16; the
human asking is the loop.

---

## D10 — Any maintainer, any port

**Decided.** dockhand runs against any port, not only ones you maintain. The PR is the notification — that is how the tree already works. Ownership sets the *gate*, not the capability: your ports may open PRs unattended, others' never do until you have read the diff.

**Why.** Earlier framing over-corrected toward ownership. The real risk is volume without attention, which attaches to autonomy and scale rather than to who maintains what.

**Note (2026-08-25) — the primary persona has no commit bit.** dockhand's
expected user is a maintainer *without* commit access: they open PRs; a
core-team committer merges them. Two consequences. Every change dockhand
produces — including on ports the user maintains — terminates in committer
attention, so the core team's review capacity is the global throughput
bottleneck, and PR convention-completeness (the commit plan, truthful
templates, correct trailers) is the lever that moves it: a PR a committer can
merge without typing anything is the fastest PR. And the 72-hour policy
windows read asymmetrically: expiry enables a *committer* to act, never the
user, for whom the window is only a lower bound on waiting. The field
evidence flagged its own commit-access bias as its weakest area; this note is
the corrected premise.

*Author versus persona (2026-08-25).* dockhand's author holds commit access;
the persona does not. Two consequences worth separating. Releases of the
dockhand port itself are self-served — the author commits the bump directly —
so tree distribution carries no cadence penalty for this project, whatever
the persona's constraints. And the author cannot natively dogfood the
primary persona's journey: `promote`-then-wait-on-a-committer is precisely
the path daily use will never exercise. The non-committer flow therefore
needs deliberate testing — a test account without the bit, or early feedback
from a real non-committer user — because it is the product's most important
path and the one its author does not live.

**Cost to reverse.** Low.

---

## D11 — Three tiers of external tooling

**Decided.**

- *Required:* `port-tclsh`, `git`. `git` declared `bin:git:git` so an Xcode CLT git satisfies it; runtime probe enforces the version floor a dependency declaration cannot express. The floor is 2.25, resolved (2026-08-31) from the introducing releases of the three porcelains D21's write path needs: `git notes` is ancient (1.6.6; full subcommand set 1.7.1, merge strategies 1.7.4), `git worktree` needs 2.5, and `git sparse-checkout` — the binding one — needs 2.25.
- *Assumed:* `patch`, matching base's own assumption. Exit status only, never message formatting. The verdict must come from whichever patch engine MacPorts will use for that port.
- *Discovered:* `tart`, block generators. Resolved from `PATH` at startup; presence determines which tiers and verifications are reachable.

**Why.** Depending on the discovered tier would inherit each tool's platform floor, gating the majority of dockhand's work on tooling a minority of ports need. A missing tool is a fact about the machine, never a finding about the port.

**Note.** The probe's unit should be `(tool, path, version, satisfies_floor)` from the start. Retrofitting versions onto a presence check ends up sprinkled through the codebase.

**Cost to reverse.** Low.

---

## D12 — Patchfile refresh: tentative

**Undecided.** Detecting staleness at extract time is clearly in scope. Recognising an *obsolete* patch — one that reverse-applies cleanly, meaning upstream merged the fix — and removing it is mechanical and verifiable. Diagnostics for the remainder are useful.

**Why held.** The failure mode is uniquely bad. A patch applied with fuzz in the wrong place yields a port that builds successfully and is subtly wrong — worse than one that fails outright, and invisible to every check in the design. Offsets are safe; fuzz is never accepted unattended. `git apply` may diagnose but must never render the verdict, since three-way merge succeeds where `patch` fails.

**Cost to reverse.** Low — it is additive.

---

## D13 — Fidelity enumerates subports, always

**Decided.** The edit-fidelity check evaluates *every* subport of the target
Portfile, before and after, and compares the full set. Not the target subport
alone, and not a conditional that enumerates only when the edit was an
insertion.

**Why.** Replacement inherits scope; insertion chooses it. Swapping an existing
`revision 1` for `revision 2` preserves whatever scope that line sat in and
cannot get it wrong. Inserting a `revision` line picks a scope — and placing it
at top level in a subport-bearing file silently moves every subport. Checking
only the target subport proves nothing about the others, so the check would stay
green precisely when the edit was wrong.

A conditional check — enumerate only for insertions — is the tempting economy
and the wrong one. It makes correctness depend on correctly classifying the edit
shape, which is one more thing to get wrong, and it leaves the cheaper path in
place to be reached for later under time pressure.

**Consequences, all of which are improvements.**

- `Metadata` is keyed by *(subport, variant set)*, not flat. This settles half of
  the standing open question about variant-relative metadata by making the other
  half unavoidable.
- `Diff` compares sets, so it must handle **membership changes** — subports
  added and removed, not only fields modified. A subport appearing or vanishing
  across a bump means the port's installed artifacts changed shape, and a flat
  check misses it entirely. Field evidence supplies the case: `mp4ff` went from
  four binaries to eight.
- The `Plan` must **predict its blast radius**. The assertion is not "exactly one
  subport moved" — a top-level version bump correctly moves all of them. It is
  "the set that moved equals the set the plan predicted", which is strictly
  stronger than checking fields alone.

**Known cost accepted.** Fidelity goes from 2 evaluations to 2 × (1 + N). About
a quarter of the tree carries subports, most of them generated rather than
written: 2,285 Portfiles set `python.versions` (969 of them list five), 2,153
use `perl5.setup`, and 574 declare subports statically, one of them forty. N is
typically one to five and occasionally forty.

**Note.** Subports cannot be enumerated from text — PortGroups generate them,
and `cmake-devel` builds subport names from variables. The set is an evaluation
result, which is consistent with values coming from execution and locations from
text.

**Note.** This makes evaluation throughput matter in a way it did not before,
and evaluation — unlike building — does not mutate `/opt/local`, so it
parallelises freely. D1's remark that "Go's concurrency advantage is
inapplicable" was reasoning about builds and does not extend to this hot path.
That does not reopen D1, whose primary argument is reach and is untouched; it
does mean dockhand wants a small pool of persistent `port-tclsh` helpers rather
than one, which is a scaling of D3's existing proposal rather than a new idea.

**Cost to reverse.** Moderate structurally, high in practice. The keying of
`Metadata` and the set semantics of `Diff` both follow from it, and reverting
would silently weaken the check rather than break it loudly — the same hazard
D3 carries.

---

## D14 — The parser is a four-level CST with lensed brace reinterpretation

**Decided.** The parsing artifact is a full-fidelity concrete syntax tree of
Tcl's actual grammar — Script → Commands → Words → Segments, byte spans at
every level — with brace bodies stored opaque and reinterpreted on demand
through exactly two lenses: a script lens (re-parse as commands) and a list
lens (split as a Tcl list, span-preserving). No expr lens exists.

**Why.** Tcl has no higher grammar — control flow and declarations are
commands, so four levels is *complete*, not minimal. The fourth level
(intra-word segments: literal / `${var}` / `[cmd-sub]`) is required regardless
by correspondence analysis on computed versions. Which brace holds a script
and which holds a list is decided by the consuming command, not by grammar, so
the lens decision must live with command knowledge, outside the parser. This
resolves the session's lexer-versus-CST oscillation: the exclusions the
"lexer" framing fought for survive as the lens rule and the missing expr lens;
the tree the "CST" framing wanted survives because for Tcl it is small.

**Verification.** Two harnesses, both prior to any use: the concatenation
invariant (spans reproduce input byte-for-byte — enforced continuously over
the checked-in fixtures, and demonstrated once against a full ports tree),
and differential testing against `port-tclsh`'s own answers (`lindex`,
`info complete`) — the oracle-is-never-dockhand principle applied to the
parser itself. tree-sitter is a permitted test harness
(disagreement mining) and a forbidden foundation: edit-grade byte fidelity
exceeds community-grammar quality, and the lens decision defeats context-free
grammars anyway.

**Cost to reverse.** Moderate. Every recognizer and both locators consume the
node taxonomy; changing the tree's shape after they exist reprices all of
them. The lens rule is cheap to relax (add a lens) and expensive to violate
(parse expr, and D3's boundary starts eroding).

**Note (2026-08-25).** Hosted in Go per D1's final pass; the enforcement kit
(sealed interfaces, generic-method visitors, linted switches) is recorded
there. First implementation: `internal/tcl/syntax` — 18 unit tests over the real
idioms, tiling invariant enforced structurally, a one-time validation run
over a full ports tree (20,037 Portfiles, zero errors, ~4 seconds), and a
standing hermetic fixture suite: 200 varied Portfiles, all 103 PortGroups,
and 50 Tcl scripts, every one required to parse error-free.

The differential harness landed 2026-08-28 (gated on a real `tclsh`): over
every fixture, completeness agreement with `info complete`, truncation
inside braced spans detected identically, and list-lens splits matching
`llength`/`lindex` element-for-element — thousands of checks. It caught a
real bug on first contact (the list lens treated backslash-newline as an
escape; rule [9]'s prepass makes it whitespace even inside braces) and
surfaced one principled oracle split, now documented in the harness: a file
ending in backslash-newline is accepted by `source` but pends under
`info complete`; the parser sides with `source`.

---

## D15 — Distribution is staged: binaries first, the tree when stable

**Decided.** dockhand ships as GitHub release binaries (and `go install`)
while it iterates; it enters `macports-ports` as a port when releases slow
down and the tool is supportable. In the tree it declares the `min_darwin`
floor its Go toolchain implies rather than holding back language versions.

**Why.** The author holds commit access, so tree releases carry no cadence
penalty — the earlier argument against early entry died with that fact — but
tree entry still implies supportability, and the early phase wants weekly
iteration without even self-imposed ceremony. Release binaries inherit only
the compiler's runtime floor. When the port lands, its own Portfile ships
`vendor/` in the release tarball, so it needs no `go.vendors` block and does
not ride that machinery's instability.

**Known cost accepted.** A rising `min_darwin` once in the tree (Go's rolling
floor), culturally visible in a community that values old platforms. Out of
tree, discovery is worse (`port search` finds nothing) and trust rests on the
author's account rather than review.

**Cost to reverse.** Low. Timing, not structure.

---

## D16 — bump applies by default; planning is opt-in

**Decided (2026-08-30).** `dockhand bump <port>` plans the change and carries
it out. `--plan` emits the plan on stdout and changes nothing. This reverses
the CLI design's original position, which made plan-only the default on the
grounds that it was both the convenient default and the safe one.

**Why.** The convenience half was wrong. The common case is one port, one
bump, and plan-only made it three commands and a temporary file for a change
the tool had already computed and verified. The safety half does not depend on
the default: a plan is still always produced, and applying it goes through the
same path `apply` uses, which refuses if the Portfile moved since planning and
restores the original bytes if the observed delta differs from the predicted
one by so much as a field. The protection is the verification, not the
inaction — and making the safe thing cost three commands is how a tool teaches
people to skip it.

**Known cost accepted.** The default now writes to the user's ports tree,
where before no invocation of `bump` could. Mitigations are the prediction
check, the restore on mismatch, and the fact that a ports tree is a git
checkout. A user who wants the old behavior asks for it by name.

**Cost to reverse.** Low. One flag's default and a line of docs; the two code
paths already exist and are shared.

---

## D17 — Verification is submit-and-poll; a Job is a value

**Decided (2026-08-30, built).** `verify.Verifier` is Capabilities / Submit /
Poll / Release. Submit returns when work is running, not when it finishes.
Poll never blocks and never mutates — polling a finished job twice answers the
same way twice. Release is the caller's decision, because only the caller
knows it is done with the environment, and on a two-slot provider an
unreleased job is a slot that never returns. Await is a free function over
Poll, so no provider implements blocking. A Job is a plain serializable value
{provider, id, started}: the process that submits is not necessarily the one
that collects.

**Amended (2026-08-31).** The contract gains Log(ctx, job): the build's
output, fetched deliberately. It was previously a field on Poll's Status,
which made every poll carry a log read — tolerable for a local VM, wrong by
construction for a CI provider where the log is a download. Poll is now
cheap state only; the log is asked for when someone wants it (`dockhand
log`, the failure tail), which is also the shape a future GitHub provider
needs.

**Amended (2026-08-31).** Request gains Test: verification can also run the
port's test suite. Two rulings, both taken from mpbb. Test is additive, never
a replacement — mpbb keeps install-port and test-port as separate steps, and
`port test` builds but never destroots or activates, so it cannot stand in
for the install that verification is. And test runs BEFORE the install, as
its own fresh invocation with -k, the install continuing from the kept work
directory: the reverse order — install then test — was measured failing with
EPERM in the guest, because two `port` invocations leave the work directory's
files owned between root and the macports user, while test-then-install is
just the normal single-run progression (build → destroot → activate) split
across two commands. The note records the bit (`tested`), because promote's
checklist vouches only for what a note remembers.

**Amended (2026-09-01, machine-wide admission).** VM capacity is
admitted, not discovered: under a per-user machine lock
(~/Library/Caches/dockhand/tart.lock), occupancy is counted live from
`tart list` — derived, never recorded, the D19 stance machine-scoped —
and a full machine refuses in about a second with a typed
CapacityError that the deferred-branch flow absorbs, where it used to
be discovered through a two-minute agent timeout. Derived counting
also sees VMs dockhand did not start, which spend Apple's licence
slots just the same; any ledger would have missed them. The lock is
held until the admitted VM is itself visibly running, so concurrent
dockhands serialize their starts instead of both counting the same
free slot; the boot's error is captured, not discarded. Every boot
site admits: workers, probes, provisioning, recheck. Attribution is a
separate, deliberately informational sidecar
(~/Library/Caches/dockhand/workers/) mapping worker to owning
checkout — admission never reads it, so staleness can only mislabel a
status line, never mis-admit a VM.

**Amended (2026-09-01, notes are shared state now).** The verify notes
are dockhand's only mutable state, edited as read-modify-write of
whole JSON documents — safe while one human ran one dockhand, a
lost-update race the day two agents shared a checkout, which is how
the tool is now actually used. Every note critical section (record,
settle, cancel, discard's read-release-remove) runs under a per-repo
advisory flock, and settle re-reads the note inside the lock rather
than trusting the caller's copy. Two evidence rules landed with it:
a failed run's first substantive Error line is stored as the run's
Detail at settle time (the failure-side twin of the lint evidence,
read before the log becomes unreachable), and the --verify gate
returns its lint evidence so a gate-verified tip's note reads exactly
like a background-verified one — one verdict vocabulary, however the
verification ran.

**Amended (2026-09-01, cargo.crates geometry).** Regenerated vendored
blocks are re-laid under the existing block's measured column
geometry, but only a geometry proven first: Assess re-renders the
existing block's own triples and must reproduce it byte for byte
before its rule is ever used, so a wrong measurement is impossible to
act on — anything unproven keeps the tool's verbatim output. The
proof-not-heuristic shape came from the tree itself: its blocks are
written by more rules than cargo2port has flags (a script layout
right-aligns versions in a narrow sliding field and overflows long
ones; the tool's own reserves the widest), and 313 of 345 committed
blocks round-trip exactly. This narrows the old "never reflow tool
output" stance to "never touch anything but whitespace between opaque
words".

**Why.** Even the "synchronous" backend takes fifteen seconds to nine minutes,
long enough that a blocking call is a lie the first time someone interrupts a
run. The serializable Job forced the tart guest to drive its own build and
record its own state, which is also what lets a verification survive dockhand
exiting. Failed and Errored are distinct states because one is a finding about
the port and the other a fact about the machine, and conflating them reports a
broken VM as a broken port.

**Cost to reverse.** Moderate. The state-file protocol inside guests and any
saved Job files assume it.

---

## D18 — Verification environments are provisioned from vanilla, and never carry another package manager

**Decided (2026-08-30, built).** Base images are built from Cirrus Labs'
vanilla macOS images: dockhand adds the tart guest agent (pinned, from the
official release), MacPorts (pinned to the newest shim version), and nothing
else. The Homebrew-bearing base/xcode images are not used, and excision was
rejected: in those images the guest agent is itself a brew formula, so
removing Homebrew without rescuing it yields an image that looks healthy while
running and is unreachable once cloned. Never-had-it beats removed-it.

Two supporting decisions ride along. dockhand authors the guest launchd
plists, so the PATH every guest command inherits names MacPorts and the
system, and no foreign prefix — the stock plists hardcode /opt/homebrew/bin.
And provisioning proves what it built rather than trusting its steps: foreign
prefixes absent, MacPorts answering, and a working compiler — the last because
a published macos-tahoe-vanilla shipped with no command line tools at all (an
upstream template greps for a label spelling Tahoe changed), and MacPorts
installs and answers happily without one.

**Known cost accepted.** One SSH bootstrap per provisioning run, password auth
against the images' documented credentials, because vanilla has no agent until
we install one. Ports needing full Xcode (about 1.2% of the tree) are not
served by these images yet.

**Cost to reverse.** Low. Re-provision from a different variant; the verifier
does not care where a base came from.

---

## D19 — Environment integrity is derived, never recorded

**Decided (2026-08-30, built).** Every verification proves its environment
before staging work into it: no ports installed, no foreign package manager, a
working compiler, MacPorts answering. The check runs on the worker (already
booted, ~1s) rather than the base (~10s to boot), observes the environment the
build actually runs in, and classifies failure as ErrNoEnvironment — a machine
fact, never a port finding. There is no manifest: every fact the check needs
is observable from the guest in about a second, and a recorded copy could go
stale or be contaminated alongside what it vouches for. A signature stored
inside the envelope vouches for nothing — and a manifest is also exactly the
store D8 forbids. The recovery path is a golden clone per base, taken after
provisioning's checks pass and never started; restoring is a copy-on-write
clone, free, so it is the obvious response to drift rather than a last resort.

**Amended (2026-09-01, Xcode in the golden).** Provisioning can bake a
full Xcode into the image (`provision tart --xcode <dir of .xip>`),
installed between the toolchain check and MacPorts, proven by
xcodebuild answering, and recorded in the golden — the versatility
buy: ports with use_xcode need xcodebuild, which the command line
tools alone cannot answer. Three rulings inside it. The archives are
supplied by the user, because Apple's downloads sit behind an Apple ID
and the guest must never hold credentials. The version is chosen per
release from a bounds table (Monterey stops below 14.3, Ventura 15.3,
Sonoma 16.3, Sequoia 26.4 — Apple raises the floor mid-line, so bounds
are minors), newest that fits, betas skipped: verdicts from a beta
toolchain answer a question nobody asked. And the transfer rides the
provisioning SSH channel (a raw pipe into cat), because tart's
directory sharing needs a macOS 13+ guest and Monterey is in the
fleet.

**Cost to reverse.** Low. Add a manifest later if provenance ever drives a
decision; nothing consumes one today.

---

## D20 — The intent catalogue is closed: ten intents, five families

**Decided (2026-08-30).** The catalogue in intents.md is a fixed list — Bump,
RefreshChecksums, RegenerateVendored, BumpRevision, BumpEpoch, Set,
ChangeDependency, DropPatch, FixLivecheck, Obsolete — closed by the family
rule: an intent restores truth, moves a counter, edits a declaration, repairs
instrumentation, or ends a life. Anything fitting none of those, or needing
authored content to reach its end state, stays out. Sweep is a shape point
intents run in, not a kind of intent.

**Why.** Two implementations and a two-year survey of the tree's history. The
survey (intents.md, patch survey) struck RefreshPatches as standalone — 88
mechanical occurrences in two years, all during bumps — and folded it into
Bump with payload identity as its acceptance judgment; it promoted DropPatch,
whose 523 drop-only bumps are where patch-related value actually sits.
MigrateIdiom was struck because an open-ended family cannot sit in a closed
list. FixLivecheck entered because the upstream corroboration machinery is
already its verifier.

**Known cost accepted.** A closed list will be wrong eventually. The remedy is
this log: an addition must argue its family or argue a new one, in writing.

**Cost to reverse.** Low for any single entry; the closure rule itself is the
thing to defend.

---

## D21 — The unit of operation is a branch

**Decided (2026-08-31).** A write intent's product is a git branch. `dockhand
bump jq` creates `dockhand/jq-<new version>`, materializes it in a sparse
worktree, lands the change as formatted commits, and submits verification
against the tip; from that moment the worktree is disposable. The branch is
the durable identity: verification verdicts attach to its commits (git notes
under `refs/notes/dockhand/verify`, keyed by sha), `status` reconciles
branches against verdicts and workers, and `promote` pushes the branch and
opens the PR. Plans remain what they already were internally — the typed
interchange between planner, applier, and verifier — and stop being a
user-facing artifact. Read-only intents are untouched: the unit governs what
dockhand produces, not what it reads.

**Why.** Four observations converged.

- Review culture polices the commit (`pr-evidence.md` §2–3). A branch of
  formatted commits is the artifact review acts on, produced at bump time
  instead of synthesized at promote time — and the observed cohort shape
  (a bump plus its grouped revbumps, N=9) is just commits on one branch.
- The plan-hash drift check fights the most realistic workflow: the user
  hand-editing after dockhand's change. On a branch, human and dockhand
  commits interleave and verification tests HEAD, whoever made it.
- Identity in a filesystem location — a plan file in a temp directory, or a
  worktree path — is destroyed by cleanup. Identity in a ref is protected by
  git: removing a worktree mid-verification orphans nothing.
- Verification scope stops being plan-carried. `git diff --name-only
  <base>...<tip>` names the changed portdirs, and it is automatically right
  for human commits too — a capability a plan-carried scope could never have.

**What made it affordable.** Sparse worktrees, measured on the real tree:
0.153s and 1.4 MB for one portdir plus `_resources`, against 3.2s and 191 MB
for a full checkout. Sparse checkout is a day-one requirement, not an
optimization — with one constraint stated now rather than discovered later: a
sparse worktree does not contain the rest of the tree, so anything assuming
whole-tree reads runs against the main checkout, never the worktree.

**Naming.** The local ref is `dockhand/<port>-<new version>`. The prefix is
lifecycle ownership: dockhand lists, dedupes ("a change for jq is already in
flight"), and prunes what it created, and a half-owned namespace rots. Two
lifecycle rules settled 2026-08-31: an intent finding an in-flight branch for
its port **refuses by default**, naming the branch — settled 2026-09-01
as `--force`, one meaning at every ring: replace what dockhand placed.
On the intent verbs it demolishes the in-flight branch through
discard's path (verification canceled, workers released, notes
removed) and re-derives the port from scratch — subsuming bump's old
at-latest override — but refuses a branch carrying commits the user
added past the mint, which only an explicit `discard` may drop. On
`promote` it force-pushes the fork copy (with lease, so a copy moved
from another machine refuses rather than tramples) and refreshes the
open PR's title and body. And `status` never auto-discards — amended (2026-08-31):
status now performs exactly one deletion, a branch whose PR merged,
announced as it happens, because a merged PR is GitHub's own word that
the work landed — and the fork copy goes with it, in status and in
`clean` both: dockhand placed it, so dockhand deletes it (plain
`discard` still leaves it, because there the copy may back an open
PR); every other cleanup remains the user's explicit act,
and `clean` stays as the manual sweep and the home of kept/closed
reporting. The original sweep verb — `dockhand
clean` (superseding a briefly-held `discard --merged` spelling the same day),
which walks the `dockhand/*` namespace, reads each branch's PR state from
GitHub, and removes what merged: worktree, branch, notes. The branch→PR link
is itself derived, never stored — git holds no PR metadata: `promote` sets
the branch's push configuration (fork remote, same ref name), and the PR is
found by querying pulls with head=`<fork-owner>:<branch>`, state=all — the
same lookup `gh` performs. The pushed name is the local name, so even lost
config derives the query from the branch alone. Merged-ness is
never sha-ancestry: the project's merge styles rewrite commits as they land
on master, so `git branch --merged` sees nothing and per-commit patch-ids
die under squash. The check is content — the paths the branch touched,
compared blob-for-blob against the upstream default branch. Byte-identical
confirms the landing; differing bytes under a merged PR still clean, but say
so (committers amend during merge, and later bumps supersede). A
closed-unmerged PR is never cleaned, only highlighted: rejection is
information. The verb borrows `port clean` safely — both mean "remove the
tool's own accumulated work-product," so the borrowing principle that
forbade `checksums` permits this. The pushed ref keeps the prefix (decided
2026-08-31, revising a same-day refspec-rename idea): local and remote names
are identical, `dockhand/<port>-<new version>` everywhere. The fork-observed
`<port>-<version>` convention was an observation of habit, not a policy —
nothing in review polices ref names — and the prefix in a PR head ref is
honest provenance, consistent with D9's candour. Identity buys real things:
no refspec, trivial push configuration, a bare `git push` that works for the
human who took over the worktree, and fork-side pruning safely scoped to a
namespace dockhand owns on both ends.

**Amends D16.** `bump` still applies by default — no plan file, no second
command — but the application now lands as commits on a fresh branch rather
than uncommitted edits in the user's checkout. That is more conservative than
D16's known cost, not less: the branch the user is standing on is never
touched. Uncommitted in-place application survives by decision (2026-08-31)
as `--in-place`: dockhand edits the Portfile where the user stands, stages
nothing, mints nothing — the prediction check and restore-on-mismatch still
run, since they live beneath the workflow layer. The flag exists for the user
already running their own workflow, who wants dockhand's mechanical edit
folded into their work surgically — an editor, not a workflow manager. Its
stated costs: no sha means no note, so `verify` can report a verdict but not
record one; the change is invisible to `status` and never directly
promotable; and on a non-git tree — which only `--in-place` can serve — it
warns loudly that the next sync destroys the edits. This is D16's survivor:
the immediacy that flipped that default lives on in the flag while the
default graduates to the branch.

**Known cost accepted.** Git becomes a hard dependency of the write path;
rsync trees get read-only intents (their write story was already a footgun —
the next sync destroys the edits). The base-ref policy becomes decision one:
branch from the tree's primary branch at its local position, warn when it is
behind, never fetch autonomously. Verdicts in notes are records, not caches —
a build happened; they are not re-derivable — which softens D8 by one fenced
species of state: sha-keyed, local, and worthless to anything but `status`.
Local is deliberate, not an accident of git's defaults (2026-08-31): a note
answers "ready to promote?", a question only this machine asks, and after
`promote` the branch's verdicts come from CI on the PR — the only evidence
reviewers would credit anyway. dockhand never pushes notes refs; the one way
they leak is `git push --mirror`, which ships every ref, and that hazard is
documentation's to name, not machinery's to prevent.
And background verification by default finally forces the two-slot scheduler
question into the open instead of leaving it deferrable.

**Cost to reverse.** Moderate at the surface: the CLI grammar and its
documentation reorganize around branches. Low underneath: plans never stopped
being the internal interchange, so reversing is re-exposing what is already
there.

**Amended (2026-08-31, same day): no worktree by default.** The branch is
minted in the object database with plumbing — hash the new blob, graft it
into the base commit's tree, commit-tree, update-ref — and verification
stages the guest from the object DB (`git archive <sha> -- <portdir>` feeds
the tar the provider already pipes in). No checkout exists until a human
wants one: the user checks the branch out themselves, in their own checkout,
and edits there. This dissolves four things at once: the
one-checkout-per-branch conflict that forced editing inside a hidden
worktree; most of the work-root question (the sparse recipe — cone checkout,
`index.sparse`, root under `.git/dockhand/wt/`, ~1.5 MB per worktree
measured — survives as the opt-in isolation case); the hooks policy
(plumbing runs no commit hooks, structurally); and dirty-checkout
interference (`bump` never reads or touches the working tree — planning
reads the Portfile from the base commit's tree, so the commit's parent
always contains what was planned against). The sha gap is the drift
mechanism: jobs and notes key to the submitted sha, so `status` warns when
a tip has moved past it ("verification is testing <old>, N commits behind";
"tip unverified; last verdict is for <old>"), `verify` on a moved branch
cancels the stale run and resubmits the tip, and `promote` refuses an
unverified tip. A message-only amend moves the sha but not the tree, which
is where the recorded tree-sha lets `status` say the content still matches
a passed verdict.

---

## D22 — The bump target is written in carrier vocabulary; uncorroborated carriers prove themselves by counterfactual

**Decided (2026-08-31, built).** A bump writes its target verbatim into
the carrier span and lets the shadow evaluation derive the evaluated
version — never the reverse. Targets come from upstream (livecheck,
forge tags), and upstream speaks the carrier's vocabulary: CPAN says
1.23, the rust-analyzer tag says 2026-08-24; the evaluated form is
MacPorts-internal and exists only after evaluation. For the large
majority of ports the two vocabularies coincide and nothing changes.
Acceptance follows the vocabulary: an untransformed corroborated
carrier is held to exact equality as before, while a transformed or
probed one is held to movement — the version's new value is the
Portfile's transform of the literal, known only to evaluation, and the
prediction check still pins it exactly.

**Two families unblock at once.** Registered transforms (perl5): locate
already corroborated through the Go-ported transform, and the
TransformedStyle decline — "the new literal cannot be derived from the
requested version alone" — dissolves, because no inverse is ever
needed. Ad hoc Portfile transforms (rust-analyzer's
`version [string map {{-} {}} ${github.version}]`): corroboration
fails textually, so the carrier proves itself by **counterfactual** —
write the target into the last literal candidate, shadow, re-evaluate,
and demand the version moved. This is the corroboration rule extended
one step, from "text equals value" to "text demonstrably drives
value", at the cost of one evaluation, only on that path. Latest
resolution rides the same candidates: the style family is enough to
ask livecheck and the forge, both of which answer in carrier
vocabulary.

**Why.** The blocks shared one root: the assumption that targets
arrive in evaluated vocabulary, which forced inverting transforms —
reimplementation by another name. Writing through the carrier is
evaluate-don't-parse winning the argument one level higher: dockhand
never computes what a Portfile means, including what version a literal
becomes. Proven live: rust-analyzer resolved latest with agreement,
probed its github.setup carrier, and planned version + checksums + a
regenerated 237-crate block, with the version line untouched in the
diff because evaluation, not dockhand, owns the transform.

**Measured (2026-08-31), full tree, 20,049 portdirs.** Bumpable
carriers went from 17,730 (88.4%) to 19,799 (98.8%): the perl5 family
is the bulk (2,012 ports, 10.0%, located all along and blocked only by
the TransformedStyle decline), and the counterfactual probe rescued 57
of the 307 not-literal ports — the ad hoc family the probe exists for:
tags with underscores (2_5_4), dashes (4-2-3), compound tags carrying
two versions (10.2+2.0.3), date-suffixed versions, and rust-analyzer's
date arithmetic. The probe's synthetic shape-preserving perturbation
broke zero evaluations. What remains unbumpable is 1.2%: 65 ports
whose candidates provably do not drive the version, 183 with no
literal candidate at all (versions computed from dates or extracted
strings), and 2 with no recognized style — each an honest decline.
The 65 are a survey artifact more than a wall: 60 are SHA-pinned
carriers (646 exist tree-wide), where the synthetic perturbation
bumped the SHA's tail and the Portfile reads its head. With a real
target they bump today — `--to <sha>` proven live on portaudio, real
tarball fetched, stale date prefix left as the human's edit on the
branch. What stays rejected is branch-head-as-latest: no upstream
declared it a version, no second resolver corroborates it, and a
pinned snapshot is often a choice. The target stays human-supplied.

**Known cost accepted.** The transform registry is closed: perl5's Go
port survives as read-side corroboration for classify's census, on
grandfather terms, and no transform is ever ported again — the probe
is the growth path, ending the faithful-port-warts-included treadmill.
A probed location is a weaker proof class than textual corroboration
(the span drives the version at this value; it is not thereby "the
version field"), which classify should eventually report distinctly;
today classify still reports these ports not-literal, since a static
census has no target to probe with. And --to is documented as the
version as upstream names it: a user who types the evaluated form for
a transformed port writes a nonexistent tag and fails loudly at fetch.

**Cost to reverse.** Low. The exact-acceptance path is untouched; the
carrier-vocabulary and probe paths are additive branches with the old
declines one revert away.
