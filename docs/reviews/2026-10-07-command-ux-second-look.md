# Dockhand's command line: a second look

Written 2026-10-07 against `main` at 16082a2. It draws on every command's `--help`, the code in `internal/command` and `internal/engine`, and probes in a scratch database against a real macports-ports clone. The probes stopped short of anything that needs MacPorts' Tcl. It follows the [2026-10-02 review](2026-10-02-command-ux.md) and doesn't repeat it.

## Where things stand

A lot of the earlier review has landed since 2026-10-02: `-b`, `-p`, and `--pr` with completion; authoring verbs starting a branch when nothing else could be meant; meaningful branch names; `start --port` and `edit --no-open`; `setup` with `setup github` and `setup tart`; `archive` taking the worktree; `undo`; `open`; and §10's preview rows, summaries, and batch exit codes. The surface is noticeably calmer. `setup`'s health report and `update jq` saying "jq is in no open branch, so this starts one for it, named for the version it moves to" are both what the review hoped for.

Still open, and placed behind layer 1 in the roadmap, are §3 (stop only for judgment), §4 (`--to`), §7 (`explain` for checks), §8 (`review --check`), and §9 (the shared-flag contract).

Today's friction comes from four places:

1. **Some bugs found while probing.** `start --port jq` makes a branch that later commands can't find, so `edit jq` starts a second one. A typo in `-b` silently starts a branch.
2. **The teaching lags the behaviour.** The top-level help, the README, and several commands' help still teach `--new`, `cd "$(dockhand path …)"`, `dockhand restore`, and `clean --archived`.
3. **The `-b`/`-p`/`--pr` rollout stopped halfway.** Eight branch verbs have them. `status`, `path`, `archive`, and the run commands don't, and `--port` now means four different things.
4. **Flags that change what a command is.** This is the larger theme, and it's where Herby's "workflows that change nature through flags" lands. `update` alone has four modes with three end states, and flags valid in only one of them.

## 1. Bugs found while probing

| What I ran | What happened | Should be |
| --- | --- | --- |
| `start --port jq`, then `edit jq` from the checkout's root | "jq is in no open branch, so this starts dockhand/jq-85oa for it": a second branch beside `jq-p3df` | `edit` uses `jq-p3df`. A branch started for a port should be found as that port's branch before it changes anything. |
| `start --port jq`, then `check -p jq` | "no tracked branch changes jq" | finds `jq-p3df` |
| `status --port jq` with two open branches, neither changed yet | "No open branches. dockhand update <port> starts one…" | "No open branch changes jq yet". It shouldn't say there are no branches when there are. |
| `edit jq -b jq-typo --no-open` | "Started dockhand/jq-typo", with no question asked | `-b` on authoring verbs creates a branch when no branch has the name, so a typo makes a branch. See §3. |
| `clean --help` | usage still shows `[--archived]`, and the long help describes it, but the flag is hidden | drop both, now that `archive` takes the worktree |
| `rebase --help` | "a checkpoint that dockhand restore brings back", and "A branch with uncommitted edits is refused" | `undo`. Carrying edits across was decided on 2026-10-02 and isn't built yet. |

The first two rows share a cause. A branch's ports come only from what it changes (`BranchesChanging`), so a branch's intent, the ports it was started for, isn't recorded anywhere. Recording `start --port`'s ports, and the port an authoring verb started the branch for, would let `-p`, auto-start, `status --port`, and the PORTS column all find it. `status` showed it as `PORTS 0 · nothing yet`.

## 2. The teaching lags the behaviour

A first user reads these before anything else, and today they teach the old shapes:

- The top-level help's loop is `update <port> --new`, `check`, `tidy`, `submit`. `--new` is no longer needed, and without a worktree, `check` needs `-p jq`.
- The README's update starts with `update jq --new` and `cd "$(dockhand path jq-…)"`, and mentions `update jq --new --submit` and `logs check-12 --port jq`.
- `bump --help` describes itself as `update --new --submit --yes`, and ends a hold with "dockhand submit --branch <name>".
- `start` ends with `Next: cd "$(dockhand path jq-p3df)"`, where `-b jq-p3df` now works from anywhere.
- `check --help` still says "(decision 29: switching is explicit)", and `serve --submit-passing` says "(Design v3 §11's guardrails)".
- No command has an `Example:` section.
- The roadmap's UX entry (line 518) still describes the first-draft selector (`jq`, `jq-4`, `#34901`, `check-42`), which the revised §1 replaced.

**Proposal.** Fix the text, and add one test. Every `dockhand <verb>` and `--flag` mentioned in help, the README, `docs/usage.md`, and `Next:` templates must exist and must not be hidden. That test would have caught every item above, and it's the kind of drift each rename will cause again. The text fixes carry no risk, and they're what first users of v0.3.0 will read, so I'd take them into the release pass if the freeze allows text.

## 3. Naming a branch: finish the rollout

Where each way of naming a branch exists today:

| | `-b` | `-p` | `--pr` | positional |
| --- | --- | --- | --- | --- |
| `check`, `diff`, `impact`, `open`, `rebase`, `submit`, `tidy`, `undo` | ✓ | ✓ | ✓ | (undo: checkpoint) |
| `update`, `edit`, `checksums`, `create`, `revbump` | ✓, creates when unknown | — | — | port |
| `status`, `path`, `archive`, `adopt`, `clean` | — | — | — | branch |
| `logs`, `cancel`, `wait`, `retry` | — | — | — | run ID |

And `--port` now has four meanings:
- the branch changing a port (`check -p`);
- a filter listing every branch touching it (`status --port`);
- that port's log (`logs --port`);
- a directory to bring into the worktree (`start --port`).

So `status -p jq` fails with "unknown shorthand flag", though `-p` works on eight other commands.

**Proposal:**

- **`status`, `path`, and `archive` take `-b`, `-p`, and `--pr`**, keeping their positional exact name. For `status`, a read-only command, ambiguity isn't an error. `status -p jq` with one match shows that branch in detail, and with several it lists them, which replaces `status --port`.
- **The run commands take `-b`, `-p`, and `--pr`, meaning that branch's latest check.** `logs -p jq` prints jq's log from its branch's latest check, from anywhere. Today, outside the worktree, you have to look up the check: "for a port's log outside its branch's worktree, name its check too: logs check-42 jq". `logs --port` goes, since `logs <check> <port>` already takes the port positionally, which frees `-p`.
- **`-b` names an existing branch, everywhere.** Naming a new branch becomes `--new=<name>`, with `--new` alone still choosing the name. A typo then fails with "no branch jq-typo; --new=jq-typo starts one" instead of starting a branch.
- **`start --port` stays**, because once intent is recorded (§1), "the branch for this port" means the same thing there as on `-p`. That leaves `--port` with one meaning, in two places that agree.

## 4. Flags that change what a command is

A flag that tunes how a command does its one job is fine: `--on`, `--fresh`, `--stat`. A flag that changes the command's **object** (what it acts on), its **end state** (edits left uncommitted, committed, or published), or its **output contract** (shape and exit code) is really a mode. Modes are where coherence breaks down, because each one has been invented locally.

| Command | Modes today | What changes |
| --- | --- | --- |
| `update` | plain · `--plan` · `--outdated` · `--submit` | Plain leaves edits **uncommitted**, `--outdated` **commits** each update, and `--submit` **publishes**. 6 flags say "with --submit", and 3 say "with --outdated". `--yes` means "start without asking" in one mode and "apply the tidy" in another, and `--check` means "queue a check of each". |
| `submit` | plain · `--check` · `--passing` · `--ready` | `--passing` makes it a batch over branches that needs a terminal. `--ready` changes the PR's state, not its content. `--check` runs a check in the foreground and then submits. |
| `bump` | (one mode) | It duplicates the pipeline flags (`--note`, `--title`, `--skip-notification`, `--tested-*`, `--on`) by hand, and already lacks `submit`'s `--type` and `--draft`. Copies like these drift. |
| `check` | plain · `--baseline` · `--variants each` · `-d` | `--baseline` builds master, not your branch. |
| `logs` | run summary · one port's log | Different output. |
| `diff` | patch · `--stat` · `--archive` | `--archive` compares source tarballs, not Portfiles. |
| `clean` | merged · `--closed` · `--legacy` | `--legacy` is a one-time migration from v2. |
| `serve` | daemon · `--drain` · `--install` / `--uninstall` | Installing and removing a launchd agent is administration. |
| `status` | table · `--attention` · `--refresh` | `--attention` changes the output and the exit code, and `--refresh` reads the network. |
| `adopt` | your branch · `--pr` | Someone else's PR, with different push rules. |
| `archive` | set aside · `--undo` | The inverse, as a flag, while `undo` is now a verb. |

### Recommendations

**a. One pipeline axis, with `commit` as a stage.** (Superseded by §6, which puts this axis on a new verb, `ship`, and takes the modes off `update` and `submit`.) §4's `--to edit|check|submit` should gain `commit`, because `update --outdated` commits each update as a side effect of being a batch. The stages become edit → commit → check → submit, and every authoring verb ends at `--to` (default: edit). Pipeline flags are then defined once and accepted by any command whose `--to` reaches their stage:
- `--title`, `--note`, `--type`, `--draft`, `--tested-*`, and `--skip-notification` for submit;
- `--on` for check.

`bump` becomes `update --to submit --yes` with serve's holds, and can't drift from `submit`.

**b. One batch grammar.** There are three batch spellings today:
- `update --outdated --mine [--check]` for ports;
- `submit --passing` for branches;
- `serve.for_outdated` for serve's daily run.

I propose:
- Authoring verbs take several ports, or `--mine`, which implies "those with a newer release", since a current port already starts nothing. `update --mine --to check` replaces `update --outdated --mine --check`.
- Branch verbs take a filter, starting with `--passing`, that makes them plural: `submit --passing` as today, and `status --passing` to look first.
- Every batch shows §10's preview rows, asks one confirmation, and follows the batch exit rule.

**c. Administration moves to `setup`.**
- `serve --install` / `--uninstall` become `setup serve [--remove]`, beside `setup github` and `setup tart`. `setup`'s health report already says whether serve runs.
- `clean --legacy` becomes something the health report offers when it finds pre-v3 branches. It's a migration, not cleaning.

**d. `undo` undoes the last thing, archive included.** After `archive`, people will type `dockhand undo`. Today that undoes the last tidy or rebase instead. `undo` with no argument should undo the branch's latest reversible step (tidy, rebase, or archive), saying which. `archive --undo` can stay as the specific form.

**e. One meaning per word.**

| Word | Meanings today | Proposed |
| --- | --- | --- |
| `--check` | queue a check of each (`update --outdated`); check then submit (`submit`); check the image (`setup tart`) | the first two become `--to check` and `--to submit`; `setup tart --check` stays |
| `--yes` | accept defaults (`setup`); don't ask (`tidy`, `clean`); submit when there's no terminal (`submit`); two things on `update`; variants each (`check`) | always "don't ask" |
| `--all` | include merged branches (`status`); list current ports (`outdated`); the whole log (`logs`) | `status --all`; `outdated --current`; `logs --full` |
| `--port` | four meanings (§3) | one, as in §3 |
| archive | set a branch aside; a source tarball (`diff --archive`) | `diff --source` |
| draft | prepare a branch (`serve.for_outdated = "draft"`); a draft PR (`submit --draft`) | `serve.for_outdated = "edit"`, or `--to`'s stage names |

## 5. Gaps: bridges still missing

- **A branch's purpose before its first edit.** See §1. It's both a bug and a missing concept.
- **From a failed check to why.** `explain` still takes only finding codes. §7 and the directions doc's `why` sit behind layer 1's readiness work, which is the right order.
- **From "changes requested" to what was asked.** `status` shows the review state, and `open` jumps to the browser. As far as I can tell from the help, nothing shows the unresolved comments with their file and line beside the branch, which Codex's directions asked for. I inferred this from the help and didn't confirm it in output.
- **Giving up on a branch for good.** `archive` keeps the Git branch and your fork's branch, and `clean` takes only the worktree of closed or archived branches. A branch you'll never return to stays on your fork indefinitely, and no command removes it. I'd add `clean <branch> --delete` for archived or closed branches. It should show what goes, local and fork branches included, and ask, because the fork branch is outward-facing.
- **Many stale branches.** `rebase` takes one branch, and after a busy week of master, a maintainer with twenty open updates rebases twenty times. With the batch grammar this becomes `rebase --stale` or `rebase --mine`, previewed as rows.
- **A note on a parked branch.** `--note` exists only on `submit`, `bump`, and `update --submit`, so there's no way to say why a branch is parked without submitting it. That's the directions doc's branch notes, and §3's `-b` rollout gives it an obvious shape: `dockhand note -b x "waiting on upstream fix"`.
- **Testing someone's PR here.** `review --check` (§8) still isn't built. Today it takes three commands: `adopt --pr`, `check --pr`, `review`.

## 6. Modes: one new verb, three folds, and no noun groups (refined 2026-10-07)

Herby asked whether the mode flags want new commands, or several commands rethought under one. I took each family of modes from §4's table in turn. The answer differs by family. Exactly one family wants a new verb, three want folding into a verb that already exists, and the rest are fine as they are.

### The pipeline family wants one new verb: `ship`

These are all the same act, carrying a change forward through edit → commit → check → submit, spelled six ways:

- `bump`;
- `update --submit`;
- `update --outdated [--mine] [--check]`;
- `submit --check`;
- `submit --passing`;
- `serve.for_outdated` and `serve.submit_passing`.

Each has its own flags, its own `--yes`, and its own idea of where to stop.

§4 above put `--to` on `update`. **I'd now revise that.** `update jq --to submit` would still make `update` end in different states depending on a flag, which is the problem itself. Instead:

- **The stage verbs lose their modes.** Each always ends in the same state:
  - `update` edits and leaves the edit uncommitted, one port or several;
  - `tidy` commits;
  - `check` checks;
  - `submit` publishes the committed head.

  `update` loses `--submit`, `--outdated`, `--check`, `--yes`, and its six "with --submit" flags. `submit` loses `--check` and `--passing`.
- **One new verb, `ship`, carries a change as far as `--to` says** (`commit`, `check`, or `submit`, the default), from wherever it is now:

  | Today | With `ship` |
  | --- | --- |
  | `bump jq` | `ship jq --yes` |
  | `update jq --new --submit` | `ship jq` |
  | `update --outdated --mine --check` | `ship --mine --to check` |
  | `submit -b x --check` | `ship -b x` |
  | `submit --passing` | `ship --passing` |
  | a held bump, continued with `submit --branch <name>` | `ship jq` again: it resumes from where the branch is |
  | `serve.for_outdated` plus `serve.submit_passing` | `serve.ship_to = "list" \| "commit" \| "check" \| "submit"`, with the same guardrails and daily limit |

**`ship`'s contract is what §3 and §10 already describe, now in one place:**

- Targets are ports (`ship jq gh-dash`, `ship --mine`) or branches (`-b`, `-p`, `--pr`, `--passing`).
- From a port, the edit stage is a version update. Other authoring (`create`, `revbump`, `edit`) starts the branch, and then `ship -b` carries it forward.
- It shows one plan, a row per target and a column per stage, then asks once. `--yes` skips the question. Without a terminal, running it is the decision, as `bump` and `submit --check` are today. The check is explicit, because `--to` names it, which keeps Herby's rule that a script's submit never starts a check unasked.
- Each step is the stage verb's own logic, with its holds. A target stops at a protective refusal or a hold, and the others go on.
- Running it again resumes. It's idempotent: a target already past `--to` is left alone and reported.
- The flags of each stage are defined once and accepted when `--to` reaches that stage:
  - edit: `--revbump-dependents`, `--except`, `--shared-release`, `--with-obsolete`, `--keep-old-checksums`;
  - check: `--on`, `--tests`;
  - submit: `--title`, `--note`, `--type`, `--draft`, `--tested-*`, `--skip-notification`.

  `bump`'s missing `--type` and `--draft` can't happen again.
- It follows the batch exit rule from §10.

**Why a new verb, and not `bump` widened.** `bump -b mods-new` (a new port) and `bump --passing` read wrong, because "bump" means a version change in MacPorts' own words. `ship` reads right in every row above. If v0.3.0 goes out with `bump`, it can stay as a documented alias for `ship <port> --yes` for one minor release.

**Its relation to layer 1.** `ship` is "do what `Next:` says, until `--to` or a stop". It needs layer 1's readiness per action to know where a branch is and what moves it, so it belongs where the roadmap already puts §3 and §4: after layer 1's step 5. The stage verbs losing their modes goes in the same batch, since `ship` is what replaces them.

### Three families fold into verbs that already exist

- **Someone else's pull request goes into `review`.** Today it's `review <pr>` (text), `adopt --pr` (a branch), `check --pr` (build), and §8's proposed `review --check`. `review <pr>` becomes the maintainer's one verb: it reads the PR, applies the rules, and with `--check` adopts the PR as `pr-<n>` if needed and builds it. It posts with `--comment` or `--request-changes`. `adopt --pr` stays for when you mean to push to their branch.
- **Administration goes into `setup`.** `serve --install` and `--uninstall` become `setup serve [--remove]`, and `clean --legacy` is offered by the health report. `setup` is already the group for the environment (`setup github`, `setup tart`).
- **The branch lifecycle goes into `undo` and `clean`.**
  - `undo` reverses the branch's latest reversible step, which can be a tidy, a rebase, or an archive.
  - `clean` covers every way a branch ends: merged (the default and automatic), and closed or archived with `--delete`, which also removes the local and fork branches after showing them.
  - `archive` stays as the one way to park a branch.

### The rest needs no new verb

- **Runs** (`check`, `wait`, `cancel`, `retry`, `logs`) share one object, a check, and each does one thing. What they lack is the `-b`/`-p`/`--pr` rollout (§3), not structure. `queue` folds into `status`. `check --baseline` stays a flag, because its result is still a check, beside the branch's.
- **Reading** (`status`, `diff`, `impact`, `outdated`, `explain`, `open`): `diff --archive` becomes `diff --source`, and `status --attention` stays, since its output contract is documented for prompts and scripts.

### What I'd not do: noun groups

`dockhand branch archive`, `dockhand run cancel`, and `dockhand pr review` would group the help, but they'd add a word to every daily command to fix what the help's own sections already do. They'd also put `ship`, which spans ports, branches, and PRs, under no noun at all. Flat verbs plus one orchestration verb is the smaller change with the bigger effect.

### Net effect on the surface

- One verb is added (`ship`). `bump` is retired or kept as an alias, and `queue` folds into `status`. The top-level count stays at 32 with the alias, or drops to 31 without it.
- About fifteen mode flags leave `update` and `submit`.
- Every remaining verb has one end state and one output shape. The one exception is `ship`, whose job is to vary its stopping point, and it does that through a single option.

## Suggested order

1. **Now, if the release pass allows text:** §2's help and README fixes and the help-reference test. Text only, no behaviour change, and it's what v0.3.0's first readers see.
2. **v0.3.1:** §1's bugs (recording a branch's intent, the `status --port` message, `-b` creating on a typo, `clean`'s stale usage), and §3's rollout of `-b`/`-p`/`--pr` to `status`, `path`, `archive`, and the run commands.
3. **With layer 1's step 5**, where the roadmap already places §3, §4, §7, and §9: `ship` and the stage verbs losing their modes (§6), the word table, and `undo` covering `archive`.
4. **When touched:** `setup serve`, `clean --legacy` into the health report, `diff --source`, `clean --delete`, batch `rebase`, `note`, and `review` as the maintainer's verb (§6).
