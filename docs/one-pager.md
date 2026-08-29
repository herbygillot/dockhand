# dockhand

*A port maintenance utility for MacPorts.*

dockhand takes a port and a desired end state and produces a minimal, reviewable diff — or refuses, and says exactly why. Everything else it does is built on that one operation.

---

## Why

Maintaining ports is mechanical labour punctuated by judgement. The judgement is the interesting part and belongs to the maintainer. The labour — reading upstream releases, rewriting version strings, recomputing checksums, regenerating vendored dependency blocks, building in something resembling a clean environment, opening the PR, watching CI, doing it again next month across forty ports — is not.

Parts of that are nominally served — `port bump` updates literal versions and checksums — but nothing reaches the places the hours actually go: Portfiles whose dependency blocks must be regenerated wholesale (`cargo.crates`, `go.vendors`, Python wheel sets), patchfiles that quietly stop applying the moment upstream moves *(tentative — see below)*, and the long tail of shepherding many ports across many weeks and one red CI matrix at a time. And nothing at all tells you whether the edit you just made means what you think it means, short of waiting for a build to disagree with you.

dockhand aims at the mechanical work, at fleet scale, without ever pretending to have done the judgement.

## How it works

Given an intent — *bump this port to that version* — dockhand:

1. **Reads authoritatively.** Portfiles are evaluated through MacPorts' own Tcl via `port-tclsh`, never through dockhand's own idea of what the file means, because `version` is frequently computed rather than literal. Reads are variant-relative by construction. The distinction between *parsing* a Portfile and *evaluating* one is load-bearing; see **Reading Tcl** below.

2. **Classifies before working.** Every edit lands on a five-rung tractability ladder, from literal substitution (T0) through regenerated dependency blocks (T3) to work that genuinely needs a human (T4). Classification costs one evaluation, so discovering that a port is intractable is cheap — cheap enough to sweep the entire tree and find out empirically which idioms dockhand handles. *(Superseded in part: this holds for static tractability only. Deciding whether a **particular** bump will work needs the target version and a read of the upstream tree, and is not sweepable in the same sense — see `cli.md`.)*

3. **Writes as text.** Edits are byte-span replacements. Bytes outside the span are identical before and after — no reflowing, no reindenting, no comment normalisation. The output is a diff a person will review, and diff minimality is a hard, testable invariant.

4. **Delegates the hard blocks.** `go2port`, `cargo2port` and their peers already do this work and are already what maintainers trust. dockhand owns the block boundary and knows nothing about what goes inside it, which means a future PortGroup that abandons today's vendoring scheme costs an adapter rather than a rewrite. These tools are *discovered*, never depended upon — see **Implementation**.

5. **Verifies three separate propositions, and never confuses them.**

    - *Did the edit do what I meant?* — evaluate, edit, evaluate again, and assert that exactly the intended fields moved. Nearly free, and the only check whose subject is dockhand itself.
    - *Does the port still work?* — answered by a build, and only by a build.
    - *Are its dependencies actually declared?* — answered only by a build in a **pristine** environment, which is what the VM executor exists for.

    These are independent, not ranked. A faithful edit can produce a broken port; a wrong edit can produce one that builds fine against a cached distfile. The disagreements carry the information: a failed fidelity check alongside a passing build means **dockhand** is broken, and should halt the batch rather than fail the port.

    Cheaper rungs sit in between. Checksums resolve at fetch time and patch application resolves at extract time — both long before anything compiles, and both cheap enough to run across a whole sweep.

6. **Publishes only what the evidence supports.** Autonomy is a function of tier and verdict, not a flag. Clean low-tier changes on ports you maintain can open their own PRs; everything else parks for batch review. Every PR states what was changed, what was verified and where — and what was not checked.

## Reading Tcl

Portfiles are Tcl, and there are three distinct depths at which a tool can claim to understand them. dockhand takes the first two and refuses the third.

**A syntax parser — yes.** Tcl's grammar is twelve rules. A parser that correctly handles word boundaries, brace nesting, bracket substitution, quoting, line continuations and comments is bounded work, and it is what makes a recognizer's span principled rather than a pattern that happened to match. Given that the product thesis is *refuse rather than guess*, the parser earns its place. It is also, usefully, a pure function — bytes in, spans out — with an immediate 20,033-Portfile test corpus sitting in the ports tree.

*(Refined: the artifact is a four-level CST — Script/Commands/Words/Segments with spans, brace bodies opaque behind script and list lenses — which for Tcl is complete, since control flow is commands, not grammar. See `reading.md` and D14. That document also records where the version actually lives across the tree: only 39% of ports carry it in a `version` line.)*

**A constrained evaluator over a tiny safe subset — cautiously.** `set`, string interpolation, simple concatenation: enough to resolve `set major 3` / `version ${major}.2` and work out which literal a T2 bump should touch. Strictly a locator hint. It may inform *where* to edit; it may never state *what the metadata is*. The authoritative re-read checks its work afterwards, so a wrong guess fails safe.

**A Tcl interpreter — never.** Not chiefly because it is hard, though evaluating a real Portfile means implementing the MacPorts port API and executing PortGroups that are themselves substantial arbitrary Tcl programs. The decisive reason is that it would collapse the verification model. The re-read is evidence *only* because the oracle is not dockhand. Read with your own interpreter and "my edit did what I intended" degrades into "my interpreter agrees with my interpreter" — a statement that remains true precisely when the interpreter is wrong. That trades away dockhand's only check on its own correctness in exchange for the cost of a subprocess.

If that subprocess cost matters at sweep scale, the answer is a persistent `port-tclsh` helper — one long-lived process per run, speaking JSON over a pipe — not an interpreter.

## Implementation

*(Superseded: the language decision was re-litigated twice and settled as **Go** — see `decisions.md` D1 for the full trajectory and reasoning. The paragraphs below record the original argument.)*

**Rust**, chosen primarily on reach. Go's macOS floor currently sits at Monterey and rises to Ventura in 1.27, moving forward roughly annually under a published deprecation policy. Rust's floor is Sierra on Intel and Big Sur on Apple Silicon, and has held since 2023. dockhand's constituency is disproportionately people running Macs that Apple itself has stopped supporting, so an annually-rising floor is a slow subtraction from exactly the audience the tool exists to serve.

Two secondary factors point the same way. The design is saturated with sum types — tiers, regions, finding kinds, verification propositions, operation kinds — and compiler-enforced exhaustive matching is directly aligned with a tool whose value lies in declining unfamiliar cases. And Go's principal structural advantage does not apply here: parallelism is capped at one or two by `/opt/local` being shared mutable state and by Apple's limit on concurrent macOS guests, so there is no concurrency story to speak of and no async machinery to carry.

**External tooling falls into three tiers.**

*Required.* `port-tclsh` and `git`. Neither is optional in any meaningful sense: the first is the authoritative reader, and the second underpins worktree isolation, upstream ref discovery and the entire branch-and-PR path. `git` is therefore declared as a real dependency in dockhand's own Portfile, in `bin:` form so that an existing Xcode command-line-tools git satisfies it rather than forcing a redundant install. Because a dependency declaration cannot express a version floor, the runtime probe enforces one — `git worktree` requires 2.5 or newer, and old macOS carries old git.

*Assumed.* `patch` ships with macOS, which is the assumption MacPorts base itself makes, so dockhand makes it too. Two constraints follow. dockhand consumes only its exit status, never its message formatting, since the ancient GNU patch Apple ships and a modern `gpatch` do not phrase failures alike. And the verdict must come from whichever patch engine MacPorts will actually use *for that port* — one that build-depends on `gpatch` has to be tested with `gpatch`. `git apply` may explain a failure or propose a rebase, but must never render the verdict: its three-way merge succeeds in cases where `patch` does not, which would manufacture false greens.

*Discovered.* `tart` and the block generators are resolved from `PATH` at startup, and what is present determines which tiers and which verifications are reachable on this machine. That resolution is reported rather than hidden: *T0–T2 available; T3 unavailable, no `cargo2port`; pristine verification unavailable, no `tart`*. Depending on these instead would inherit each one's own platform floor, gating the large majority of dockhand's work on tooling that a minority of ports require. When something genuinely needed is absent, dockhand stops and says so — at plan time, before a batch begins, rather than forty minutes into it. A missing tool is a fact about the machine, never a finding about the port.

## Principles

- **Read as Tcl, write as text.** Never round-trip through an AST.
- **The oracle is never dockhand.** Any claim of correctness must rest on something the tool does not itself control.
- **Refusal is a feature.** A decline costs five minutes; a confident wrong edit ships corruption.
- **Own the edit; never own the semantics.** Duplicating what `port bump` does is fine, and probably necessary — an edit dockhand can verify is worth more than one it merely delegated. Reimplementing dependency resolution, build phases, the sandbox or checksum semantics is not: MacPorts defines those, and a dockhand that disagrees with base is simply wrong. Keep the coupling that remains confined to the Tcl shim and the recognizer set, so base drift lands in a small blast radius.
- **Prefer state you can delete.** GitHub and the ports tree hold the truth; anything persisted locally is a cache that `rm -rf` should cost only time.
- **CI is the authority.** Local verification is a fast filter that saves you a public red X, never a substitute for the build matrix.
- **Never outrun review capacity.** Any maintainer may run dockhand against any port; the PR *is* the notification, and that is how the tree already works. Ownership sets the gate, not the capability — your own ports may open PRs unattended, others' never do until you have read the diff yourself.

## Non-goals

- Not a replacement for `port`, and not a fork of macports-base.
- Not a Tcl implementation.
- Not a Portfile formatter, linter, or style enforcer.
- Not a substitute for CI, and not a claim to coverage it cannot have — older macOS versions and other architectures stay out of reach.
- Not an interpreter of vendored dependency formats.
- Not a bundle — optional external tools are discovered on `PATH`, never vendored, and never required in order to install dockhand.
- Not a mass-submission tool — unattended PRs stay reserved for ports you maintain.

## Under consideration

**Patchfile refresh.** When a port moves forward, its `patchfiles` are the first thing to rot, and repairing them by hand across a tree is a large recurring cost. dockhand will never author patch content — rewriting hunks is judgement. But three things below that line look mechanical, and all of them resolve at extract time, before anything compiles:

- Detecting staleness with a dry-run apply, using whichever patch engine MacPorts will use for that port.
- Distinguishing a patch that is *broken* from one that has become *obsolete* — if it reverse-applies cleanly, upstream has merged the fix and the correct edit is to delete the file and its `patchfiles` entry, which is a change dockhand can make and verify.
- Reducing the remainder to a diagnostic — which hunks failed, against what context now — so a blocker becomes a five-minute task rather than an afternoon.

Held tentative because the failure mode here is uniquely bad. A patch that applies with fuzz in the wrong place yields a port that builds successfully and is subtly wrong — worse than one that fails outright, and invisible to every check in this design. Offsets are safe; fuzz is never accepted unattended.

---

A dockhand does the loading and unloading. The decisions about the cargo belong to someone else.
