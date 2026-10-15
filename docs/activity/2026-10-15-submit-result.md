# submit's result names who requested changes, and what it rewrote (v0.4.0 part 1, c)

The Submit group's first item, the rc8 full stage's B4, with the command
package review's `Submitted.Edited`; both change `applySubmit` and its
result.

- **`changes_requested_by` in submit's JSON.** B4 updated #17 in place,
  and "@… requested changes; ask them to review again on GitHub, or set
  submit.rerequest_review = \"always\"" went to the text alone. The result's
  `pull_request` now names each reviewer GitHub last reported as requesting
  changes on a pull request that existed, `{login, rerequested}`;
  `rerequested` is true for those `RequestReview` asked. The result is
  emitted once the submission is made, however the question after it ends
  (a deferred emit), so an error asking the reviewers still leaves the
  envelope saying what was pushed. The text offer stays on stdout.
- **`Submitted.Edited` from `ApplySubmit`** (`engine.SubmitEdits{Title,
  Body}`): the engine says what it rewrote of an existing pull request, and
  `updatedWords` reads it, where it repeated the engine's title and body
  comparison. The JSON carries it as `updated_title` and
  `updated_description`.
- usage.md says both.

Tests: TestSubmitAsksTheReviewersBack (the JSON without a terminal, where
nobody is asked, and with rerequest_review = "always", where the reviewer
is), TestSubmitNoteIsKeptWithTheBranch (`updated_description`).
