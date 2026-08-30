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

## D5 — `Executor` interface in v1, VM backend later

**Decided.** Define the execution interface now; implement only the local backend. Shape it against the VM case regardless.

**Why.** Local hands you warm shared state — deps installed, populated distcache, incremental build products. A VM hands you nothing. An interface designed around local's conveniences bakes in "the deps are already there" and turns the second backend into a rewrite. `PrepareRequest.Pristine` must exist and be unsatisfiable locally from day one.

**Cost to reverse.** Deceptively high — invisible until the second backend exists, by which point assumptions have spread.

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

- *Required:* `port-tclsh`, `git`. `git` declared `bin:git:git` so an Xcode CLT git satisfies it; runtime probe enforces the version floor a dependency declaration cannot express (`git worktree` needs 2.5+).
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
