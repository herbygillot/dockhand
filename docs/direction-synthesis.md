# Dockhand's direction: one synthesis

Written 2026-10-02 against `main` at dd1fe11a, and accepted by the person the same day (see [Decided](#decided)). It merges four proposals that were written separately:

- **[directions](directions.md)**: Claude's, from the dogfood runs: six areas, three features, five redesigns.
- **[codex-directions](codex-directions.md)**: Codex's, which deliberately ignored the roadmap: seven areas and two experiments.
- **[The command UX review](reviews/2026-10-02-command-ux.md)**: agreed by the person, then parked.
- **[The maintainer review survey](https://github.com/herbygillot/dockhand/blob/claude/project-thread-skk316/docs/review-survey.md)**: what reviewers ask for on 140 merged PRs. It's on its own branch, not on main.

This document sets the order and the frame. Each item still becomes roadmap work at the person's word, as before.

## Already done, so dropped

Batches 37 to 46 landed today, and they take these off the table:

- **Expiring credentials** (roadmap Later) is done: the login renews itself (batches 44 to 46).
- **`why`'s first piece** is done: a failed Tart check now names the failing command and what the tool said (batch 43).
- **trivy's false "already gates on"**, one of redesign 1's four examples, is fixed (batch 42). The pattern behind it isn't.
- **Subports** now resolve as their own ports (batch 40), which UX §1's `-p` relies on.

## The reframing: three layers, built bottom-up

Read together, the four proposals aren't a list of features. They form three layers, and each layer depends on the one below it:

1. **What dockhand knows and says** about a branch: what it checked, what it found, whether the branch is ready, and what to do next.
2. **How a person drives it**: naming branches, chaining steps, and the words commands use.
3. **What it reaches**: new things to know about, such as consumers, advisories, the review queue and patches.

The main pushback is on order. The UX review's biggest change, "stop only for judgment" (§3), means chaining steps automatically. That's only safe once layer 1 can say, in one place, which findings hold, which need judgment, and which are ceremony. Built first, chaining would hard-code today's scattered refusals, which already disagree with each other (tidy and submit on the 72-character line; rebase suggesting `submit` on a merged branch).

## Layer 1: one model of a branch's state

These six pieces turn out to be one design: **ledger → decisions → readiness for an action → next step**.

| Piece | From | Its role in the model |
| --- | --- | --- |
| Coverage ledger: each rule says what it read and what it found | directions R1 | the input: findings, *including* "checked, clean" |
| A refusal is a type with a reason and a `Next:` | directions R4 | each finding's kind: **protective**, **needs judgment**, or **ceremony** (UX §3's taxonomy) |
| How loud a mark is vs whether it holds automation | UX §10 | two fields on a finding: severity (`✗ ! ·`) and what it holds (bump, serve, submit) |
| One readiness evaluation | directions R3 | reads the ledger for a requested action and mode, and says what can proceed, what prevents it, and what's unknown |
| A recorded decision on a finding | Codex #3 (its model; its interface is theme B) | how "needs judgment" becomes "judgment supplied": which finding it answers, the evidence it rests on, and when it expires |
| `Next:` lines from the branch's state, checked to advance it | UX §3 and §9 | the evaluation's output: a step valid for the captured state that doesn't repeat the same refusal (qemu's dead end), or, where no command can, a decision to make, something to wait on, or information to supply |

**Deliverable:** one `branchstate` evaluation that `status`, `tidy`, `submit`, `explain` and serve all render, plus a contract test that every refusal carries a kind and a `Next:` that advances it.

Its contracts (from Codex's review of this document, at the person's request):

- **Readiness is for an action, not a boolean.** A branch can be ready to check but not to submit, ready for a draft pull request but not for unattended publication, or ready to tidy while a build runs. The question is: given these recorded facts, this action, and this mode (a terminal or a script, a person or serve), what can proceed, what prevents it, and what remains unknown?
- **Collecting evidence and evaluating it stay apart.** The evaluation reads what was recorded. It never fetches sources, runs checks, or rewrites state.
- **The decision record is layer 1's, though its interface ships later.** Without it, the first evaluation could say "needs judgment" with no durable way to say "judgment supplied". A decision names its finding, references the evidence it rests on, and expires when that evidence changes. Branch notes and the pull request section (theme B) are its interface.
- **A `Next:` advances the situation; it doesn't promise success.** A contract test can show that a step is valid for the captured state and doesn't repeat the refusal. It can't show that a network request, a build, or a review will succeed.

**First validation:** before widening the model, make `status`, `tidy` and `submit` agree on three real contradictions: one commit-rule finding, one stale check, and one upstream finding that needs judgment.

**Edit the declaration that ran** (directions R2) belongs in this layer too, but it's independent: it's about the evaluator, not the verdict. It can run in parallel, and it's what turns `update`'s per-port refusals (cargo, rust-src, py-flatbuffers, git, trivy) into one capability.

## Layer 2: the command surface (UX review, unchanged, re-sequenced)

The UX review stands as agreed. Two of its items absorb features from directions.md:

- `explain` covers checks and branches, which **is** `why`. Batch 43 gave it the content; the verb isn't built yet.
- `setup` replaces init, providers and auth, which **is** `doctor`.

The order within this layer becomes:

1. Branch naming: `-b` with completion, `-p`, `--pr`, and auto-start (§1, §2). It doesn't depend on layer 1, so it can go first or alongside.
2. Shared-flag contract test (§9), and output fixes (§10).
3. Stop only for judgment (§3), **after** the verdict exists, so it chains exactly the findings the verdict calls ceremony.
4. `archive` takes the worktree (§5), `explain` (§7), and `setup` (§6).
5. `review <pr> --check` (§8).
6. `--to` (§4), last and discussed first, as the review said.

## Layer 3: reach, merged and ranked

Overlaps between the docs collapse to seven themes. They're ranked by value to a maintainer, against cost and risk.

**A. Reviewer rules from the survey.** *Top of this layer, because the evidence for it is strongest, but not uniformly cheap.* Lines a PortGroup already sets, unnecessary revbumps, `Closes:` trailers, `github.tarball_from`, path-style deps, `-append`, the Python version in `create`, licence names, maintainers, and noarch platforms. Each rule flags only lines the diff touched. Each is a ledger rule (layer 1), so build it after the ledger, not before.

The survey sets priority; each rule still needs its own validation. Mechanical rules (`-append`, a `Closes:` trailer) are distinct from contextual suggestions, and several of these are contextual:
- a line that gives the same value without it in one environment isn't shown redundant across variants, platforms, or later evaluation;
- a build-dependency or compiler-flag change can change what's installed, so neither alone shows a revision bump unnecessary;
- an `X-devel` port existing doesn't make it a correct interchangeable path dependency.

Reporting stays scoped to the diff by default, though the analysis may read wider. An ambiguous rule starts as a notice, and holds automation only once its false positives are measured.

**B. The person's voice in the PR.** This merges directions' branch notes, directions R5 (a PR section the person owns), and Codex #3 (recorded decisions on findings: resolved by an edit, acceptable because…, awaiting info). It's one feature: a note attached to a branch or to one finding, carried into a PR section dockhand never writes. A decision on a finding is invalidated when its evidence changes. In layer 1 terms, it's how a "needs judgment" finding gets its judgment.

> **Pushback on Codex #3 and #7:** don't make dockhand's database the home of long-lived maintenance knowledge. It's one person's local store, and it's lost on a maintainer handoff. Reasoning about *this change* belongs in the PR, which is durable and public. Reasoning about *a patch's existence* belongs in the patch file's header or a Portfile comment, where the next maintainer will find it. Dockhand can help write those; it shouldn't be the only place they live.

**C. What an update does to consumers.** This merges Codex #4 and directions area 2. Compare the destroot's Mach-O install names and compatibility versions, the installed files, and the `.pc` and CMake metadata, before and after. It supplies observed interface changes that help justify dependent rebuilds, and bears on survey gap 2 (unnecessary or missing revbumps). A changed install name can establish a concrete problem. Unchanged names, compatibility versions, or exported symbols don't establish that consumers still work, and static inspection covers neither configuration migrations nor runtime behavior. Those limits are the standing reason to revisit the deferred testing work. It inspects files and runs nothing, so it stays clear of the deferral below.

**D. A maintainer's inbox.** This merges Codex #5 with directions' review queue, Trac tickets, and after-merge buildbot follow-up. It extends `outdated --mine` and serve's daily look: others' PRs on your ports with the 72-hour clock, open Trac tickets with a `Closes:` offer, and your merged updates failing on a buildbot. `review --check` (UX §8) is how you act on the "needs a test on hardware you have" item.

**E. Upstream context for the reviewer.** Directions area 6, plus the patch half of Codex #7: the changelog section between the two releases, and a patch whose upstream fix shipped flagged as removable. Both read sources dockhand already fetches.

**F. Advisories.** OSV for vendored crates, Go modules and PyPI packages. Both docs agree it's evidence for investigation, never a hold.

**Before D: a trust rule for other people's Portfiles.** (From the blind-spots review, decided by the person on 2026-10-02.) Today dockhand mostly evaluates the person's own Portfiles. `review <pr>` only reads a Portfile's text (`portfileFindings`) and never evaluates it. Planning or checking a branch made by `adopt --pr` evaluates a stranger's Tcl on the host, as the person (inferred from the code's shape, not yet traced). [Oracle](oracle.md) says a Portfile can deliberately get around that evaluator, and it rules out a host sandbox. That host holds the GitHub login that pushes to the fork, and `~/.dockhand/ssh/archives.key`, which every Tart guest trusts.

What's new is the unattended case. A reviewer who runs `port install` on a pull request already runs a stranger's Tcl, as root. So the risk dockhand adds is evaluating strangers' Portfiles with nobody looking. The rule:

1. **Nothing unattended evaluates a Portfile the person didn't author on the host.** That covers serve, the inbox, and scripts. Such a Portfile goes to a guest, or the step is skipped and reported. This is a fixed rule, not a setting.
2. **An attended command may do it, and says so once,** naming whose Portfile it is: "planning @alice's Portfile on this Mac". That is the trust a reviewer already extends, made visible.
3. **The Tart guest is the only real boundary.** No host sandbox, per the oracle's decision.
4. **One setting, `trust.others = "guest"`,** sends attended evaluation to a guest too, for anyone who wants that. It isn't a setup question, because a newcomer can't judge it. Named policy levels were considered and set aside: a level means nothing beyond where the safeguard lives, and this rule already says where.

**The inbox lists first.** Its first version shows others' pull requests and their state, without planning or building them, so rule 1 costs it nothing. Building one is `review <pr> --check` (UX §8), which a person runs on purpose and which follows rules 2 and 4. `adopt --pr`'s path is traced against the rule, which also covers reproduction bundles from others (G).

**G. Investigations.** Codex #2. First an opt-in to keep a failed Tart guest and open a shell in it (small, and the most-wanted piece). Reproduction bundles come later.

## Still deferred, and why

- **Running what a port installs, and upgrade testing** (Codex #1, Codex's top pick). The person deferred it on 2026-09-28 until the core is solid. Theme C gets much of its value without executing anything. I'd revisit it after layer 1 lands, starting with Codex's "recipes", which would be readable and run with a timeout and no input.
- **Imported results from others' hardware** (Codex #2). This conflicts with the roadmap's Deferred item "portable verification evidence". `review --check` on the person's own machine covers the near-term job.
- **Campaigns** (Codex #6). Named groups of branches add little for one person until there are real multi-port efforts. `update --mine --to check` plus status grouping covers today's case.
- **Reproducibility investigations.** Interesting, but nothing in the person's workflow asks for it.

## Suggested sequence

1. **Layer 1:** ledger, finding kinds and holds, the decision record, the per-action evaluation, and the `Next:` contract, proven first on the three contradictions above. Edit-what-ran in parallel.
2. **UX naming** (§1, §2), which can start alongside step 1.
3. **Survey rules** (A) on the ledger, and the person's voice (B), both of which feed the verdict.
4. **The rest of the UX review**, chaining included, now built on the verdict.
5. **Consumers** (C). Then the **trust rule** for others' Portfiles, then the **inbox** (D), listing first, with `review --check`.
6. **Upstream context** (E), **advisories** (F), and **investigations** (G), in whatever order use asks for.

## Decided

The person agreed to all three on 2026-10-02:

1. **The layering stands, and the readiness verdict comes before the UX review's chaining (§3).** Branch naming (UX §1 and §2) can go alongside layer 1.
2. **Maintenance reasoning lives in the pull request and in patch headers or Portfile comments.** Dockhand helps write it there, and its database is never the only home. This narrows Codex #3 and #7.
3. **Running what a port installs, and upgrade testing, stay deferred until layer 1 lands.** Consumer comparison (C), which runs nothing, is the safe first half.
