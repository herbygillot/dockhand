# status reads pull requests itself; one refresh shared with serve (v0.4.0 part 1, c; SQL-O8, part 1)

The person's "yes" to the coalesced roadmap's decision 6 (2026-10-13): a
plain `status` said "CI not read yet (dockhand status --refresh)", and
showed "MacPorts CI failing" only after `--refresh`. With the SQL study's
O8, the command package review's three pull-request enums, and the split's
7b row 7, the `refreshPullRequests` fallbacks.

- **The three enums in `model`**: `PullRequestState` (open, closed,
  merged), `PullRequestReview` (none, approved, changes-requested), and
  `PullRequestChecks` (none, pending, passing, failing), typed on
  `PullRequestObservation`; their reading sites in engine and command
  converted. Stored values are the same strings.
- **One observe-then-write refresh** (`RefreshPullRequests`): every pull
  request is read first, outside any transaction, as the store's rule is,
  and what was read is written in one `Update`, where each was its own.
  `observePullRequest` and `recordPullRequest` are the halves; rebase's
  single-branch `refresh` uses them too.
- **`RefreshResult{Branches, Refused}`**: a read the forge refuses
  altogether, a rejected or ended login, is `Refused`, said once by every
  caller, the store's error alone returned as an error. command's
  `refreshPullRequests` words it once (`refusedRead`) for status and
  clean.
- **status reads stale pull requests itself** (`readStalePullRequests`):
  where an open branch's pull request was never read, or was read longer
  ago than serve reads them (`engine.PullRequestRefresh`, five minutes,
  now serve's default too), plain status reads them first, within twenty
  seconds, saying what changed; one that fails is said in one line, "#N
  not read now, so as last read: …", and status goes on. `--attention`,
  for a prompt, reads nothing; `--refresh` reads whatever their age.
- **serve --drain names the pull requests it leaves unwatched**: "serve: 2
  pull requests open, which this drain leaves unwatched: #35353 and
  #35400; dockhand serve follows them".
- **Found on the way:** serve's notice of a login that can't renew itself
  was unreachable. The refresh returned at the first authentication error,
  which an ended login is, so serve said "could not read pull requests: …"
  and never notified. serve now reads the refusal for an ended login and
  says and notifies it once, as intended.

Still to do in this item: the one GraphQL read over every pull request,
which takes the read from about five requests a pull request to one for
all of them.

Tests: TestStalePullRequestsAreReadAndADrainNamesTheUnwatched,
TestARejectedLoginFailsTheReadOnce (Refused), TestServeSaysAnEndedLoginOnce,
TestStatusRefreshShowsWhatTheReviewersSaid (status's own read, and one
that fails).
