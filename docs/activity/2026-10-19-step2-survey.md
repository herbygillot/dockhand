# Step 2's whole-tree survey, and ls-tree in batches

The end of the roadmap's v0.5.0 part 3, step 2: one whole-tree
tools/survey pair, before and after, on tree commit ac62a9de, with no Tart
check running. "Before" is 7db41efa, main before step 2's first commit.
"After" is d8e44ce9, with step 2's three parts.

## ls-tree in batches

On the first try, both builds failed: "git ls-tree: fork/exec
/opt/local/bin/git: argument list too long". A whole-tree survey passes
every port directory to `git.MaterializeInto`, which gave them all to one
ls-tree.
- It now gives at most 2,000 paths to each ls-tree.
- It takes each file once, since batches' paths can overlap. The second
  try failed on that: "open .../aqua/DiskJockey/Portfile: file exists".
- Both builds of the pair carry the fix; the before build is 7db41efa
  with it applied.

## The pair

Both runs took about 1h30m on 9.9 cores at parallel 8, with 42,314 ports
each.

    input-found  40495 → 40510 (+15)
    unsupported    517 →   502 (-15)
    moves: 15 unsupported → input-found; regressions 0
    Portfiles fully covered: 19611 → 19617

Each move, one by one:
- checksums now checked, where they were unsupported: LyX, fltk,
  fltk-devel, msodbcsql, qca-qt5 and its three plugins, and qca-qt6 and
  its three plugins. These are commands of one text that the frame's line
  places, or that the probe settles.
- qca-qt6 and its three plugins also gain four
  `platform-restriction:not-tested:fetch-rejected` findings each. Those
  checks run now that the checksums are located, and found nothing.
- The version input now checked, where it was unsupported: adodb,
  bash-devel, and lrslib. These are version edits of several literals.

No other move. The survey's journals are in the session's scratchpad.

## The survey's parallelism

Before the pair, one category, sysutils (1,394 ports), was surveyed with a
CPU profile. It ran 10.06 cores at parallel 8, near the baseline's 10.7,
so the 7.2 cores of 2026-09-22 didn't come back. Of the survey's own CPU,
workspace.Overlay's os.Link, which hard-links the tree for each candidate
evaluation, took about 66 of 133 profiled seconds, and RemoveAll about 16.
Prioritization closed the item, keeping the hot spot in the roadmap's
history.
