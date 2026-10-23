<!-- The testing thread's proposal, copied whole from the project's files on 2026-10-23 at the person's question about a Portfile corpus. The roadmap's entry, "A corpus of real Portfiles replays update's plan path", coalesces it; where they differ, the roadmap's decides: its oracle replays each exemplar's last version bump through update's own path, and its Go and Rust half goes before rc5's tag. -->

# A Portfile corpus for update's plan path (proposal, 2026-10-23)

Read from herbygillot/dockhand main at 52d14a6c, and macports-ports at 4cf40280.

## What the suite has today

There's no corpus of real Portfiles, and no golden plans. Real Portfiles reach dockhand's tests in four places, and none of them reaches the code where v0.5.0's regressions were:

| Layer | What it runs on | Runs where | Reaches update's dependency plan? |
| --- | --- | --- | --- |
| Unit tests | Portfiles written inline in Go tests. PortGroups are hand-written stand-ins: the evaluator's `golang-1.0.tcl` is three lines (`eval/evaluator_test.go:444`). 52d14a6c's go.vendors test models golang 1.0's `extract.rename` with a `proc` of its own. | `make test`, CI | Yes, but only against our model of each PortGroup |
| Live evaluator tests | 13 real ports (the php and tesseract families, 11 compiler-query ports), with `DOCKHAND_TEST_PORTS_TREE` set | Only when someone sets it; CI doesn't. CI's MacPorts is installed `nosync`, with no ports tree. | No: evaluation only |
| CI's Intel job | One real port: `update jq --new --plan` on a shallow clone | CI | jq has no dependency block |
| `tools/survey` | Every port in a pinned tree | By hand, about 72 minutes a build on the main Mac | No. `Assess` stops before archives and generators, and reports `regeneration: archives-required` as NotTested (`portedit/assessment.go:104`). |

`testdata/` holds only livecheck responses and license and packaging vectors.

All four regressions were in update's dependency step, which no layer runs against a real PortGroup:

- rc1: Rust `dist_subdir`. The block step didn't predict what cargo's PortGroup sets.
- rc4: go.vendors. `WritesChecksums` refused golang 1.0's file-alone appends, and the block step missed `extract.rename` and `extract.cmd`. This hit all 172 go.vendors ports.
- direnv: a `/vN` module path (31 ports in the tree have one).
- opkssh: the 16 MiB RPC frame limit, hit when a big regenerated block is evaluated again.

## Where the gap really is

"We need more fixtures" is not the right diagnosis. The suite tests dockhand against what we believe each PortGroup does, and those beliefs went stale. A corpus of hand-written Portfiles would repeat the mistake. The corpus has to contain MacPorts' own PortGroup files and real Portfiles, at a pinned commit, with an oracle that doesn't depend on what we believe.

## Proposal

1. **Vendor a small, pinned slice of the tree** into `testdata/corpus/`: the PortGroup files the exemplars use (golang, cargo, cargo_fetch, rust, github, python, perl5, php, and the rest they pull in), plus about 30 exemplar port directories. Record the macports-ports commit in a `COMMIT` file. A `tools/corpus refresh <commit>` command rewrites the slice. Each refresh is then a diff we review, so an upstream PortGroup change shows up in our repo instead of in the field.

2. **Pick exemplars by shape, one row each with the reason:**
   - golang with go.vendors: a small port, a large one (caddy, 913 lines), one with checksum and size rows (git-credential-oauth), a `/vN` module path (direnv), and opkssh (frame size).
   - cargo with cargo.crates: a small port, a large one, one with git crates, and one with `cargo.offline_cmd` or a feature patch. Also the rust PortGroup port from rc1.
   - Shapes other than dependency blocks: the five-port replay set (terraform, py-lmdb, qemu, perl5, terraform's obsolete stub), a large subport family, a livecheck override, and a port with no fetch.
   - A standing rule: **every bug found in the field or in acceptance adds its port to the corpus** in the same commit that fixes it.

3. **Use a self-checking oracle, not golden plans.** For each dependency-block exemplar, plan an update to the version already committed, and expect the Portfile to come back unchanged. That means the same dependency block, the same checksums and the same version lines. Any difference is a finding. This needs no golden files to maintain, and it would have caught all four regressions: each was a refusal or a crash on the port's real shape, whatever the version. For the other exemplars, check only a recorded outcome and refusal code per port (`plans`, or `refuses: <code>`), so a change in wording doesn't churn the corpus.

   `update` itself refuses a port that is already current (`engine/update.go:78`). The harness would call portedit directly, the way `tools/survey` does, through a new entry point that plans at the committed release.

4. **Fetch inputs once and cache them.** Regeneration needs each version's source archive and go2port and cargo2port. Fetch from distfiles.macports.org, checking against the Portfile's own checksums, into a cache keyed by checksum. The first run needs the network, and later runs don't. Keep it out of `make test` as `make test-corpus`, run it in CI's macOS job (which already has MacPorts), and run it before every field-testing push from main.

5. **Retire the PortGroup stand-ins where the corpus replaces them.** Once the editprep and portedit tests can load the real `golang-1.0.tcl` and `cargo-1.0.tcl` from the slice, the hand-modelled procs go away (for example, 52d14a6c's `fixture_extract`).

## Cost and timing

- Building it: a tools/ command for refresh and replay, one portedit entry point, the slice, and a row table. That's about a part of a batch.
- Running time is unmeasured. The goal is under 5 minutes with a warm cache, so it can run on every push. The first measurement decides whether it runs on every push or only on main.
- It doesn't replace `tools/survey`, which still covers the whole tree when the evaluator or the version probe changes. The corpus covers update's plan path in depth.
- It shouldn't hold rc5. Suggested placement: the first item after the v0.5.0 tag, since every later field-testing batch leans on the go and cargo paths.

## Open questions for prioritization

- Should the slice be vendored (hermetic, reviewable diffs, about 1 to 2 MB) or sparse-checked-out at test time (smaller repo, needs the network)? I recommend vendoring, because a refresh that shows up as a diff is the point.
- Should the oracle use the committed version only, or also replay one recorded past bump per exemplar, from version N−1 to N, checked against the commit that made it? That bump would test version edits too, but it needs two archives per port.
