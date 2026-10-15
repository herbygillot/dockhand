# submit --rewrite-description

macports-ports #34756, the dockhand port, was updated to v0.3.0 through
dockhand (adopt, update, check-358, tidy, submit), but kept September's
description: its branch was adopted, so nothing recorded dockhand writing the
description, and submit keeps such a description as the person's. Its Tested
on named a v0.0.0 dockhand, Tart 2.37, and a build from 09-21. The person
chose a submit option over a hand edit (2026-10-15).

- `SubmitRequest.Rewrite`, `--rewrite-description`: the pull request's whole
  description is replaced with the one dockhand composes, the person's note
  kept, every section refreshed. The preview says "replaces its whole
  description with dockhand's (--rewrite-description)". Someone else's pull
  request is refused, as `--note` is; `--passing` doesn't take it.
- Once rewritten, the description is dockhand's again, recorded as written,
  so later submits refresh it as usual.
- TestAResubmitRefreshesTheDescriptionItWrote covers the person's edited
  Description replaced, the Type ticked again, and the next preview "current".
