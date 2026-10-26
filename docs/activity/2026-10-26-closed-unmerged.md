# An update closed unmerged isn't proposed again

2026-10-26, roadmap ec97bb9a, the documentation dig's I1, harm by the
bar's letter, so v0.6.0 waits for it as rc5. `PlanOutdated` left alone
only a port an open branch changed; once a pull request closed unmerged,
the follower closed its branch, and the next day's look planned the same
version under a new name. Under `serve.ship_to = "submit"`, serve checked
it and opened a pull request again, undoing the maintainer's close each
day, up to `submit_limit`.

- `Engine.ClosedAt(port, version)` is the newest closed or archived
  branch whose recorded update took the port to that version, passing
  over an archived branch whose pull request merged.
- `PlanOutdated` skips such a port, saying so where it says its other
  skips: "1.8.1's pull request #123 was closed unmerged, in jq-1.8.1; a
  newer release lifts this" (`ClosedWords`). A newer version plans as
  before. Both callers, serve's look and `outdated`'s preparing, share
  it.
- An attended `update` or `bump`, which don't plan through it, goes
  ahead and says "Closed before: …" (Update.ClosedBefore).

TestAnUpdateClosedUnmergedIsNotProposedAgain prepares jq's update as
serve does, closes its pull request unmerged, and reads that the next
plan skips it with those words, serve's submit pass has nothing, a newer
release plans it, and ClosedAt finds the closed branch.
