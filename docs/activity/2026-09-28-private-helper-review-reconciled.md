# 2026-09-28: the private-helper review, checked and reconciled

The person asked Codex to review where logic lives: the private helpers that decide what a MacPorts, Tcl, Git, or forge fact means, away from the package that owns the fact. The [review](../reviews/2026-09-28-private-helper-ownership.md) read `7be0dc2d`, and saw `f43228ac` land as it finished. Each of its ten findings was checked again at `dd21ac87`, two commits later.

## What the check found

**Every finding holds.**
- **The probes.** The seven probe tests in the review's [patch](../reviews/2026-09-28-private-helper-probes.patch), applied to a scratch worktree of `dd21ac87`, all fail as it says:
  - `known_fail on` is eligible, and a braced `supported_archs` excludes its own architecture (finding 1);
  - a data block's `revision` line reads as a revision-only change (finding 2);
  - a Cargo `[dependencies.x]` table, a pyproject list in single quotes, and an unreadable package.json each compare as no change (finding 3);
  - a registry that isn't crates.io passes creation's Cargo reader (finding 4);
  - braces and backslashes in a new port's description don't survive Tcl (finding 5).
- **The rest, by citation.**
  - Tart's archive signing in the SSH package's keys (6), and the layout facts' many copies, among them `reuse.Resources` and Tart's own `validPortName` (7).
  - Three SSH readiness loops, the facts tool's the third (8), and the stealth edits made after the editor's fidelity check (9).
  - `refreshedParts` locating the description's sections again after the merge (10). That's still true after `2d92608b` and `dd21ac87`.
  - The smaller table, row by row. `commitrules.Version` reads `github.setup` and its kin but not `go.setup`, so tidy can't derive the version of a Go port a person edited (finding 2).

**Worse than it ranks it.** Finding 3 weakens the guardrail unattended submission relies on (D4): a manifest the comparison can't read, or a dependency in a form it doesn't parse, reads as no change, so nothing holds. `[dependencies.serde]` tables are common in Rust projects, so the gap is real, not only synthetic.

**Written this session.** Finding 6 (archive keys in `channel`), finding 7's `validPortName`, and finding 10 (the body merge) are code this session added to item 6 and to submit. The review is right about each: the facts they interpret have owners they didn't use.

## Where it went

Each lands in its own commits, and each probe becomes a regression test when its finding is fixed.

**Before item 6 goes on,** what stands on its own, in the roadmap's own order: a guardrail first, then what's written into a Portfile, then fidelity, then structure.
1. **Finding 3**, source comparison in a package of its own that says what it couldn't read.
2. **Findings 5 and 4**, `create`'s Tcl words and Cargo lock.
3. **Finding 9**, a stealth update's edits inside the editor: the open half of the earlier finding 19, moved here from the smaller items.
4. **Finding 10**, the body merge's typed result.
5. **Finding 7 and the table**, the facts with homes: the tree's layout (the earlier finding 29), GitHub addresses (13), provider names (14), a maintainer's identity, and which Darwin releases have arm64. The first three move here from the smaller items. They come before items 6 and 7, which would otherwise add more readers of their copies.

**Inside item 6,** where the code they touch is already moving, so it's touched once:
- **Findings 1 and 2**, build eligibility and a conservative Portfile inspection in `macports`, as planning moves out of the engine, beside the evaluator's typed facts (the earlier finding 27);
- **Finding 6**, binary-archive preparation in `macports/binaryarchive`, before item 6 builds more on the archive install.

**With item 7,** `create` from `crates:` names builds on finding 4's shared Cargo.lock reader.

**With the Tart smaller item** it revalidates (the earlier finding 7): finding 8, with the facts tool named.

The first version of this reconciliation put findings 1, 2, and 6 in their own block before item 6. Asked whether the items needed synthesizing with the roadmap, that was the part that didn't: the same planning code and archive install would have been moved twice.

**Kept as the review says.**
- `newport.licenses` stays in `newport` until a second consumer needs it.
- The launchd plist writers stay separate, as the earlier reconciliation declined.
- The small formatting helpers, and the helpers it lists as already home, stay where they are.

**Declined:** nothing.
