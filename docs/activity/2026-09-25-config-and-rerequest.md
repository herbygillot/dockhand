# 2026-09-25: maintainer, submit.rerequest_review, and dockhand config

Design v3 §12's configuration file, and §6.10's re-request of review.

## What changed

- **`maintainer`** is read and checked the way MacPorts writes a maintainers line: entries separated by spaces, each a braced group such as `{@ada example.org:ada}`, or a bare address, handle, `openmaintainer`, or `nomaintainer`. An unbalanced or nested brace is refused by name. Nothing writes it into a Portfile yet: `create` will, and `outdated --mine` will read it.
- **`[submit] rerequest_review`** is `ask` (the default), `always`, or `never`. It applies after `submit` pushes to an existing pull request whose reviewers requested changes:
  - `ask` asks `? ask @ryandesign to review again? [Y/n]` on a terminal. Without one, it says who requested changes and how to have dockhand ask them.
  - `always` asks them to review again.
  - `never` does nothing.
- **Who requested changes.** The forge client's `Inspect` now keeps the logins whose latest review requests changes. The branch's recorded observation carries them, which is an added JSON field, so no schema change. The new `RequestReviewers` is GitHub's "re-request review". `Engine.RequestReview` asks them and journals `branch.rerequest`.
- **`dockhand config`** shows the configuration file and the database it uses. For every setting it shows the value in effect, marked "(default)" when the file doesn't set it. It also reports as JSON.

## Tests

- **Config:** a valid maintainer and rerequest setting, four malformed maintainers, and a bad rerequest value.
- **Forge client:** `Inspect` names two reviewers who requested changes, and `RequestReviewers` sends them.
- **Command:** after changes are requested:
  - a push without a terminal names the reviewer and asks nothing;
  - on a terminal, Enter asks them again;
  - with `never`, nothing is said or asked.
- **`config`:** defaults, a file's values, JSON, and a malformed maintainer refused.

The command tests' stand-in GitHub now reports a pull request's head from the fork, as the engine's stand-in already did. Before, a second push to the same pull request looked like someone else's.
