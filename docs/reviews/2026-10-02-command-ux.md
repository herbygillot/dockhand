# Dockhand's command line: a UX review

Written 2026-10-02 against `main` at 3c80520, from every command's `--help`, the code in `internal/command`, design v3's command rules (§4, §5, §10, §12, §16), and what the field-testing thread ran into on real ports today. It is a proposal for discussion. Nothing here is decided or implemented.

## The short version

The surface is sound where it matters most. Flat verbs grouped by purpose, `--plan` on changing commands, a `Next:` line after each step, the attention list, and bare `dockhand` showing status are all worth keeping. I would not regroup the verbs into `gh`-style nouns.

The friction comes from three patterns, not from any single command:

1. **Naming the branch you mean costs too much.** Branches are named `jq-4k2p`, `--branch` takes only an exact name, and the fallback is to `cd` into a worktree dockhand can't take you to.
2. **The core loop is a chain of refusals.** `submit` refuses until `tidy` has run, `check` refuses outside a worktree, and `update` refuses (or asks) until you add `--new`. Of the loop's refusals, about a dozen name a fix that is reversible and that dockhand could simply do (§3).
3. **One idea is spelled several ways.** "How far to go" is `update --submit`, `update --outdated --check`, `bump`, `serve.for_outdated`, and `serve.submit_passing`. "I'm done with this branch" is `archive` followed by `clean --archived`. `--plan`, `--yes`, `--all`, and preview exit codes mean different things on different commands.

Fix those three and the README's update shrinks from five commands, one of which needs a random suffix, to three:

```sh
# today
dockhand update jq --new
cd "$(dockhand path jq-4k2p)"
dockhand check
dockhand tidy
dockhand submit

# proposed
dockhand update jq
dockhand check -p jq
dockhand submit -p jq
```

The rest of this document is the proposals in the order I'd take them, then smaller items, then what I'd leave alone.

## 1. Naming the branch you mean (revised 2026-10-02 after scrutiny)

**What happens today.** There are three conventions:

- a positional branch name: `adopt`, `path`, `status`, `archive`, `clean`;
- `--branch <exact name>`: `update`, `edit`, `create`, `checksums`, `revbump`, `check`, `diff`, `impact`, `tidy`, `rebase`, `submit`;
- a positional run ID, falling back to the branch's latest only inside its worktree: `logs`, `cancel`, `wait`, `retry`.

`--branch` has no short form, though `--tree` has `-t`, and no command offers shell completion for branch names (no `ValidArgsFunction` or `RegisterFlagCompletionFunc` in `internal/command`). Names carry a random suffix (decision 37), so you look one up in `status` before you can type it. Outside a worktree, `check` says to run it in the branch's worktree, and `start` ends with `Next: cd "$(dockhand path uxprobe)"`.

### Why the first version doesn't hold up

The first version proposed one positional "selector" that took a branch name, a unique prefix, a port, `#PR`, or `check-42`. Walking it through dockhand's workflows breaks it in several places.

- **Two branches on one port is a normal state, not an edge case.** An adopted branch of your own beside dockhand's branch for the same port, a contributor's pull request (`adopt --pr` makes `pr-34905`) beside your own update, or yesterday's draft beside the one serve prepared overnight (`serve.for_outdated = "draft"`) are all ordinary. In each, `jq` stops resolving, so the shortcut fails exactly when you have the most branches to tell apart.
- **Resolution changes over time.** `dockhand submit jq` names one branch today and is refused tomorrow, after serve opens another. Once the first merges, it silently names a different branch. That's harmless for `status` but wrong for `submit`, `archive`, `cancel`, and `clean`, and for any `Next:` line a person copies an hour later.
- **Namespaces collide.** Branch names are anything for an adopted branch, and `start jq` makes `dockhand/jq`, which needn't change jq. If one argument can be a branch or a port, `check jq` checks the branch named `jq` even when another branch is the one changing port jq, and nothing says so.
- **Prefixes collide by construction.** Names are `<port>-<ID>`, so `libuv` is a prefix of `libuv-4k2p` and `libuv-devel-9x2m`, and `py-` of hundreds. Prefix matching has to go.
- **"Changes the port" is fuzzier than it sounds.**
  - `BranchesChanging` matches by directory name (`macports.ScopeOf(…).PortNames()`), so a subport isn't found: libuv-devel lives in `devel/libuv`, and terraform-1.17 in a directory named for another port. Meanwhile 3c80520's roadmap note says a change to one subport is that subport's.
  - `update --revbump-dependents` puts 30 revision bumps in libuv's branch. `jq` would then resolve to "rebuild jq for libuv", which is surprising when you meant jq's own update.
  - A `_resources` change touches no port at all.
- **`#34901` is a shell comment.** `dockhand submit #34901` reaches dockhand as `dockhand submit`, which then acts on the branch you're standing in.
- **A run ID that selects its branch misleads.** `submit check-42` reads as "submit what check-42 checked", but the branch may have moved since.
- **`logs` already uses its positional for a port within the branch,** so a selector there would mean two things.

### Revised proposal

The goal stays the same: nobody should have to type or look up `jq-4k2p`. The fix is to keep **one meaning per argument** and resolve ambiguity where a person can see it.

1. **`-b` takes an exact branch name, with completion.** Add `-b` to every `--branch`. Add dynamic shell completion for branch names that shows each one's ports and purpose: `jq-4k2p  jq: update to 1.8.1`, `pr-34905  @alice's #34905 (jq)`. Most of the typing problem goes away here, while a person is looking, and nothing about it shifts over time. Drop prefix matching.
2. **`-p/--port <port>` picks the branch changing that port,** as `status --port` already finds them. It never comes from a bare positional, so a port name and a branch name can't collide.
   - It matches by port name and includes subports, using the same rule the roadmap now uses for subports.
   - With one candidate, it acts and names the branch on its first line, as design §4 already requires.
   - With several on a terminal, it asks, labelling each by purpose: "your update to 1.8.1", "@alice's #34905", "revision only, in libuv-4k2p". In a script it refuses and lists exact names.
   - When a branch changes the port only by its revision, it ranks below branches with a substantive change. It still counts, but it's labelled.
3. **`--pr <number>`**, the spelling `adopt --pr` already uses, picks the branch tracking that pull request. When none tracks it, the command offers `adopt --pr`. No `#`.
4. **Run IDs stay with the run commands** (`logs`, `cancel`, `wait`, `retry`) and never stand for a branch.
5. **A port-scoped command keeps the port as its positional.** `update`, `edit`, `checksums`, and `logs <port>` already have the port, so with no `-b` and no worktree, they resolve the branch from it under the same rule as `-p`, which `update` already does on a terminal. A command about the whole branch (`check`, `tidy`, `submit`, `rebase`, `archive`) never guesses from a positional; it takes the worktree, `-b`, `-p`, or `--pr`.
6. **Output prints exact names.** `Next:` lines, the attention list, and errors say `-b jq-4k2p`, never `-p jq`, because a person copies them later, when `-p jq` may mean something else.
7. **Explicit beats where you are.** `-b`, `-p`, or `--pr` overrides the worktree you're standing in. When the two differ, the first line says so: "jq-4k2p (not uxprobe, checked out here)".

The README's loop then reads `dockhand check -p jq` and `dockhand submit -p jq`, or, with completion, `dockhand check -b jq<Tab>`.

### Meaningful branch names (Herby's decision, 2026-10-02)

Branches dockhand starts are named for what they do, and decision 37's short ID is added only when that name is taken:

| Started by | Name | Example |
| --- | --- | --- |
| `update` | port and new version | `jq-1.8.1`, `terraform-1.16-1.16.5` |
| `update --revbump-dependents` | the library's update | `libuv-1.52.0` |
| `revbump` | first port, `rebuild` | `gdal-rebuild` |
| `create` | port, `new` | `mods-new` |
| `checksums` (stealth update) | port, `checksums` | `jq-checksums` |
| `edit`, or nothing more specific | port and ID, as today | `jq-4k2p` |
| `start <name>`, `adopt`, `adopt --pr` | unchanged | `my-fix`, `pr-34905` |

Things the implementation has to get right:

- **"Taken" means it exists now, as `FreeName` already checks.** That covers a tracked branch that isn't merged (open, archived, or closed unmerged, which keep their Git branches), a local Git branch, or a worktree directory. A merged branch is the end of its line, and the store already allows its name to be reused (`branch_name`'s unique index excludes merged rows, and `ResolveRecord` falls back to the newest merged branch of a name). So `jq-1.8.1` can be started again after the first one merges and is cleaned. A merged branch that hasn't been cleaned still has its Git branch, so it holds the name until clean runs. A stray fork branch of that name, with no local one, isn't looked up, because `submit`'s push is already conditional on where the fork's branch was and refuses safely.
- **A name can go stale.** If 1.8.2 comes out before `jq-1.8.1` is submitted and the branch is moved to it, the name lies. Before a pull request exists, `update` renames the branch, keeping the record as `adopt` already does for renames. After one exists, the name stays, as design §3 requires, since the fork's branch is the pull request's head.
- **Versions are made safe for Git.** Characters Git refuses in a ref (`~ ^ : ? * [ \`, spaces, `..`) become `-`.

With names like these, `-b jq-1.8.1` is something you can type from memory, and completion does the rest.

### Left for you to decide

- **Whether `-p` should act alone in scripts when there's exactly one candidate.** I've proposed yes, since the first line names the branch. The cautious alternative is to refuse in scripts and require `-b`. That's safer against overnight drafts, but meaningful names make `-b` cheap enough that it would cost little.

## 2. Start a branch when nothing else could be meant

**What happens today.** At the checkout's root, `update jq` with no open branch for jq asks "start dockhand/jq-4k2p for it? [Y/n]" on a terminal and refuses in a script, naming `--new`. `revbump` instead starts a branch without asking ("since a rebuild has its own reason"). `create` and `checksums` follow `update`. So the default differs by verb, and the main journey always carries `--new`.

**Proposal.** When you're on master or in no branch, and no open branch changes the port, every authoring verb starts the branch and says so on its first line, as `revbump` does now. `--new` stays, to force a second branch when one exists. Starting a branch is cheap to undo, and the first line names it, so the prompt protects against nothing.

`init`'s closing `Next: dockhand start <name>` should point to `dockhand outdated --mine` or `dockhand update <port>` instead. `start <name>` is the less common path, for work that isn't one port's update.

## 3. Stop only for judgment (revised 2026-10-02, replacing "`submit` carries the loop")

### The refusals along the loop today

I went through every refusal and hold that `update`, `check`, `tidy`, `submit`, and `rebase` can produce (`engine/submit.go`, `tidy.go`, `verbs.go`, `capture.go`, `command/check.go`, `author.go`). They fall into three kinds.

**Protective refusals.** Going ahead would be irreversible, would touch someone else's work, or would publish something unverified. These are the guardrails, and they should stay as they are:

- someone else pushed to the pull request since dockhand last did (`submit.go:417`);
- the contributor's pull request doesn't let maintainers push, or you lack write access (`:459`, `:467`);
- the branch moved since the preview (`ErrStaleSubmit`, `ErrStalePlan`);
- restoring a checkpoint would discard later work or staged files (`tidy.go:978`, `:993`);
- a rebase conflicts, which is abandoned with the branch as it was (`verbs.go:182`);
- a merge commit on the branch (`tidy.go:147`, `submit.go:244`);
- holds: an upstream finding, a commit-rule finding, or another open pull request for the port.

**Refusals for judgment.** Only the person has the answer. These should stay, but on a terminal they should **ask** rather than exit:

- a commit needs a subject, or a combined commit needs `--author` (`tidy.go:293`, `:306`);
- a pull request changing several ports needs `--title` (`submit.go:494`);
- which of several branches you mean (§1).

**Refusals of ceremony.** Dockhand names the exact fix, and the fix is reversible or only reads. These are the ones that make the loop feel like a chain:

| Refusal today | Where | What it should do |
| --- | --- | --- |
| "start one with --new" | `author.go:784` | start the branch (§2) |
| "run this in the branch's worktree" | `check`, `tidy`, `submit` | `-b` / `-p` (§1) |
| "these edits are not committed… Commit them with dockhand tidy" | `submit.go:215` | include tidy's commits in the preview |
| "has no commits above master yet; commit your edits with dockhand tidy" | `submit.go:222` | the same |
| "no check has finished for this commit's files; run dockhand check first" | `submit.go:346` | offer to check first, which is `submit --check` |
| "check-42 is already running for these files; dockhand wait check-42 follows it" | `check.go:862` | follow check-42 |
| "check-42 is running for other files; --replace…" | `check.go:868` | ask on a terminal, refuse in a script |
| "worktree has edits; choose --head or --working-tree" | `check.go:266` | check the working files, as in a worktree, and say so |
| "has uncommitted edits…; commit them (dockhand tidy) or set them aside before rebasing" | `verbs.go:158` | carry the edits across the rebase, and abandon as now if they don't reapply |
| "is not above its base; rebase it onto master first" | `tidy.go:139` | offer the rebase |
| "--plan changes nothing, so it starts no branch" (revbump, checksums) | `verbs.go:147` | plan on master, as `update --plan` does |
| "X is already changed in Y, so nothing was changed" (bump) | `author.go:240` | continue that branch from where it stopped (§4, rerunning resumes) |
| "--on… go with --submit", "--mine and --check go with --outdated", "--yes goes with…" | `author.go:95–111` | gone, with one `--to` (§4) |

### The principle

**Dockhand does what a refusal would tell you to do, unless the fix is irreversible, touches someone else's work, or needs your judgment. It shows what it will do in the preview it already gives.**

`tidy` already works this way. A plan made only of dockhand's own edits applies without review, and anything else is shown first. The proposal extends that rule from one command to the loop.

### Where chaining needs care

Chaining steps has costs, and they set the boundaries:

- **Cost.** A check can take most of an hour on a VM, and running one isn't a step to hide. Because of that, implied steps split by cost:
  - Cheap, reversible, local steps happen anywhere, scripts included: starting a branch, committing dockhand's own edits, following a running check, and planning on master.
  - Steps that build or take a VM are offered on a terminal, defaulting to yes, and need the flag in a script (`--check`, `--replace`).
  - Publishing is never implied. `submit` stays the decision, as design principle 7 says.
- **One confirmation, not one per step.** Today `update --submit` previews each step, so a session asks three times. A chained command should show the whole plan once before anything happens, for example:

  ```
  jq-1.8.1: commit "jq: update to 1.8.1" → check on tart:26 → push to your fork and open the pull request
  ? go ahead [Y/n]
  ```

  After that it stops only on a protective refusal or a hold.
- **Saying where it stopped.** When a chained command stops partway, the message names the step, what was kept, and the one command that continues, which is usually the same command again (§4). Design §12 already asks errors for this. Chaining makes it matter more.
- **Keeping the model learnable.** People who never run `tidy` might not learn that dockhand rewrites commits. The plan line above names each step, so the model is still in front of them every time, and the separate commands remain for doing a step alone.

### Decided

- **A script's `submit` never starts a check without `--check`** (Herby, 2026-10-02).
- **`rebase` carries uncommitted edits** (Herby, 2026-10-02).
  - Dockhand's own workflow leaves work uncommitted. `update`, `checksums`, and `create` write working files, and `check` builds them, so a branch that is all uncommitted edits is the normal state, not a careless one. Today's advice, to commit with `tidy` first, forces commits mid-work that tidy's follow-up rule then has to fold back together.
  - It's safe if it's atomic. Capture the working files and the index as a snapshot first, as `check` already captures them. Then replay the commits and reapply the edits on top. If any edit doesn't reapply cleanly, put the branch, files, and index back exactly as they were, as a conflicting rebase does now, and name the file. Usually master changed that port, and the message should say so.
  - Git has the same behaviour as `rebase --autostash`, so it's familiar.
  - Things to get right:
    - the rebase checkpoint records the snapshot, so `restore rebase-4` brings the uncommitted edits back as they were;
    - files `create` staged stay staged;
    - an untracked file at a path master now has stops the rebase;
    - unresolved conflicts are still refused.
  - One cost remains. An editor holding a Portfile that the rebase rewrote has a stale buffer, as with `git rebase --autostash`, so the rebase's output lists every file it changed under the edits.

### What changes for the loop

`update`, `edit` or `create`, then `submit`. `submit` shows the commits it will make, offers the check if none covers these files, and opens the pull request. `check` and `tidy` stay for when you want to run a step by itself, and the README teaches the three-command form first.

## 4. One "how far" setting

**What happens today.** The update pipeline is edit → tidy → check → submit. How far a command goes is set in five ways:

| Today | Goes to |
| --- | --- |
| `update jq` | edit |
| `update jq --submit` | submit, previewing each step |
| `bump jq` | submit, asking nothing |
| `update --outdated --mine` | edit (and commit), for many ports |
| `update --outdated --mine --check` | check queued, for many ports |
| `serve.for_outdated = list / draft / check` | nothing / edit / check |
| `serve.submit_passing` | submit, within serve's guardrails |

`update` has 15 flags, and several only work in one mode: `--check` only with `--outdated`, `--on` and `--tested-*` only with `--submit`, and `--yes` means "start without asking" with `--outdated` but "apply the tidy" with `--submit`. `serve.for_outdated = "draft"` means "prepare a branch", which is easy to confuse with a draft pull request (`submit --draft`).

**Proposal.** One option, `--to edit|check|submit`, with `--yes` meaning only "don't ask":

| Proposed | Replaces |
| --- | --- |
| `update jq` | `update jq --new` |
| `update jq --to submit` | `update jq --new --submit` |
| `bump jq` | unchanged; documented as `update jq --to submit --yes` with serve's holds |
| `update --mine` | `update --outdated --mine` (a current port already starts nothing) |
| `update --mine --to check` | `update --outdated --mine --check` |
| `serve.for_outdated = list / edit / check / submit` | `list / draft / check`, plus `submit_passing` |

Two consequences are worth more than the flags:

- **Running it again resumes.** `bump jq` today says "a branch already changes the port" and stops. After a hold (terraform-1.16's false commit-rule finding today), you continue with `submit --branch <name>`. If the pipeline ran up to `--to` from wherever the branch is now, running `bump jq` again after you looked would continue, and the held branch would say "rerun to continue".
- **serve's daily work and a person's command use the same words.** `serve.for_outdated = "submit"` keeps the guardrails and the daily limit that `submit_passing` has now, and `serve` still announces it at start.

This is the largest change here, and it touches the 2026-09-27 decision on `bump`, so it's the one I'd discuss before anything is built.

## 5. Ending a branch is one command

**What happens today.** To abandon a branch you run `archive`, which "hides a branch from status without touching its files", and then `clean --archived` to free its worktree. Field testing hit this today. A worktree is reconstructible: "dockhand path or any command that needs the worktree checks it out again" (`clean --help`).

**Proposal.** `archive` also removes the worktree when it has no uncommitted edits or untracked files, and keeps the Git branch, the fork's branch, the checkpoints, and the record, as `clean --archived` does now. `archive --undo` brings it back, with the worktree recreated on demand. `--keep-worktree` is for anyone who wants it kept. `clean --archived` goes away.

`clean` is then for merged and closed branches and leftover VMs, which serve already does daily, and it stops being something a person has to remember. `clean --legacy` moves to the health report in §6.

If the branch has an open pull request, `archive` should say so and print the command that closes it. It shouldn't close the pull request itself.

## 6. `setup`: one first-run command and the health check, rather than adding `doctor`

**What happens today.** First run is three verbs from three groups: `init`, `providers setup tart`, and `auth login`. `providers` and `auth status` report readiness separately. `init` already prints a health table (Git ✓, Authoring !, Providers, Publishing, Records) and is "safe to rerun". The directions doc proposes adding `doctor` on top. `maintainer`, which `--mine`, `create`, and serve's daily look all need, can only be set by editing TOML, though `create` already works out the right line from your GitHub login.

**Proposal.** One verb, `setup`, replaces `init`, `providers`, and `auth`:

- `dockhand setup` registers the checkout the first time. On a terminal it then offers each missing piece in turn: the Tart image, the GitHub login, and the `maintainer` line it inferred, written to the config with your agreement. Later runs print the health report, with the fixes offered. This is what `doctor` would be, under the name newcomers already ran. The disk under Tart's images, log size, stale worktrees, and pre-v3 branches join the report, as the directions doc lists.
- `setup tart [release] [--xcode …]` replaces `providers setup tart`, and `setup github [--logout]` replaces `auth login` and `auth logout`.

That is two fewer top-level commands, and one line under "Getting started".

## 7. `explain` answers "why" for anything dockhand printed, rather than adding `why`

**What happens today.** `explain` takes only a commit-rule code. The directions doc proposes a separate `why <check>`. Field testing hit three different "why" questions today: a check that said only "command execution failed", a bump held on a finding, and a branch on the attention list.

**Proposal.** `explain` says why something dockhand printed is in its state. Its positional takes a finding code or a run ID, which can't be confused because run IDs always look like `check-42`, and a branch comes through the same flags as everywhere else (§1):

- a finding code, as today;
- a check: the step that failed, the log's lines for it, whether it was advisory, and whether a baseline can answer (the directions doc's `why`);
- a branch (`-b`, `-p`, or `--pr`): why it's held or on the attention list, and what clears it.

`Next:` lines on a failed check or a held bump then point to `dockhand explain check-42` or `dockhand explain -b jq-4k2p`. One verb that means "tell me more about what you just said" is easier to learn than three.

## 8. A bridge for maintainers: test someone's pull request here

**What happens today.** To try a contributor's pull request, you run `adopt --pr 34905`, then `check --branch pr-34905`, then `review 34905`, which leaves the build to MacPorts CI.

**Proposal.** `review 34905 --check` adopts the pull request if needed, checks it where `check.on` says, and puts the result in the review text: "Built on macOS 26 arm64: passed, check-51". It still posts nothing unless asked. This is the "a PR needs a test on hardware you have" job from Codex's directions, and it reuses only what exists already.

## 9. A shared-flag contract, with a test

These are each small, but together they make the tool feel inconsistent, and a table-driven test over the command tree would keep them fixed:

- **`--plan`** is on every changing command and means the same thing everywhere. Today `create` and `rebase` have none. Field testing found that `update --plan` with no branch plans on master, while `revbump --plan` and `checksums --plan` refuse.
- **Preview without a terminal** has one exit code. Field testing found `submit` exits 1 and `clean` exits 0. Design §12 ("a missing choice is an error naming the argument") says 1.
- **`--yes`** means "don't ask" and nothing else. Today it's "accept defaults" (`init`), "submit without asking, when there is no terminal" (`submit`), and two things on `update` (§4).
- **`--all`** has three meanings: include merged branches (`status`), list current ports too (`outdated`), and print the whole log (`logs`). I'd keep `status --all`, rename `outdated --all` to `--current` (it adds current and unchecked ports), and rename `logs --all` to `--full`.
- **`check`'s capture** uses four flags, `--head`, `--staged`, `--working-tree`, and `--include`. It could be `--files working|staged|head`, plus `--include`.
- **`Next:` lines come from the branch's state.** Field testing found `rebase` suggesting `submit` on a branch whose pull request had merged. The directions doc's "one readiness verdict" is the fix: one function decides the next step, and every command prints it.
- **Foreground and serve run checks the same way.** Field testing found `providers.tart.capacity` governs only serve. A foreground `check` or `bump` should take a slot under the same limit.
- **Help text speaks to users.** `check --help` cites "(decision 29: switching is explicit)" and `serve --submit-passing` cites "(Design v3 §11's guardrails)". No help page has an `Example:` section. Most `Long` texts are one dense paragraph per flag, which the guide could carry instead. Every page also repeats `--db`, `--git`, and `--tree`, which a custom usage template could list once.

## Smaller items

- **`restore` → `undo`.** `git restore` restores files; dockhand's restores history. `undo` with no argument undoes the branch's latest tidy or rebase, and `undo tidy-3` picks one.
- **`queue` → part of `status`.** It's already status's footer line. **`watch` → `status --watch`**, keeping its keys.
- **`--tested-binaries --tested-variants` → `--tested binaries,variants`** on `submit`, `update`, and `bump`.
- **`dockhand open [-b|-p|--pr]`** opens the branch's pull request in the browser, since status shows its number but no link.

## What I'd leave alone

- **The flat verb list.** A noun-verb shape (`dockhand branch check`) adds typing to every daily command to save a few lines of help. The groups in `--help` already do that job.
- **`bump`'s name**, decided on 2026-09-27. With §4 it's a preset, which makes the name matter less.
- **The random branch suffix**, already declined in the roadmap. Selectors make it something you rarely type.
- **Aliases for v2 verbs**, decided against in design §16.

## Suggested order

1. `-b` with completion, `-p`, and `--pr` (§1), with auto-start (§2). This is the biggest daily win, and mostly in branch resolution (`engine.named`, `BranchesChanging`) and the commands' argument parsing.
2. Stop only for judgment (§3), starting with `submit` folding in tidy and `check` following a running check of the same files, and the shared-flag test (§9), which would catch the field-testing inconsistencies as they're fixed.
3. `archive` taking the worktree (§5) and `explain` for checks and branches (§7). Both are small, and both close friction field testing actually hit.
4. `setup` (§6) and `review --check` (§8).
5. `--to` (§4) last, after a discussion, since it changes the most words and touches decided ground.
