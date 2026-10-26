# rc2's quick stage: the harness's grep, and D-N6's one request

2026-10-26, from the M1's quick stage on rc2 (54 rows: 46 passed, E5
and E8 refused well, 3 not run by design, 3 failed, no harm tripped).
Its three findings are fixed before rc3.

- **C13 failed on a plan that did what it should.** Its plan exited 0 and
  moved opkssh 0.16.0 to 0.19.1. The row's `plan_words … | grep -qE …`
  ran under run.sh's `set -o pipefail`: grep -q exits at its first
  match, the plan's printer dies of SIGPIPE, and the pipeline fails as no
  match would (5 of 5 on the M1). Where a match means a failure, as in
  C2, C3, and C4, a long plan could pass a real failure silently. Every
  pipelined `grep -q` in the rows and lib, 51 of them, is now `matches`,
  common.sh's grep that reads all its input.
- **D-N6's second look counted 1 request GitHub didn't charge.**
  X-RateLimit-Used stood at 130 after both looks. The one was outdated's
  look at GitHub's rate_limit endpoint (engine.outdated's Allowance),
  which GitHub's documentation says costs nothing of the allowance; the
  spend counter counted it as a request. It no longer does
  (github.freeOfTheAllowance), so a look's count is what GitHub charges.
- **C15: submit's preview didn't say what serve would hold** (roadmap
  a2e4b53b). The plan had the majors, floors, and `after` holds, and so did
  the JSON's concerns, but the text showed only a moved tag's, under
  Upstream. `after` binds serve alone by design, so the preview holds
  nothing: under a `Serve` label it says "would hold it:" and each hold's
  own words, once each (view.ServeHolds), and the submission goes ahead as
  before. C15's row stands as written.

## The full stage on rc2

The M1's full stage on rc2 (75 passed, 2 refused well, 13 not run, 4
failed, no harm): C15 and D-N6 were the quick stage's, fixed above.

- **C11, C12, and C13 plan against live master,** where
  git-credential-oauth is at 0.18.0 already (44bbe1c, 10-23), so C11's
  plan rightly said nothing to change, and failed. Each now says "not
  run" where its port is current (plan.sh's `plan_current` and
  `plan_not_due`), as B4's setup does. The grep -q under pipefail it
  also named was fixed in aafea625.
- **A4** passed on the product (sandbox #91), but looked for the
  README's old heading, "An update, start to finish"; it reads "Make
  your first update" now.
- **The README's known issues** are v0.6.0's: the rows v0.6.0 fixes go
  (the signed-out limit, the reset collaborator's commit, a project that
  moved, the doubled trailer, the silent check off a terminal, the
  rejected login's words, and setup under a locked Keychain), the two
  still true stay, and the status --all row the roadmap placed joins
  them.
- **D-N2** wasn't run, ACCEPT_DN2_PORT unset. Its fixture, a distfile
  gone upstream with a newer release, needs a test repository whose
  releases the harness deletes and re-tags, which the roadmap leaves to
  the person (Later's fixtures for D-N2 and D-N5).

## D-N6 on rc4

rc4's quick stage counted 8 requests for the first look and 1 for the
second, the cache holding 8 entries the second rewrote none of. Traced
here with a scratch build logging each request: two looks over jq,
ripgrep, and lazygit, and over the quick stage's go-critic, dust, and jq,
each second look's every read a 304, counted 0, so the M1's one isn't
reproduced; X-RateLimit-Used drifted by one or two between looks without
a charged request, so it doesn't name one either. Each dockhand's spend
line now names the requests its cache didn't answer, "GET /repos/…/tags
404", as a fourth field (github.recordCharged, from the conditional
transport), and D-N6's failure quotes the second look's, so the next
stage says which it is.
