# Dockhand's command line: a UX review

Written 2026-10-02 against `main` at 3c80520, from every command's `--help`, the code in `internal/command`, design v3's command rules (§4, §5, §10, §12, §16), and what the field-testing thread ran into on real ports today. It is a proposal for discussion. Nothing here is decided or implemented.

## The short version

The surface is sound where it matters most. Flat verbs grouped by purpose, `--plan` on changing commands, a `Next:` line after each step, the attention list, and bare `dockhand` showing status are all worth keeping. I would not regroup the verbs into `gh`-style nouns.

The friction comes from three patterns, not from any single command:

1. **Naming the branch you mean costs too much.** Branches are named `jq-4k2p`, `--branch` takes only an exact name, and the fallback is to `cd` into a worktree dockhand can't take you to.
2. **The core loop is a chain of refusals.** `submit` refuses until `tidy` has run, `check` refuses outside a worktree, and `update` refuses (or asks) until you add `--new`. Each refusal names the fix, but it's always the same fix.
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
dockhand check jq
dockhand submit jq
```

The rest of this document is the proposals in the order I'd take them, then smaller items, then what I'd leave alone.

## 1. One way to name a branch

**What happens today.** There are three conventions:

- a positional branch name: `adopt`, `path`, `status`, `archive`, `clean`;
- `--branch <exact name>`: `update`, `edit`, `create`, `checksums`, `revbump`, `check`, `diff`, `impact`, `tidy`, `rebase`, `submit`;
- a positional run ID, falling back to the branch's latest only inside its worktree: `logs`, `cancel`, `wait`, `retry`.

`--branch` has no short form, though `--tree` has `-t`. Names carry a random suffix (decision 37), so you look one up in `status` before you can type it. Outside a worktree, `check` says "no tracked branch… run this in the branch's worktree (dockhand path <name>)", and `start` ends with `Next: cd "$(dockhand path uxprobe)"`. Field testing spent its first minutes on most runs finding branch names.

**Proposal.** Every place a command takes a branch accepts a **selector**, resolved in this order, and refused with the choices when it's ambiguous:

| Selector | Means |
| --- | --- |
| `jq-4k2p`, `dockhand/jq-4k2p` | that branch (as today) |
| `jq-4` | the one branch whose name starts with it |
| `jq` | the one open branch that changes port `jq` |
| `#34901` | the branch whose pull request that is |
| `check-42` | (for run commands) that check; anywhere else, its branch |

- `--branch` gets `-b`, and accepts a selector.
- Commands whose only object is a branch or a run take the selector positionally: `check jq`, `tidy jq`, `submit jq`, `rebase jq`, `logs jq`, `cancel jq`, `wait jq`, `status jq`, `path jq`, `archive jq`. `logs jq` already means "jq's log in the latest check" inside a worktree, and resolving `jq` to its branch keeps that meaning everywhere.
- Commands whose positionals are ports or paths (`update`, `edit`, `diff`, `impact`) keep `-b`.
- `status`'s `Next:` lines and the attention list then print `dockhand check jq` rather than `dockhand check --branch jq-4k2p`.

**This relaxes a design rule, deliberately.** Design v3 §4 says "a port name never picks a branch." The rule exists so that an edit never lands silently in the wrong one of two branches touching `jq`. A selector that resolves only when exactly one open branch changes the port, and refuses with the list otherwise, keeps that guarantee. The interactive prompt in `update` already picks the branch from the port when there's exactly one, so this just extends it to scripts and the other verbs.

One risk is that `check jq` reads as "check only jq" when the branch changes more ports. It checks the whole branch, which is almost always what you want; `--only` still narrows it.

With selectors in place, being inside a worktree becomes optional. You go there to edit, never to run dockhand.

## 2. Start a branch when nothing else could be meant

**What happens today.** At the checkout's root, `update jq` with no open branch for jq asks "start dockhand/jq-4k2p for it? [Y/n]" on a terminal and refuses in a script, naming `--new`. `revbump` instead starts a branch without asking ("since a rebuild has its own reason"). `create` and `checksums` follow `update`. So the default differs by verb, and the main journey always carries `--new`.

**Proposal.** When you're on master or in no branch, and no open branch changes the port, every authoring verb starts the branch and says so on its first line, as `revbump` does now. `--new` stays, to force a second branch when one exists. Starting a branch is cheap to undo, and the first line names it, so the prompt protects against nothing.

`init`'s closing `Next: dockhand start <name>` should point to `dockhand outdated --mine` or `dockhand update <port>` instead. `start <name>` is the less common path, for work that isn't one port's update.

## 3. `submit` carries the loop

**What happens today.** With uncommitted edits, `submit` refuses: "these edits are not committed… Commit them with dockhand tidy, or submit only what is committed with --head" (`engine/submit.go:215`). Tidy never changes a file, and checks capture files by content, so a check of the working files already covers what tidy will commit. That last point is my inference from the help texts. The refusal is a step dockhand could take itself.

**Proposal.** When edits are uncommitted, `submit`'s preview includes tidy's plan, "will commit: jq: update to 1.8.1", and applies it under the same rules `tidy` uses. A plan made only of dockhand's own edits applies without asking, and anything else is shown on a terminal and refused in a script. `submit --check` already checks and then submits. With tidy folded in, the shortest loop for any branch is:

```sh
dockhand edit jq        # or update, create, revbump…
dockhand submit jq --check
```

The same applies to answering a review. You edit, then `submit --check`, which folds the fix into the port's commit (as `tidy`'s follow-up rule asks), checks it, and pushes. `tidy` stays for when you want to rearrange commits, use `--squash`, or save a plan to edit.

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

**Proposal.** One option, `--through edit|check|submit`, with `--yes` meaning only "don't ask":

| Proposed | Replaces |
| --- | --- |
| `update jq` | `update jq --new` |
| `update jq --through submit` | `update jq --new --submit` |
| `bump jq` | unchanged; documented as `update jq --through submit --yes` with serve's holds |
| `update --mine` | `update --outdated --mine` (a current port already starts nothing) |
| `update --mine --through check` | `update --outdated --mine --check` |
| `serve.for_outdated = list / edit / check / submit` | `list / draft / check`, plus `submit_passing` |

Two consequences are worth more than the flags:

- **Running it again resumes.** `bump jq` today says "a branch already changes the port" and stops. After a hold (terraform-1.16's false commit-rule finding today), you continue with `submit --branch <name>`. If the pipeline ran up to `--through` from wherever the branch is now, running `bump jq` again after you looked would continue, and the held branch would say "rerun to continue".
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

**Proposal.** `explain <thing>` takes any identifier dockhand prints and says why it's in that state:

- a finding code, as today;
- a check: the step that failed, the log's lines for it, whether it was advisory, and whether a baseline can answer (the directions doc's `why`);
- a branch or port selector: why it's held or on the attention list, and what clears it.

`Next:` lines on a failed check or a held bump then point to `dockhand explain check-42` or `dockhand explain jq`. One verb that means "tell me more about what you just said" is easier to learn than three.

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
- **`dockhand open [selector]`** opens the branch's pull request in the browser, since status shows its number but no link.

## What I'd leave alone

- **The flat verb list.** A noun-verb shape (`dockhand branch check`) adds typing to every daily command to save a few lines of help. The groups in `--help` already do that job.
- **`bump`'s name**, decided on 2026-09-27. With §4 it's a preset, which makes the name matter less.
- **The random branch suffix**, already declined in the roadmap. Selectors make it something you rarely type.
- **Aliases for v2 verbs**, decided against in design §16.

## Suggested order

1. Selectors and `-b` (§1) with auto-start (§2). This is the biggest daily win, and mostly in branch resolution (`engine.named`) and the commands' argument parsing.
2. `submit` folding in tidy (§3), and the shared-flag test (§9), which would catch the field-testing inconsistencies as they're fixed.
3. `archive` taking the worktree (§5) and `explain` for checks and branches (§7). Both are small, and both close friction field testing actually hit.
4. `setup` (§6) and `review --check` (§8).
5. `--through` (§4) last, after a discussion, since it changes the most words and touches decided ground.
