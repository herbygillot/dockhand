# 2026-09-22: evidence is worded once

## What the review measured

Three renderings of one set of facts: `renderStatus` walked a job's
evidence by hand, the GitHub run, its jobs, the environment, the developer
tools, the test policy; the pull request's Tested on section walked the
same evidence in `publish/body.go`; and the completion summary walked it
a third time for its one line per attempt. Each was maintained
separately, and the pull request's was reshaped on 2026-09-20 while the
other two were not.

## The projection

`workflow/view` now words evidence once. `Facts` turns a recorded
`Evidence` into what a reader needs: the verdict and when it was
observed, the provider, the workflow run with its outcome and jobs when
it was one, the admitted environment with its identities, image, and
pristine state when it was a local build, the components the build ran
on in table order, macOS, the developer tools, MacPorts, dockhand, the
failure with its phase, package, detail, and the mirrors tried, and the
first log's location. `Verification` adds the attempt's platform;
`AttemptWords` is the summary's line, verdict or progress on the
platform with the failing phase and the log; `Platform` words a platform.

Status prints the projection's fields in its layout, the summary prints
`AttemptWords`, and the pull request body writes the component table from
`Components` and its provider block from `Workflow` or `Environment`. The
words, the tables, and the pinned tests are unchanged; what changed is
that the body no longer knows how a guest reports Xcode, and status no
longer knows that a run without a conclusion is worded by its status.

## Left

The command pipeline over the `r.build` prologues, which the review put
after multi-target contributions settle the command surface, and the
intake move into `workflow`, which waits for the rules to stop moving.
