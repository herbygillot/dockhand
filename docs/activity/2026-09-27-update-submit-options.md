# 2026-09-27: update --submit takes submit's options

`update <port> --new --submit` goes from the edit to a pull request in one invocation: it tidies the branch, checks the commit, and submits that commit once the check passes. It passed nothing on to the tidy or the submit, so three things a person running it had to do by hand now come with it.

**`--tested-binaries` and `--tested-variants`.** These tick the pull request template's checkboxes, as they do for `submit`. Without a terminal there was no way to tick them, and the pull request always went up with both unticked. On a terminal, either flag answers the template's two questions, as it does for `submit`.

**`--on`.** This says where the check builds, as for `submit --check`. The check had always run where `check.on` says.

**Where to check is settled before the edit.** A mistaken `--on`, or no provider at all, used to fail only after the branch was started, the Portfile edited, and the tidy applied. It now fails first: "--on nowhere: no provider "nowhere" is set up; nothing was changed".

**`--yes`.** On a terminal, `--yes` applies the tidy without its review menu when the plan is unambiguous, meaning dockhand's own edit alone. That is what happens without a terminal anyway. It never applies an ambiguous plan, which still asks. `--yes` already meant "start without asking" for `--outdated`. For a single port's update it was silently ignored, so without `--submit` or `--outdated` it is now refused.

Flags that do nothing without `--submit` are refused without it: `--on`, `--tested-binaries`, `--tested-variants`, and `--yes`.

**What changed.**
- `linkedOptions` carries `on`, `testedBinaries`, `testedVariants`, and `yes`; `tidyAndSubmit` passes them to `decideTidy` and `submitChecked`.
- `checkWhere` resolves the environments before `author` edits anything.
- The help, `docs/usage.md`, and design §6.2 describe the options.
- `TestUpdateSubmitPassesOnWhereAndWhatWasTested` covers each refusal, and the early failure leaving no branch. It also covers a terminal run stopping at the tidy menu without `--yes` and going through with it, with one checkbox ticked by a flag. It fails without the pass-through.
