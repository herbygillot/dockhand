# status: latest_check while a check runs, and the files a check included (v0.4.0 part 1, c)

The Status and outdated group's last two items.

- **`latest_check` while a check runs** was null, which read as no check.
  It is the branch's newest finished check, and a check queued or running
  is among `active_checks`: the field's documentation says so
  (`branchJSON.Latest`), and usage.md's Scripting section. Naming the
  running check there would have made `latest_check` two things, finished
  and not; the roadmap offered either.
- **A check's `--include` counts when status compares files.** A check
  that passed with an untracked patch included read "files have changed
  since" in status, and asked for another check of the same files: the
  comparison left out what the snapshot included. `compareIncluded` takes
  the untracked files the latest snapshot holds, adds them to the files as
  they are now, as a capture does (`WithFiles`), and where that is what
  the check checked, the check is current, with `BranchStatus.Included`
  naming them. status says "passed for snapshot N, with
  textproc/jq/files/fix.diff included, untracked", and `--json`'s
  `latest_check` has `included`. An included file changed since is files
  changed since, as before.

Tests: TestJSONEnvelopes (null `latest_check` and the check among
`active_checks` while it waits), TestStatusComparesTheFilesACheckIncluded.
