# An update's commit names its upstream release, at best effort

2026-10-27, the docs dig's I11 (roadmap c5cc671d), at the person's "best
effort" and "just keep it minimal". An update's commit was its subject
alone, and a one-commit pull request's description is its commit's body,
so a reviewer looked up what changed by hand.

- Discovery keeps the release's page, where the forge's listing gave one,
  and the tag of the version it updates from, where the listing names it
  (model.Release's `Page` and `FromTag`).
- tidy writes an update's commit body from its release
  (`Release.CommitBody`, by tidy's `chainBody`): "Updates from 1.7.1.",
  then "Release: <page>", else "Changes: <compare page>" between the two
  tags on GitHub or GitLab. Where any of it isn't known it says what is,
  or nothing, as before; none of it can hold, refuse, or fail.
- A body makes submit's note an "Author's note" beside it, as with any
  commit body; TestSubmitNoteIsKeptWithTheBranch, about a commit with no
  body, now amends its commit to the subject alone.

TestAnUpdatesCommitBodyNamesItsRelease; the resubmit test's description
carries "Updates from 1.7.1.". design-v3's `r` key isn't built.
