# Every pull request in one GraphQL read (v0.4.0 part 1, c; SQL-O8, part 2)

The second half of "status reads pull requests' CI itself": the roadmap
asks the read to be one GraphQL query over every pull request, the
pull-request half of Later's "Spending less of GitHub's allowance". Each
pull request took about five REST requests (the pull request, then for an
open one the pull request again, its reviews, its check runs, and its
commit statuses).

- **`forge.PullRequestReader`** reads many pull requests at once, each a
  `PullRequestReading`: Found, Partial, the pull request's state and head,
  and its status. A forge that can't is simply not a reader, as one that
  can't inspect isn't an inspector.
- **GitHub's** (`forge/github.ReadPullRequests`) asks GraphQL for 25 pull
  requests a request, each by an alias: state, draft, head, the latest
  opinion of each reviewer (`latestOpinionatedReviews`), and the head's
  `statusCheckRollup` contexts, check runs and commit statuses both,
  counted as `Inspect` counts them (action_required waiting, neutral and
  skipped passing). A pull request GitHub hasn't comes back null with a
  NOT_FOUND error at its path, read as not found; any other error is the
  read's. One with more than 100 reviews or checks is Partial.
- **The refresh reads them so where it can** (`engine.readPullRequests`):
  one read over every branch's pull request, with `observePullRequest`,
  the REST read, for a Partial one, for every one where the forge can't
  read many, and for every one where the read over all fails but for a
  refusal. GitHub's GraphQL API takes only a signed-in request, so with no
  login the public pull requests are read a request at a time, as before.
  `observation` turns either reading into what the branch records.

Tests: TestPullRequestsAreReadAtOnce (the query, the counting, not found,
partial), TestPullRequestsAreReadAtOnceWhereTheForgeCan (the refresh reads
once, reads a partial one alone, and says one not found).
