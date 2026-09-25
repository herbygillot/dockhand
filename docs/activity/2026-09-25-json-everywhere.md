# 2026-09-25: --json for the commands that change things

This follows [the envelope](2026-09-25-json.md). `--json` now covers the whole loop, not only the commands that read.

## What changed

- **Now reporting JSON:** `start`, `adopt`, `update`, `checksums`, `revbump`, `edit`, `tidy`, `restore`, `submit`, `rebase`, `review`, `cancel`, `logs` (with `--port`, the log's text), `archive`, `clean`, and `explain`. These join `status`, `path`, `diff`, `impact`, `check`, `retry`, `wait`, `queue`, and `config`.
- **Shapes.** Each result has its own snake_case shape in `command/json_results.go`. The engine's and the model's types are never serialized directly.
- **Previews.** A command that shows a preview reports the preview as its result. If it goes on to act, it reports what it did:
  - `tidy --plan` gives the proposed commits, their messages, authors, notes, and anything blocking. An applied `tidy` adds `applied` with the checkpoint and the new commits.
  - `submit` gives its preview: title, commit, push, checks, findings, blocking, and the description. Once submitted, it adds `pull_request` with its number, URL, and whether it was created or pushed.
  - `clean` gives each step, with `applied` saying whether anything was removed.
  - `update --plan` includes the diff, and `--revbump-dependents` lists the dependents it bumped or would.
  - `review` gives the findings, what is resolved, the text, and how it was posted, if it was.
- **Refusing is unchanged.** A `--json` command line never asks, so each command's no-terminal rule applies. `submit` needs `--yes`, and its refusal still carries the preview as the result. `init`, `auth`, `serve`, and `watch` still refuse `--json` before doing anything: the first two are interactive, and the last two keep running.

## Tests

- **`TestJSONForTheWholeLoop`** decodes all of standard output as one envelope at each step:
  - `start`;
  - `update --plan`, then `update`;
  - `check`, then `logs`;
  - `tidy --plan`, then `tidy`;
  - `submit` refused without `--yes` but carrying its preview, then `submit --yes`;
  - `explain`;
  - after the merge, `clean`, then `clean --yes`;
  - `serve --drain`, refused.
- **The earlier test's refusal case** now uses `watch`, since `tidy` reports JSON.
