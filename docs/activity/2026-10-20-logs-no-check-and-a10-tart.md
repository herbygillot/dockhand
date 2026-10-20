# logs with no check yet, and A10's Tart refusals

From the rc5 A10 rerun on the M1, which passed with no harm open.

- `logs <port>` in a worktree whose branch has never been checked said to
  name a check "outside its branch's worktree". `latestLogging`
  (command/queue.go) answered no both outside a worktree and in one with
  no check. It now names the unchecked branch, and logs says "<branch> has
  no check yet, so no log of <port>: dockhand check runs one". Outside a
  worktree the hint stays as it was. The roadmap's post-rc5 item
  (e066e851); there's no known-issues row, since nothing needs working
  around.
- A10 counts a refusal naming `dockhand setup tart` as run as written,
  listed apart in its pass line. Its grep wanted "…: dockhand setup tart",
  but the provider says "…; run dockhand setup tart sequoia". So the M1,
  which has only macOS 27's images, failed `check --on sequoia --on
  tahoe` and `check --on tahoe`, though both were refused rightly. A10
  also lists `logs` refused for want of a check as not run, as it does
  submit's, since it runs check only as its plan.
