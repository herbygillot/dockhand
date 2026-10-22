<!-- Copied on 2026-10-22 from the command UX thread's write-up, /mnt/project-files/ux-review/output-and-overview-2026-10-22.md; where the roadmap placed it is in its part 5. -->

# Output levels and the status overview

2026-10-22. This is a design pass the person asked for (17:21Z). It covers why the default output reads as busy, the levels output should have, and how status and the other overview commands should be laid out.

Read against main d97b03a. The evidence comes from two places:
- **A corpus of 744 default-text runs.** Every command run in `go test ./internal/command/...` was captured at `cmdtest.RunFor`, then the hook was reverted. The corpus is in the UX thread's scratchpad and isn't shared, since it's large. The counts and examples below come from it.
- **The field notes and activity notes**, read for complaints and decisions about output.

## The short answer

- **The default is busy because multi-stage commands print every stage's full report.** A clean `bump jq` prints 46 lines. It names the branch 7 times and the commit 4 times, and it carries two warnings that fire on every success. What the person needs is 5 lines: what changed, and each stage's outcome.
- **No new verbosity flag is needed.** `-v` and `-vv` exist and Design v3 §12 already sets the rule: "Digests, record IDs, and lease terms appear only at `-v`". The fix is to move the stage reports, digests and plumbing lines behind the `-v` we have, and to write "said once" down as a rule with a test. I'd still leave `-q` out. Scripts have `--json` and exit codes, and a quiet default leaves little for `-q` to remove.
- **Status is already a table, but it's one flat table of every open branch.** With field testing's ~150 open pull requests that's ~150 rows, and the next step is said three times. Status should group branches by what each one waits on, using the buckets `branchstate` now owns, and collapse the branches waiting on reviewers to a count.
- **Everything at default fits the terminal's width.** Today only the attention list wraps. The tables and prose don't, and field testing measured lines of 121 to ~400 columns.
- **Where it lands: v0.5.0 part 5, "the interface on top"**, after the package split. `branchstate` owns the words and command owns the layout since today, so this is layout work. It adds about 2 batches.

## 1. Where the noise comes from

| Command | Runs | Lines out, median / max | Main source of length |
|---|---|---|---|
| ship | 31 | 11 / 82 | four stage reports in a row |
| bump | 20 | 10.5 / 45 | the same four reports |
| submit | 58 | 15 / 53 | the full plan block, and the whole PR description under `--plan` |
| status | 55 | 6 / 46 | four sections, and the next step said three times |
| tidy | 45 | 9 / 27 | the proposed commit block |
| check | 71 | 4 / 17 (stderr up to 24) | progress lines per attempt, each repeating the run's URL |

The patterns behind those numbers:

1. **Stage reports run back to back.** ship and bump print update's, tidy's, submit's and check's reports, each under its own `<branch> · <phase>` header. Those headers are for one stage run alone. Inside a chain, each report repeats context the previous one gave.
2. **The same thing is said more than once.**
   - **Identifiers:** `jq-1.8.1` appears on 7 lines of one bump, and commit `287d200` on 4.
   - **Branch name on one line:** every update and status-detail header reads `jq-update · ~/Source/macports-branches/jq-update`.
   - **The upstream comparison:** reported twice in one output. "Upstream not compared: …" comes first, then "Upstream · … nothing to compare". The hold legend appears twice in 3 runs.
   - **Status's next step:** said three times, once in the attention message, once in the attention row's command line, and once in the queue table's DETAIL column.
   - **One notice, two wordings:** check says "check-1 runs here, since no dockhand serve is running…" and wait says "No dockhand serve is running, so check-3 will run here…".
3. **Warnings fire on success.**
   - "Note missing; … Add --note" is in 56 runs, including runs where nobody could add one any more.
   - `! commit …'s Generated-By names dockhand devel` is in 69 runs. That's a development-build condition, said once per commit.
4. **Plumbing is shown at default.**
   - "check-1: attempt 1 of 3 on command, run command_iap4n2…", "bundling the revision" and "running my build box" are record IDs and steps behind the scenes, which §12 puts at `-v`.
   - The "Dockhand processes" table, added today, is shown whenever a process runs.
   - "Checkpoint tidy-1 keeps the old history" is printed on every tidy. It's needed only when someone wants to undo.
5. **Nothing is sized to the terminal's width.**
   - 103 of 1154 submit lines and 48 of 192 serve lines are over 100 columns.
   - Only status's attention list wraps.
   - The F9 acceptance row checks piped output for escape codes but never checks width.
6. **Marks mean different things.**
   - `·` means "act now" in status, "for your information" in the UX review's intent, and is a separator in about 94 places.
   - The only `!` in a successful run is the dev-build warning.

What works and stays:
- the ports × macOS table in a branch's detail, which field testing called "the right summary view";
- the `Next:` lines;
- `--plan` previews, which are meant to be read in full.

## 2. Levels

There are three levels, as Design v3 §12 already defines them. What changes is what each one includes.

| Level | Shows |
|---|---|
| default | What changed, one line per stage with its outcome, anything that needs the person (a hold, a failure, a question), and one `Next:` line. Run IDs and checkpoint names appear only where the person will type them. |
| `-v` | Each stage's full report: the proposed commit, the ready-to-submit block, the check's provider and per-port table. It also adds identifiers (commits, record IDs), the processes table, provider progress, and the GitHub request count. |
| `-vv` | Every step, as today. |

Three exceptions keep a stage's full report at default:
- **`--plan`**, because a preview exists to be read.
- **A question on a terminal**, because the person needs the block to answer it. ship's single confirmation shows the plan it asks about.
- **The stage that held or failed.** That stage, and only that one, expands to say why.

### Multi-stage commands print one line per stage

`bump jq` today is 41 lines on stdout and 5 on stderr. At default it becomes:

```
jq 1.7.1 → 1.8.1 (GitHub tag jq-1.8.1), on branch jq-1.8.1
  ✓ edited     textproc/jq/Portfile
  ✓ committed  jq: update to 1.8.1
  ✓ checked    passed on macOS 15 and 26
  ✓ submitted  #34901  https://github.com/macports/macports-ports/pull/34901
```

When it holds, only the stage that held expands:

```
hyperfine 1.21.0 → 2.0.0 (GitHub tag v2.0.0), on branch hyperfine-2.0.0
  ✓ edited     sysutils/hyperfine/Portfile
  ✓ committed  hyperfine: update to 2.0.0
  ✓ checked    passed on macOS 15 and 26
  ! held       a new major version, 1 → 2 (holds.majors)
Next: dockhand ship hyperfine   shows the hold and asks
```

On a terminal, the line for the stage in progress redraws in place: "⋯ checking  building jq (2 of 5) on macOS 26 · 3m". The "runs here, since no serve is running; Ctrl-C keeps what finished" notice goes into that line. The per-attempt progress lines go to `-v`. When output isn't a terminal, each stage line is printed once, when its stage ends.

A batch `ship --mine` prints one row per port, with each stage as a column cell (`✓ ✓ ✓ #35012`, or `✓ ✓ ! held: major`). Under it comes a "Needs you" list for the ports that stopped. This is the same grammar status uses below.

### Single-stage commands

These keep their current output, minus the patterns in §1:
- **Branch headers:** the header says the branch once and gives the path after "in" only where it isn't the current directory.
- **Checkpoint lines:** "Checkpoint tidy-1 keeps…" becomes part of the `Next:` line only when tidy rewrote history someone might want back. Otherwise it moves to `-v`.
- **The note nudge:** "Note missing" appears only in a preview or a question, where `--note` can still be added.
- **The dev-build warning:** the Generated-By warning is said once per command, not once per commit.
- **The upstream comparison:** it gets one line, and the empty "nothing to compare" moves to `-v`.
- **`submit --plan`:** the PR description shows its first 8 lines and "+N lines (dockhand submit --plan -v shows it all)".

### "Said once", as a rule with a test

At default, one command's output names a branch, a commit, a pull request's URL, or a run at most once outside a table. It also has at most one `Next:` line, and every line fits the width.

The test reuses the corpus hook as a permanent invariant check in the §9 contract test. It runs every command test's default output through those rules, with no golden files to maintain. Field testing has fixed "said twice" case by case in about ten batches, and this stops the next ten.

## 3. Width

- One writer in `term` wraps prose at the terminal's width with a hanging indent, as `writeAttention` does today.
- Tables truncate their widest free-text cell with `…`, and `-v` shows it in full.
- When output isn't a terminal, it uses 100 columns, or `COLUMNS` when set.
- The F9 acceptance row gains a width assertion at 60 columns.
- No dependency is needed: `term.Streams.Width` (internal/term/streams.go:95) already reads the width.

## 4. Marks, and colour

Part 5's §10 marks proposal is the right set, now that finding kinds have landed:

| Mark | Meaning |
|---|---|
| `✗` | broken |
| `!` | look at this |
| `✓` | done |
| `⋯` | in progress |

The separator `·` stops being a mark. Holds are said by a word ("held"), not by the mark, as the second look asked.

**Colour.** I recommend colouring only the marks on a terminal: `✗` red, `!` yellow, `✓` green. It would respect `NO_COLOR` and `--plain`, and no meaning would be carried by colour alone. Today there is no colour anywhere and none has been ruled out. Colour on the marks makes a 40-row status scannable at a glance, and it costs a few lines in `term`. the person chose this, "Marks only", on 2026-10-22 17:39Z.

## 5. Status and the overview commands

### Status groups by what each branch waits on

```
Needs you (2)
  ✗ gdal-3.9        check failed on macOS 15: undefined symbol in libgdal    dockhand logs -b gdal-3.9
  ! hyperfine-2.0.0 held: a new major version, 1 → 2                         dockhand ship hyperfine

Working (3)
  ⋯ jq-1.8.1        checking: building jq (2 of 5) on macOS 26 · 3m
  ⋯ yq-4.44.6       queued behind jq-1.8.1
  ⋯ mods-new        edits, uncommitted

Waiting on reviewers (142): CI passed 134, running 8 · dockhand status --waiting lists them

Your ports: 6 have newer releases (serve looked 2h ago) · dockhand ship --mine
serve: running since 09:12 · next look at 14:00
```

- **The groups are `branchstate`'s buckets.** Needs you, Working (being edited, queued, checking, submitting) and Waiting (on reviewers, or merge) get one judgment, written once. Each row is one line: mark, branch, what it's doing, and the command that moves it, in that order.
- **The command is said once**, in the row's last column. It isn't repeated in the message or in a queue table.
- **Waiting on reviewers collapses to a count**, with CI's tally in it. A Waiting branch whose CI failed or got a review moves up to Needs you, so the count holds nothing to act on. `status --waiting` lists that group, and `--all` adds merged and archived branches, as today.
- **The checks table folds into the Working rows.** A check shows on its branch's row, with live progress in the row. The separate RUN, BRANCH, SOURCE, ON, STATE, DETAIL table moves to `-v`, where run IDs and providers belong.
- **The processes table moves to `-v`**, except for its warning, "pid 412 expects schema 31…", which stays at default as a Needs you row.
- **Empty groups aren't printed.** With nothing in any group, status is one line, "Nothing needs you; 142 pull requests wait on reviewers", plus the serve line.
- **`status --watch` uses the same layout.** It adds the header line it has today, and fits the screen's height: Needs you in full, then Working, then the Waiting count, with "+N more" where a group is cut. This is still line output, redrawn, not a full-screen interface. That keeps the person's 2026-10-02 "room for one, but not now".
- **A branch's detail**, from `status <branch>`, keeps its layout and the ports × macOS table. Only its header changes, per §2.

### The other overview commands follow the same grammar

The grammar is a group heading with a count, one row per item, a mark, the item, what it is, and the command, sized to the width.

- **`outdated`:** PORT, HAVE, NEWEST and NOTE columns, grouped "Ready to ship", "Held" and "Not checked (by reason)".
- **`clean`:** shows its plan as rows grouped by what it removes. After running, it prints one line: "Removed 12 branches' worktrees and 9 fork branches" (with `-v` for the list), instead of restating the list.
- **`setup`:** one row per part, with a mark, as today. Each row's detail is wrapped, which fixes the "one big run-on" Tart line the person flagged on 2026-10-21. The maintainer hint said in setup, create and serve becomes one sentence in one place.
- **serve's log output:** one line per port's outcome, in the batch-ship row shape.

## 6. Where it lands, and its order

This goes in v0.5.0 part 5, "the interface on top", beside §10's marks and §9's contract test. It comes after part 4's package split, because the layout lives in `view` and in the verb packages the split makes. It adds about 2 batches to part 5's 2.5:

1. **Levels and the stage lines** (about 1 batch):
   - the stage-line renderer for ship, bump and serve's batch;
   - the full reports moved to `-v`, with the three exceptions;
   - the §1 patterns removed;
   - the live stage line on a terminal.
2. **Status and the overview grammar** (about 1 batch):
   - grouping on `branchstate`'s buckets;
   - the Waiting count and `--waiting`;
   - the queue and processes tables moved to `-v`;
   - the width writer;
   - `outdated`, `clean` and `setup` brought to the same grammar.
3. **With §9 and §10**, already planned: the marks, colour if the person takes it, and the "said once" and width invariants added to the contract test.

The acceptance rows that read default text need re-pointing to the new lines; most rows read `--json` and don't change. The JSON doesn't change at all.

## Not proposed

- **A `-q` flag.** It can be added later if a script needs one that `--json` doesn't serve.
- **A full-screen interface or a TUI library.** The person's 2026-10-02 "not now" stands.
- **New dependencies.** None are needed.
