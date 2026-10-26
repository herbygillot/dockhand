# rc2's quick stage: the harness's grep, and D-N6's one request

2026-10-26, from the M1's quick stage on rc2 (54 rows: 46 passed, E5
and E8 refused well, 3 not run by design, 3 failed, no harm tripped).
Two of its findings are fixed before rc3; C15's, submit's preview not
showing an `after` hold, went to prioritization as a design question.

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
