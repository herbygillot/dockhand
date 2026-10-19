# The architecture review of 5897a82, coalesced with the exiv2 field notes

On 2026-10-19 the person chose "Now" for the [architecture review](../reviews/2026-10-19-architecture-5897a82.md)'s second-owner batch (18:44Z), and sent its items to the prioritization thread instead of the implementation thread. They then set a rule (18:45Z): "Recoalesce the roadmap every time you receive a list of items to examine that are intended for implementation." This run coalesces four inputs at once:

- the review's forty findings, R3-1 to R3-40, with its text edits;
- the [exiv2 field notes](2026-10-19-exiv2-field-notes.md), whose placement was still waiting on the person;
- the four roadmap entries the implementor placed itself since 2026-10-18 (dd836869, a3ff473f, 0a4a05ea, c3c12d28);
- field testing's libxkbcommon license hold.

This change is docs only. The review read main at 5897a828, which is still main. Where a placement depends on the code, the lines were read again:

- R3-2's predicate (engine/status.go, runner.go);
- R3-3 and R3-25 (command/ship.go `shipTargets`, author.go `settleSelection`, engine/selector.go);
- R3-4 and R3-14 (check.go `runQueued`, ship.go `queueHead`, engine/ship.go);
- R3-22 (root.go);
- where the tree is chosen (settings.go `options`);
- the license classifier (project/license.go).

Nothing else was re-checked.

## Where each finding goes

| | Finding | Review placed | Placed |
| --- | --- | --- | --- |
| R3-1 | The major hold twice on serve's path | Now | Part 1, item 4 |
| R3-2 | `BranchStatus.Active` counts baselines and runs asked to stop | Now | Item 4, in the first commit, so an rc6 can carry it if the person wants one |
| R3-3 | ship's own port-to-branch rule | Now | Item 4, with R3-25 |
| R3-4 | The check pipeline a third time | Part 4.1, or now | Item 7's head (below) |
| R3-5 | The command's ship, a second driver | Part 4.1, with the review's shape | Part 4, item 1: `shipOne` calls `Engine.Ship` for edit, tidy, and check; done when command/ship.go judges nothing |
| R3-6 | Five shapes of "another try may meet it" | Part 4.1 | Part 4, item 1, beside `Refusal` |
| R3-7, 18, 19, 26, 29 | Duplicates below the engine | Now | Item 4. R3-29 takes the code-organization review's finding 14 from Taken when touched (below) |
| R3-8, 11, 12, 13, 15, 16, 28, 33, 35, 36, 37 | Second owners and hygiene | Now | Item 4 |
| R3-9 | "Nobody looks" four ways | 4.1, and now for `ArchiveBranch` | Item 4 for the engine half; part 4, item 1 for one consent value |
| R3-10 | The unattended publication gate twice | 4.1, and now for `Held`'s signature | The same split |
| R3-14 | Three active-run policies in command | Add to 4.1's text | Item 7's head, with R3-4 (below) |
| R3-17 | The spend log's environment half in github | X1 | Item 6 |
| R3-20 | The manifest-hook string flag | Part 3 | Step 3, beside xapian's family of archives |
| R3-21 | Three seam styles, three plist producers | X1 for the seams, now for the plist | Item 6 and item 4 |
| R3-22 | The `inspectRoot` seam | Now | Item 4, as placed. X1 changes what `settings` carries, not where `Run` builds the tree, so `newRoot(settings, streams)` survives it (inferred from root.go and settings.go). |
| R3-23 | `ComparisonLines` in words.go | 2.1 | Part 2, item 1 |
| R3-24 | Two readings decide PR precedence | 4.1, already | Part 4, item 1, named |
| R3-25 | `Select`'s stringly `what` and `purpose` | Now (section 2); missing from section 5's list | Item 4, with R3-3 (below) |
| R3-27 | Drifted surface text | Now | Item 4, in the first commit, with R3-2 |
| R3-30 | The accepted reason as a sentence | 2.2 | Part 2, item 2, ahead of the standing acceptance |
| R3-31, R3-38 | Sentences below the engine; verdicts read from errors | 4.1 | Part 4, item 1, under `Refusal` |
| R3-32 | The process-global pending login | X1 | Item 6 (already CX-11's) |
| R3-34 | Status JSON without the standing | Part 5 | Part 5 |
| R3-39 | Attention beside the standing; two refresh readers | 4.1, and now for the readers | Part 4, item 1, and item 4 |
| R3-40 | Smaller placements below the engine | When touched | Taken when touched |

The review's process rule became a standing rule under Maintenance ("Relocations"), and item 4 closes by it. The person's coalescing rule is in the roadmap's opening paragraph.

## What moved, and why

- **The check pipeline written once, at the head of item 7** (R3-4, R3-14). The review disagrees with itself here: section 4 pulls the pipeline forward into the batch, and section 5 leaves it in part 4.1. Item 7 is the next item to add to the pipeline. `check --keep` needs a plan field through `CheckRequest`, and the drain is `runQueued`'s case where nobody leads, today "Start dockhand serve to run it." (command/check.go). That case is the `Leader == nil` decision R3-14 moves into `SeeThrough`. Written once first, each lands in the engine once, not in command's copies. It isn't in item 4: item 4 is a few lines per finding, and it should land fast enough for R3-2 to reach a candidate quickly. v0.4.0's part 2, item 4 said the pipeline "written three times becomes one", and is marked done. Three copies are left, and item 7 says so.
- **R3-25 beside R3-3.** Section 2 places R3-25 "Now"; section 5's list leaves it out. Once R3-3 makes ship call `Select` and `settleSelection`, an untyped `what`, `purpose`, and `errStartForVersion` would reach a new caller and be rewritten later.
- **Finding 14 into item 4.** Its remedy names "one macOS release-to-words helper", which is R3-29. Its provider-name half was done earlier. The entry is in [the roadmap's history](2026-10-16-roadmap-history.md#from-taken-when-touched-on-2026-10-19-now-in-v050).
- **`path` outside the tree, from the log item to X1.** The tree is chosen in command's `settings.options` (`--tree`, `MACPORTS_TREE`, or here), the environment catalogue `compose` takes over. The fallback written there before X1 would be moved by X1. The log item (item 5) is then only about failed checks, about 1 batch.
- **Field testing's two from Later, into X1's one GitHub runtime** (0a4a05ea placed them in Later's "spending less"). The ask before a look that would pass the hour should compare against what's left of the hour, which only a shared runtime knows from GitHub's answers. Against 5,000 it misses what other sessions spent, and that's the note's reason for asking. Looking up shared-source subports once goes with it, since the two together cut the same look. ETags and GraphQL batching stay in Later.
- **A typed accepted reason before the standing acceptance** (R3-30, dd836869). A standing acceptance is one more reason a failure was accepted, so the kind comes first.

Placed and not moved:
- **xapian's family of archives** (c3c12d28) stays in step 3, beside R3-20, both being predictions.
- **The license group entry** (a3ff473f) stays in Later. The person still decides first whether dockhand reads infrastructure's list of licenses.
- **libxkbcommon's hold** is a field-testing fix for main. rc5 was tagged on 5897a828 at 19:03Z, while this run was written, so whether an rc6 carries it, with R3-2, is the person's call. Dockhand's own classifier, run on the two tags' LICENSE files, finds MIT, HPND, and HPND-sell-variant at both ends. Their coverage falls from 79.5% to 64.0% of the words, below `wholeText`'s 75. 1.13.2 adds an "SPDX full name / SPDX short identifier / License text" heading to each block, a longer preamble, and more copyright lines. MacPorts had 1.7.0, so the comparison spanned six releases.

Also brought up to date:
- Next's first paragraph, which still had rc4 in acceptance, now says rc5 (the person's card, 17:57Z).
- The open decision on rc3's three fixes went to Decided, since they shipped in rc4.

## The size

| | Batches |
| --- | --- |
| v0.5.0 with the exiv2 items, as proposed earlier today | about 25 to 29 |
| Item 4, the second-owner batch (the review said one) | +1.5 |
| X1, the quota ask and the shared-source lookups | +0.5 |
| Item 7, the check pipeline once (v0.4.0 counted it done) | +1 |
| Part 4, item 1, ship's shape, one consent value, and the transient kind | +0.5 |
| **v0.5.0 now** | **about 28 to 32**, 2.5 of them done |

Part 6, linked libraries, is still the one part to move to Then; without it, about 27 to 30.
