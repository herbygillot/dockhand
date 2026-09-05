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

- *Required:* `port-tclsh`, `git`. `git` declared `bin:git:git` so an Xcode CLT git satisfies it; runtime probe enforces the version floor a dependency declaration cannot express. The floor is 2.5, re-resolved (2026-09-01) after the assessment caught the old 2.25 reason — sparse-checkout — outliving the worktree-based design that needed it. What the write path actually needs: `git notes` (ancient; full subcommand set 1.7.1) and worktree-aware plumbing (`--git-common-dir`, 2.5, which the notes lock resolves) — the binding floor.
- *Assumed:* `patch`, matching base's own assumption. Exit status only, never message formatting. The verdict must come from whichever patch engine MacPorts will use for that port.
- *Discovered:* `tart`, block generators. Resolved from `PATH` at startup; presence determines which tiers and verifications are reachable.

**Why.** Depending on the discovered tier would inherit each tool's platform floor, gating the majority of dockhand's work on tooling a minority of ports need. A missing tool is a fact about the machine, never a finding about the port.

**Note.** The probe's unit should be `(tool, path, version, satisfies_floor)` from the start. Retrofitting versions onto a presence check ends up sprinkled through the codebase.

**Amended (2026-09-02, tools are found through one injected finder).**
"Resolved from PATH at startup" is no longer the mechanism. Every
tool dockhand drives — git, gh, tart, curl, the Tcl shells, the block
generators — is a `tool.Tool` resolved by one `tool.Finder`: built
once at the composition root (cmd's Root) over os/exec's own PATH
search, and handed explicitly to every component that execs — a field
on the run's Context, a parameter to `git.Open` that the Repo carries,
a field on `tart.Provider` and `provision.Tart`, the closure the gh
runner and the verify-provider resolver are built as, a parameter to
the upstream tag resolver, a field on the vendored regeneration
context, a parameter to distfile's extractor, prefix discovery, and
doctor's probe. Nothing is resolved at startup: a lookup runs at first
use and its success is memoized per finder, misses never — a tool
installed mid-process is found, not remembered absent. The point of
one finder is that doctor and the working code cannot disagree about
what the machine has, because they ask the same object; the point of
injection over a package-level provider is that a test builds its own
finder over a fake lookup — per-tool overrides with real fallthrough,
so a tart-less machine runs the tart-present goldens and a tart-bearing
one runs the tart-absent ones — and no tool-lookup seam or
process-wide finder memo remains in the product. (The package-level
run-a-version and list-VMs indirections in doctor, prefix and tart's
admission are a different seam — what a tool says, not where it is —
and carry over unchanged.) Six wrappers word their failures over
`tool.Output` (stdout as data, stderr as the story), each keeping its
own grammar and miss: git passes the finder's words through, gh
appends the install hint, upstream and the generators substitute their
sentinels. tart's wrapper stays merged-stream over `tool.Run`, because
its callers parse the transcript after a non-zero exit.

The host tar is one decision too: `tool.Tar` pins /usr/bin/tar,
macOS's libarchive bsdtar, rather than searching PATH. distfile needs
it — a distfile arrives as gzip, bzip2, xz or zip, and reading one
member to stdout across all of those is what bsdtar does and a GNU tar
on PATH (MacPorts' gnutar, a coreutils shadow) cannot — and tart's
staging stream and git's materialized archive, which consume plain
ustar and would work with any tar, use the same one so that there is
one answer to "which tar" rather than three. The tars run inside a
guest are the guest's own and outside this decision. The pin shows
only where no tar can be found, which no golden pins and no Mac
reaches: tart's staging stream and git's materialized archive report
`/usr/bin/tar not found` where they reported a bare `tar` missing from
PATH, and distfile's extractor refuses once, up front, rather than
recording every candidate as not an archive.

**Cost to reverse.** Low.

---

## D12 — Patchfile refresh: the simplest version, and nothing more

**Decided 2026-09-04**, having been held since 2026-09-02 on the
reasoning below, which the ruling turned out to restate. The
maintainer's words: for each hunk, find its entry point in the new
source and see whether the patch still applies verbatim with that
entry point moved up or down; if it is more complicated than that, give
up outright.

**What shipped.** `macports/patch` parses a unified diff and relocates
each hunk iff its before-block — every context and removed line, in
order, byte for byte — occurs exactly once in the target, rewriting only
the `@@` numbers. No fuzz, no whitespace tolerance, no partial
application. Not found, found twice, a target the distfile does not
carry, or a hunk landing on another hunk all give up on the whole patch,
and the whole patch giving up **declines the bump** (`PatchWontRelocate`,
exit 10), naming the patch, the file and the hunk. That is nettle 4.0 —
which cost a VM boot and a baseline install to discover on 2026-09-03 —
caught at `--plan` time: "files/no-fink.patch: configure.ac hunk #1: its
before-block occurs nowhere in the file". A patch whose hunks all moved
rides the plan as a rewritten `files/<name>`; a plan now carries whole
files beside its Portfile edits, and mint, `--diff`, `--in-place` and the
verification shadow all write them. It is not a rider: `--no-riders`
does not touch it, because a bump is not correct without it.

**Two choices made in the building, either reversible in a line.** The
target file is read at exactly `worksrcdir/<path>` with no fallback —
`distfile.Extract`'s basename fallback would relocate a patch against a
nested file `patch(1)` will never open — so a flat tarball
(`extract.mkdir`) declines rather than being read by luck; under "give
up outright" that is the right side. And the patch list is the shadow
evaluation's, not the pre-edit Portfile's, because `patchfiles` may be
conditional on the version just moved to.

**What it does not do, by design.** Recognise an obsolete patch (one
that reverse-applies, meaning upstream merged it) — that is a second
verdict and the ruling asked for one. Model `patch.dir` — such a port's
targets miss and decline, the safe direction. Check anything when the
plan takes a branch that fetches no distfile — there is no target to
check against, and the plan proceeds with the patches untouched and
unmentioned; recorded in `docs/todo.md` as the one gap worth closing.

**Why it was held, and still governs the edges.** The failure mode is
uniquely bad. A patch applied with fuzz in the wrong place yields a port
that builds successfully and is subtly wrong — worse than one that fails
outright, and invisible to every check in the design. Offsets are safe;
fuzz is never accepted unattended. `git apply` may diagnose but must
never render the verdict, since three-way merge succeeds where `patch`
fails. Every "give up" above is that paragraph applied.

**Cost to reverse.** Low — it is additive, and the ruling was for the
simplest version on purpose.

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

**Amended (2026-09-02, the visitor ceremony removed).** No total consumer
ever materialised. The generic-method visitors D1 recorded as the CST's
compile-time exhaustiveness mechanism — SegmentVisitor/VisitSegment and
ItemVisitor/VisitItem in `internal/tcl/syntax` — had zero implementations
and zero callers: every consumer outside the package (the checksum
rewriter, the toolchain bump, the portstyle locator, the vendored
regenerator) walks the tree through the Commands iterator, and the
switches inside the package are kept total by the sealed interfaces and
`gochecksumtype` at CI time. Deleted as dead ceremony. The enforcement
kit is two mechanisms now, not three; D1's list stands as the record of
what was designed, and the cost D1 accepted — CI-time rather than
compile-time exhaustiveness wherever a switch stands in for a visitor —
is simply the whole cost. Reinstating is cheap (a few dozen lines) and
should wait for a consumer that needs a compile-time break rather than
a lint failure.

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

**Amended (2026-09-01, tart is declared the only local provider — for
now).** The verify contract is provider-shaped and the second
assessment rightly noted the abstraction is only partly honoured:
SubmitVerification's degradation gate asks whether the tart executable
exists, exec drives tart directly, and the PR prose says "pristine
tart VM". Ruled: declare rather than abstract, until a second local
provider exists. The declaration is this paragraph; REVISIT when the
GitHub CI provider lands — that is the moment to route availability
through the injected provider, validate capabilities at submission,
and derive the PR prose from Capabilities rather than the provider's
name.

**Amended (2026-09-01, the flag falls; status gains --no-clean).** The
friction ruling completed: invoking promote IS the publication choice,
so an unverified branch promotes with a complaint and no flag — the
reviewer and the user converged on this independently. --no-verify
survives with one meaning only: overriding the refusal of a completed
FAILED build, which is negative evidence, not absence. And status
keeps its reconciliation — settling, releasing, autocleaning — as the
default, with --no-clean withholding just the deletions for callers
that want the merged-PR report without the sweep.

**Amended (2026-09-01, promotion is frictionless and binary).** An
independent review recommended closed evidence states — outstanding
runs blocking promotion, the PR body enumerating every local state —
and explicit unverified consent uniformly. Ruled the other way, on the
tool's own philosophy: dockhand removes friction from the workflow
while ensuring correctness. A promote issued mid-verification IS the
user's answer about the running build — the run is canceled with a
warning (worker released, note recording the cancellation) and the
promotion proceeds without demanding a flag on top. The PR body only
ever says verified or not: local state — deferred, canceled, errored,
running — is the local user's business and never reaches a reviewer.
And a machine without tart degrades dockhand into a bump-and-promote
tool by design, warning rather than gatekeeping. Correctness lives in
what the tool does locally; the PR speaks the one binary a reviewer
needs.

**Amended (2026-09-03, the binary keeps its cause).** The half of the
ruling above that said local state never reaches a reviewer is
withdrawn, because of what it cost in public. "Verified or not" was
implemented as one bool and one fixed sentence — "Not locally
verified: no verification environment on the submitting machine" —
printed whatever the record held. That cause is true of exactly one of
the eight shapes that reach an unverified body, and the sentence went
out over a promotion whose real cause was a neighbour's failed build,
on a machine that had a verification environment and had just been
told not to wait for it. A body that states a cause it did not read is
worse than one that states none. So the binary stands — the header
still says verified or not, and no gate changed — and under it the
body now names why: failed and promoted anyway, blocked before the
change was reached, queued, still running, canceled, superseded,
errored, never asked for, never recorded. What stays local is the
PROSE and not the cause: a build log's words, a worker's name, a
dependency named only inside a detail string. The distinction the
original ruling was reaching for is between a reviewer's business and
a user's diagnostics, and a cause is the reviewer's business — it is
the first thing they would otherwise have to ask for.

The same rule was then applied to the three other claims in the body
that were stated from a literal rather than read from something. The
commit named as the branch head is the tip being pushed and not the
commit the record hangs on: the ledger answers a tip with no note of
its own with a record found over the identical tree at another sha, so
those are two facts and the body says both, with the tree identity that
makes the second evidence for the first. The pristine-VM claim is the
provider's own sentence off the run, because only the provider knows
what its environment guarantees and the phrase would have gone on being
printed over the first backend that proves less. And the three boxes a
verification run answers are printed for a run that reached a verdict,
not for a run RECORD — a promotion that overtook a queued or canceled
run was no more in a position to lint or install than one with no
record at all, and an unchecked box says a step was declined.

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

**Amended (2026-09-02, a deferred submit is a claim).** Two dockhands
sharing a checkout — the status pump and `dockhand verify <branch>`,
or two status passes — both read a run as deferred, both submitted,
and the second RecordRun overwrote the first's job: a worker no note
accounted for, a slot spent twice. Schema 2 has no field to claim a
run with (a peer binary's WriteNote round-trips the struct and drops
what it does not know), so the claim is a lock: every deferred submit
runs under a per-repository submit lock held from a re-read of the
note through the record, and a run the re-read finds no longer
deferred was started or settled by the other claimant and is skipped.
It is a lock of its own, not the notes lock, for two reasons: the
record inside the submit takes the notes lock on a fresh file
description, so a claimant holding it would wait itself out; and a
submit that boots a guest holds its lock for minutes, which no other
note writer should sit through. A claimant that finds the lock held
past a short wait yields with a named refusal (lockfile.ErrHeld,
worded as the expected case — a peer mid-submit — not a hung
process), because the peer is starting the very run it would have.
Lock order is submit → admission and submit → notes, never nested the
other way; the analysis stands above the pump. The lock lives in the
git common dir beside the notes lock, so linked worktrees share it.
This is the parity-safe closure; the schema-3 claim marker (Queued →
Submitting under the notes flock, CAS to Running) replaces it when the
record can carry one.

**Amended (2026-09-02, the shell asks the kernel for its terminal).**
`dockhand shell` requests a TTY from `tart exec` (-t) only when stdin
is one, because -t on anything else dies on the terminal-size ioctl.
The check was the file mode — os.ModeCharDevice — which is the wrong
question: /dev/null is a character device too, so a shell fed from it
asked for a terminal and died. The check is now the kernel's own
answer (`tool.IsTerminal`, a termios ioctl — TIOCGETA on darwin,
TCGETS on linux, no on anything else — through the stdlib syscall
package, no new module). The observable change is the intended one: a
redirected stdin no longer passes -t, and the shell runs without a
terminal instead of failing. The shell also resolves tart through the
run's finder like every other tart site, rather than naming it bare
for os/exec to search. The pager decision on stdout in the diff
realization keeps its file-mode check; it was not the bug, and it is
not this ruling.

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
open PR's title and body. Amended (2026-09-03, ruling 15): the one
meaning was two, and the subsumption is what proved it — replacing a
standing branch and re-deriving a port at a version that did not move
are different acts on different objects, and a user who wanted the
second was buying the first. `--force` is retired from the intent
verbs and split: `--replace` is the in-flight policy, on one target,
and `--recheck` is bump's re-derivation parameter, which additionally
sets the run's from-source flag, because an archive matching an
unmoved version predates the change and a pass earned by unpacking it
proves nothing about the distfile just fetched. `promote --force`
keeps its name — it is git's own word for the force-push-with-lease it
performs, and it moves a branch dockhand published rather than
destroying one it minted. And `status` never auto-discards — amended (2026-08-31):
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

## D23 — Verdicts blame precisely: a witness that abstains has not disagreed, and a neighbor's breakage is not the change's

**Decision.** Two field-measured refinements to how dockhand assigns
blame, one in upstream resolution and one in verification verdicts.

**Upstream: corroborate before declaring livecheck ahead.** The
releases API is authoritative about which tags upstream blessed as
releases — but an upstream that tags without cutting a release (the
gopass satellite repos) has abstained, not disagreed. When livecheck
outruns the authoritative releases, the tag list is fetched as a
second witness: a tag vercmp-equal to livecheck's answer resolves the
report (TagWithoutRelease — the maintainer's declared policy and
upstream's own ref agree), and its absence hardens the decline. The
same sweep fixed the LivecheckAhead message, which claimed "newer
than any forge tag" when no tag had been read. Sibling ruling, same
commit series: a release supersedes its own prerelease
(PrereleaseSuperseded) — semver precedence lives in the judgment,
never in VerCmp, which stays the pure port of base's vercomp.c.

**Verification: a dependency's failure blocks, it does not fail.** A
failure log whose first Error line names a DIFFERENT port than the
one under test is a neighbor breaking before the change was reached
(gomuks blamed for olm, a nomaintainer port it never touched). That
records as a distinct state, blocked: untested, not disproven. It
sits on the unverified side of the promote gate — promotes with the
dependency named in the complaint, no --no-verify demanded, PR reads
simply as not verified — and its worker is RELEASED, unlike a real
failure's: the kept environment exists to debug one's own breakage,
and slots are machine-scarce. The detail annotates a nomaintainer
dependency when the tree proves it, because "no one to nudge" changes
what the maintainer does next. Detection is conservative like the
unsupported reclassification: no extractable port name, or the port's
own name, stays failed.

**Cost to reverse.** Low. Both are additive branches on existing
verdict paths; removing either restores the prior blame.

## D24 — The dependents are best effort, on both roads; an outcome is about the port, not the machine

**Decision.** Five rulings from the cohort live check of 2026-09-03/04,
made against a real ports tree and recorded here because until now
they lived in commit messages and code comments. Each amends something
above it, and the amendments are named.

**Dependent revision bumps are best effort.** A dependent is owed a
revision bump because the library it links moved; whether it builds
today is usually a fact about the dependent. So the promote gate asks
the HEADLINE for a pass and no failure, and asks each dependent only
for an outcome: a dependent that failed, was blocked, was withheld, or
declined the platform is published over, named to the author on stderr
before the pull request exists, and named to the reviewer in the body
where the bump is claimed. gthumb, unmaintained and already broken on
Tahoe for reasons unrelated to libraw, is the case that decided it — a
gate that held the whole change for it would make every cohort hostage
to its least maintained member. Some run somewhere must still have
passed: a change nothing ever built has no evidence to publish on.

This holds on the unattended road as well. The plan's Part 6 and S14's
delivery line said the machine publishes on "positive evidence only";
that is amended. A machine publishes a cohort whose dependent failed on
the same evidence a person would, because the alternative — the machine
refusing the ordinary cohort, since most carry a member that does not
build — would spend the whole road on the least maintained port in
every set. What the machine cannot do is be told; so the body is where
that road's honesty lives, and a verified body never deletes a failure
(the suppression that keeps a promotion's own cancellations local was
taking failures with it, measured live, and no longer does).

**An outcome is about the port.** Best effort does not reach errored,
canceled or superseded. The argument for best effort is that a
dependent's build is a fact about the dependent, and those three are
facts about something else — the machine's silence, a person's "no",
the branch moving. A dependent in one of those states is unanswered,
and the change waits. `RunState.Outcome` names the distinction once.

**The evidence is read before anything is cancelled.** `promote`
without waiting cancels the running builds; it used to cancel first and
judge second, so the gate judged canceled runs its own promotion had
just written. With canceled excluded from outcomes that would merely
have refused, but the ordering was wrong on its own terms, and it now
reads the record, decides, and only then cancels. The body names what
was true at the decision — "still running when this was promoted" —
rather than what the verb then did. D17's amendment of 2026-09-03 said
"no gate changed"; as of this entry, it has.

**The cohort cap is off.** Ruling 2 set it at ten to twelve; the code
had eight; it is zero. The proposal names every dependent, the count is
stated, `bump-revision --for` is the deliberate act of accepting them,
and `--exclude=a,b` is how a person takes some and not the rest — an
excluded port is out of the change entirely, listed among the ports
examined and not bumped, where a reviewer can disagree. A number in a
source file could not know which eight the maintainer wanted. Part 11's
risk register names the cap as the capacity mitigation; what mitigates
capacity now is consent and the exclusion list, and the exposure the
cap was quietly limiting — a cohort that breaks early leaves the
members behind it unbuilt — is the runner's stop-at-first-failure,
recorded in `docs/todo.md` as its own problem.

**Two members MacPorts will not activate together are not built
together.** A cohort member declaring `conflicts` with a member already
seated is bumped by the commit and left out of the guest alone, under a
new run state, `withheld`: this build held the subject back, and
nothing about the subject is the reason. The seat goes to the name not
ending in `-devel`, else to whichever is earlier in build order — the
suffix is a stated convention that settles the case that arrives, a
stable port and its development twin, and decides about a fifth of the
tree's conflict pairs on its own. A withheld member counts as answered
for the gate, and its line is always on the body. Two of the two live
cohorts examined carried such a pair, so this is the ordinary case.

**`_resources` is named, not pattern-matched.** The tree's own
infrastructure directory is skipped as a cohort subject by name, and
the leading underscore is not taken as a rule: it would be a guess at
a convention MacPorts has never stated, and `_resources` is a fact
about the tree today. It is staged into every overlay, because a port
served from a tree without it can reach no binary archive — MacPorts
resolves archive sites under the port's own tree with the fallback
disabled — which is why the ABI baseline could not run at all before
this series.

**Pending, not ruled.** `status` is to become a verb that changes
nothing anyone else can see, with settling kept (reconciling the
workers into the ledger is reading the world) and a `cycle` verb taking
retire, the pump and the publish slot, with one flag per thing it can
keep rather than a universal `--keep`. Discussed and recorded in
`docs/todo.md`; to be taken up as a conversation.

## D25 — A member behind a failed prerequisite is blocked, and the judge trusts the guest's state files

**Decision.** Two rulings made ahead of the runner change they
unblock. The cohort runner today breaks its loop at the first failing
member, and every member behind it is blamed on that failure whether
or not it depends on it — `mise`, unrelated to `oniguruma6`, came back
"oniguruma6 fails to build; this member is untested". The fix is for
the runner to continue, skipping only members whose prerequisites
failed; `Dependent.Requires` already carries the graph. That fix
cannot be designed without these two answers.

**A member skipped because its prerequisite failed is `blocked`.** The
state already means "something failed before this subject was reached
— a dependency, or an earlier member of the cohort; untested, not
disproven", and after the fix that is exactly and only what it says:
the sibling sentence names a member this one really depends on.
`withheld` is not this. It means the build held the subject back and
nothing about the subject was the reason, and a member whose own
dependency broke has a reason about itself. Keeping the two apart is
what lets a reader tell "the tool chose not to" from "it could not
have".

**The judge may trust the per-member `state.<i>` files.** The guest
writes two kinds of evidence: the log, written by MacPorts while
building the change under test, and one state file per member, written
by dockhand's own runner script. The judge has read the log and held
the state files as corroboration it had not decided to rely on, because
in principle a Portfile could write anything into the guest — including
a forged state file. It relies on them now. The threat is a maintainer's
own Portfile lying to the maintainer's own tool about the maintainer's
own bump, which is self-deception and not an attack, and dockhand does
not engineer against it. What the state files buy is the one
distinction a continuing runner needs and the log cannot give: a member
skipped for a failed prerequisite writes no log marker, and neither
does a member the runner never reached because it died; the state file
is what says which.

**What this costs when the runner change lands.** `verdict/cohort.go`'s
blame is built around one `Stopper` and one `Culprit` — exactly one
member that stopped the run. After the fix there may be several
failures and several skips, each blamed on its own prerequisite, and
the seven cohort corpus fixtures, all two-member pairs with a real
edge, will need siblings with independent members.

## D26 — The human workflow first; the audit row says what a promotion carried

**Decision.** Two rulings, one about priority and one about evidence,
made together because the second is what keeps the first honest later.

**Machine publication is deprioritized.** The overhaul's last step was
written to end at the R3 flip — the machine publishing unattended once
the trust ladder's numbers arrived. That is not where the work goes
now. What comes first is the human workflow, fully fleshed out: bump,
verify, the cohort, promote, and the report a person reads between
them, each complete and each honest about what it did. The machine
road stays as built and stays off at build time; its gate follows the
human gate by D24, and nothing about it is wrong. It is simply not the
thing to finish next. The ladder's bookkeeping — the queries that
compute each rung's numbers, the R3 flip itself — joins the providers
and the upstream sources in a roadmap dated after the overhaul is
declared done.

Concretely, the last step is what the live check of 2026-09-03/04
found wanting on the human road, most of it already recorded in
`docs/todo.md`: `status` reporting and a `cycle` verb acting; the
runner continuing past a failure that other members do not depend on;
a failed member saying for itself, in the body's member list, why it
carries no proof; a way to read the body without publishing it; the
stale primary and the tracked-upstream inference; the stranger named
as a later member's dependency; the option to force a withheld member
to build; `verdict.Weigh`, which nothing calls; and a bound on the body
now that the cap is off.

**The audit row records what a promotion carried.** Every promotion
appends a row to `refs/notes/dockhand/outcome`, and until now the row
said only whether the change was verified. With the dependents best
effort, a verified change can carry members that failed, were blocked,
or were withheld — and the trust ladder's entry evidence, when it is
eventually computed, must be able to tell that population from the one
where everything built, because their complaint rates may differ and a
number reaching fifty across a population that changed midway would
tell an auditor of the flip nothing. So the row carries `unproven`: how
many members were published without a pass, zero and omitted where
everything built. It is one field written at promote time from the
same reading the author is shown on stderr, so the two cannot
disagree. The promotions already recorded count toward the ladder —
the ladder measures what reviewers decided about real pull requests,
and the gate that admitted each does not change what the reviewer
did — and from this row on, which gate that was is on the record.

## D27 — `status` observes and settles; `cycle` acts

**Decision.** The mandate of `status` is reversed, and a new verb takes
what it gives up. Ruled across a conversation on 2026-09-04, point by
point, and recorded here whole.

**`status` makes no change anybody else can see.** It reads the
branches, polls the workers, writes what they said into the ledger,
releases a guest whose verdict says so, and renders. That is the whole
of it. Settling stays, and stays on purpose: it is the one write that
makes the report truthful — every other write changes the world, settle
changes the report to match a world that already changed — and a
`status` that showed "verifying" over a guest that finished an hour ago
would be a worse lie than the write it avoided. The ledger is
dockhand's own account; a branch, a pull request and a VM are the
world. For the cases that want the ledger and nothing else — a watch
loop that must not take locks, a script — `status --no-update` polls
nothing and writes nothing. Where work is waiting, `status` says so and
names `cycle`, because with the split nothing begins on its own.

**`cycle` does what acts on the world.** It retires the branches of
merged pull requests, locally and off the fork; drains queued work,
which boots guests; runs the publish slot; and, asked to, reclaims
untracked workers. `clean` folds into it — `cycle` is `clean` plus the
rest — and `clean`'s `--superseded` comes with it. Each thing `cycle`
removes has its own flag, and the flag's shape follows the default:
what happens unless withheld gets `--keep-<x>` (`--keep-merged`); what
happens only when asked gets a plain flag (`--superseded`,
`--reclaim-orphans`). A universal `--keep` would have been withholding
for one and meaningless for the other, and hard to aim.

**A passing run's environment is kept by the person who started it.**
`--keep-env` on `verify` and the `bump` family, recorded on the run and
honoured wherever release happens. Not a flag on `status` or `cycle`:
by the time either settles, the release is in the same pass and nothing
could intervene, and the person who wants to look inside a green build
knows it when they submit. The failure path keeps by rule; this keeps
by request.

**What it costs.** `status --no-clean` is dropped, having withheld a
deletion `status` no longer performs. Every `status` golden that shows
a retirement or a drain line belongs to `cycle`. A band `status` could
exit on because a write failed is `cycle`'s now.
