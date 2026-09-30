# 2026-09-30: the release outdated found, a plan's JSON, and one GitHub client

Item 6's seventh and last piece is the code-organization review's findings 36 and 1, as the roadmap narrowed them.

## The release outdated found goes to the update (finding 36)

`outdated` resolves each port's newest release upstream and keeps it (`OutdatedPort.Release`). Preparing the update passed only the version string, so `Update` asked upstream again for the release it had just been told of. That's a second round of requests per port, against GitHub's rate limit when serve prepares many. And if the tag moved between the two asks, what outdated reported wasn't what was prepared.

Now:
- `UpdateRequest.Release` carries the release, and `prepareOne` passes outdated's.
- `preparation.ResolveRelease` takes a release it's given rather than resolving one, and still checks it against the Portfile (`CheckRelease`). So a tag the Portfile can no longer name is refused, however it was found.
- `Update` refuses a release that isn't the version asked for, or one given to anything but a version update.

A test fixture had preset a release that the calendar-version test then asked `ResolveRelease` to ignore. The test now clears it, since it wants resolution.

## A plan's JSON says what was asked, and what --only left out (finding 36)

`check --json` and `check --plan --json` now give the plan's `only`, `also`, and `fresh`, and its `omitted` targets: the changed ones `--only` left out, which submission still requires. The text printed the omissions already. The JSON dropped them, and dropped what was asked, which the plan records.

## One GitHub client (finding 1)

`github.SystemClient(store)` is the one way dockhand makes GitHub as the person's login reaches it: `GH_TOKEN` or `GITHUB_TOKEN` where set, else the stored login. The engine's forge, upstream discovery, and `create`, and the command layer's `auth`, all use it. The client literal had been written out at two sites, and at four when the review read it.

## One set of dependencies for engine.Open: not taken

The review's other remedy for finding 1 was for `settings.open` to build the engine's collaborators once and pass them to `engine.Open`, so the lazy getters could go. That was meant to fix a data race. The race was fixed on 2026-09-27: every getter assembles under the engine's lock (`assemble`).

What the remedy would add now is eager construction. Every command, `status` and `queue` included, would set up the evaluator, the port index stager, and upstream discovery that it never uses. The lazy getters are what let a command build only what it reaches. The review found that the workspace registries built twice cost nothing, so there's nothing to share either. So it's taken off item 6, and the getters stay.

## Tests

- `TestAReleaseFoundAlreadyIsCheckedNotFoundAgain` runs against MacPorts. A release given isn't asked of upstream again, and one whose tag the Portfile can't name is refused.
- `TestOutdatedPortsArePreparedOneBranchEach` checks that the update prepares the release outdated found.
- `TestUpdateNeedsTheBranchCheckedOut` covers a release that isn't the version asked for, or isn't for a version update.
- `TestJSONEnvelopes` and `TestAPlansJSONSaysWhatWasAskedAndLeftOut` cover the plan's new fields.
