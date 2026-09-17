# Interface pass, step 3: the per-command minimum

Per the [output design](../output.md), the info level now prints only what a person needs to follow a command; the full record moved behind `-v`.

## Action results

`bump`, `bump-revision`, `refresh-checksums`, `verify`, `publish`, `wait`, and `cancel` ended with a full `status` dump: every identifier, timestamp, policy, environment digest, and resource. They now end with `renderSummary` (`internal/cli/summary.go`), one short block per job:

```
jq: 1.7 -> 1.8.1; verified
  branch: dockhand/bump/jq-1.8.1
  passed on macOS 26 arm64
  PR https://github.com/macports/macports-ports/pull/34721 (created)
```

The headline is the port, what the job does (the version move, "revision bump", "checksum refresh", "verification", "publication"), and a state word: queued, preparing, verifying, publishing, verified, branch ready, published, no update needed, failed, needs attention, canceled. Then the branch (or the unconfirmed candidate branch), patch problems, the prerelease warning, one verdict line per platform with the failing phase and the log location, the PR with created or updated, and the job's detail when it stopped. Work still pending says how to resume by port name; the job ID form remains only when a job has no target. `-v` prints the old full record, now including the same resume guidance. `status` itself is unchanged until the contribution projection lands.

## Verdict words

`assess` prints "ready" and "candidate ready" for `input-found` and `candidate-checked`, in the port line and the count line; `outdated` prints "update available". The codes stay in JSON.

## Previews and contributions

`--diff` previews print one header line, `jq: 1.7 -> 1.8.1`, in place of the "Release:" line; the tag, upstream commit, and archive listing moved to verbose reports. Revision and checksum previews say "revision bump" and "checksum refresh". `abandon` and `refresh` print the port and branch with the disposition, the PR and its observed status, and the detail; the contribution ID joins at `-v`.

## Friction items closed

From the [new-user exercise](../reviews/2026-09-16-new-user-deno-exercise.md): item 10 (the "input-found" headline) and item 22(a) for action output (the ID soup at the end of a run). Item 26's "full status dump" closure is now the summary; the full dump is one `-v` away. Item 22 for `status` itself waits for the projection.
