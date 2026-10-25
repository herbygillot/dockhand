# Spending less of GitHub's allowance on a look

2026-10-25, Then's item 5 (from Later, the reflections' G). The M1's run
at 1da4fdbf spent 2,166 requests in 267 seconds on one `outdated --mine`
over 835 ports, and a second look spent as many.

- **Conditional requests.** github `ConditionalCache` keeps each read's
  answer with its ETag, by the request's address, what it accepts, and a
  digest of its credential, so one login's answers are never another's.
  The client's `conditional` transport, below the credential, sends a read
  asked again with If-None-Match, and answers GitHub's 304, which counts
  nothing against the hour, with what was kept, the 304's rate-limit
  headers laid over it. Answers are kept for the process and on disk, in
  `dockhand/github` in the user cache directory (`DOCKHAND_GITHUB_CACHE`,
  kept in serve's agent too), so the next command's look is nearly free.
  An answer over 4 MiB isn't kept.
- **The spend counts what's spent.** github `Spend.Requests` leaves out the
  reads answered on a 304.
- **Asking first** was already there: `outdated` weighs its estimate
  against what's left of the hour (`allowLook`, `GitHubAllowance`).
- **Measured** with a scratch build against GitHub: `outdated` over ten
  ports spent 20 requests, and the same look again spent 1, GitHub's own
  count (`X-RateLimit-Used`) unchanged between them.
- **Left for later:** GraphQL's batched tag and release lookups across
  repositories, which would cut the first look; the done line, a second
  look within the hour spending a tenth of the first's, is met without
  them. The cache isn't swept by cleanup yet; it's disposable.
- **Test:** TestAReadAskedAgainCostsNothingWhereNothingMoved.
