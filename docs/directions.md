# Directions

Where dockhand could go past its [roadmap](roadmap.md), proposed 2026-10-01 at the person's asking, from what the dogfood runs of September and October showed. Nothing here is decided: each becomes roadmap work at the person's word. What the roadmap already holds, as running what a port installs, the prefix provider, and expiring credentials, isn't repeated, nor what's been decided against, as MacPorts' buildbot history as a baseline (D1's decision 20).

## Areas to grow into

1. **Security advisories.** OSV.dev answers, through a documented API, for Go modules, crates, and PyPI packages. A Cargo or Go port vendors hundreds of pinned dependencies (`cargo.crates`, `go.vendors`) that no one audits today. dockhand could say a port, or a dependency it vendors, with a known advisory, and whether the newest release fixes it; serve could take security updates first.
2. **A library's interface changing, found rather than guessed.** `update --revbump-dependents` exists, but whether dependents need their revision bumped is the person's call. The guest installs the port; comparing what its libraries declare, their install names and compatibility versions, before and after (`otool -D`, `otool -L`), says when an update changes what dependents link. It inspects the destroot and runs nothing the port installs, so it stays clear of that Later item.
3. **After the merge.** status records a branch merged and stops there. MacPorts' buildbots then build the port on every release; dockhand could say when a merged update of yours fails on one, as a notice that starts a fix, never as a baseline.
4. **Trac's tickets.** The pull request's "referenced existing tickets" item is the person's word today. Trac's query exports CSV, a documented format: `update` and `submit` could name a port's open tickets and offer the `Closes:` line, and serve could say a breakage reported against a port you maintain.
5. **A review queue.** `review` assesses one pull request. Others' pull requests touching ports you maintain, with their age and the 72-hour maintainer timeout's clock, would complete a maintainer's side.
6. **Upstream's notes, carried to the reviewer.** The comparison already reads both releases' source; the changelog's section between them, or the forge's release notes, under the pull request's Description, is what a reviewer otherwise looks up.

## Features

- **`dockhand doctor`:** the environment's health in one place: sign-in, Tart's images and the disk under them, the index cache, logs (780 MB on 2026-10-01), branches from before v3, stale worktrees, the database's size, and the limits in force ([limits](limits.md)). Half of what the runs of these two days found would have shown there.
- **`dockhand why <check>`:** one answer for a failure: the step that failed, the cause read from that step's part of the log, whether master fails the same way, and whether it was only advisory. rust's "tests failed (advisory)", an environment's permission error before any test ran, quoted lint's line instead ([#35084](https://github.com/macports/macports-ports/pull/35084)).
- **Branch notes:** a person's note kept with a branch, shown in status, and carried into the pull request; this subsumes `submit --note` (batch 24) and says why a branch is parked.

## What to redesign

1. **Say what was checked, not only what failed.** The strongest pattern in the runs' findings: an assessment reports problems, so silence can't be told from not looking.
   - rust's patches and its Cargo manifests, both read and both silent (batch 23);
   - an assessment reused from before a fix, standing silently until `-v` learned to say so (the rust and cargo run);
   - fluent-bit's six patches, which applied without a word, so the person dry-ran them by hand;
   - trivy's "go.mod requires Go 1.27.0, which go.toolchain_min 1.27.0 already gates on", while a Go 1.26 pin contradicted it (batch 27).

   An assessment built around a coverage ledger, each rule saying what it read and what it found, compact by default as `✓` lines and whole in JSON, would make the batch-by-batch fixes one rule.
2. **Edit the declaration that ran.** `update` assumes one literal declaration of what it edits, and each port that breaks the assumption has had a fix or a refusal of its own: cargo's two `cargo.crates` lists, one per platform (batch 24); rust-src's own distfiles; py-flatbuffers' checksums shared across its family; git's checksums in a variant (batch 5); trivy's Go pin. Asking the evaluator which declarations ran for this platform and variant, and editing within that branch, with a pinned toolchain part of what an edit reads, would turn the list of refusals into one capability.
3. **One readiness verdict.** `tidy` said "nothing to tidy" where `submit` warned of a body line over 72 characters (the rust and cargo run), and status and submit have disagreed before. One evaluation of whether a branch is ready, which every command renders, each finding naming the command that fixes it, would end the disagreements; tidy would then offer the message to rewrite.
4. **A refusal is a type, not a style.** "Shared-source preparation is required", a "fidelity:" prefix said twice, and refusals with no next step (the rust and cargo run) come from free-form error text. A refusal type that carries a person's sentence and its `Next:`, with a test that every refusal `update` and `submit` make has one, would keep them so.
5. **A pull request part the person owns.** dockhand writes the whole body and nothing edits it by hand, so a person can say nothing in it. A section dockhand keeps but never writes, filled from branch notes, would let them.

## Suggested order

The first two redesigns first: most of the runs' recent findings trace to them, and the batches they'd fold together (23, 24, 27) are next anyway. Then `why` and `doctor`, cheap and felt daily. Of the areas, security advisories and the follow-up after merge add the most for a maintainer.
