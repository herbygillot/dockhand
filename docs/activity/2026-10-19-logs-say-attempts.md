# logs says each environment's attempts

The roadmap's v0.5.0 part 1 item 5, its third line, from the exiv2 field
notes: exiv2's check-448 had an empty `tart-macos27-arm64-1/` beside
`-2/`, a port's log said neither its environment nor its attempt where
there was one environment, and a failed check said only `dockhand logs
check-N`.

- A port's view (`logs check-N <port>`) always opens with which build it
  is (`writePortLogHeading`): "jq's log on <environment>, attempt 1 of 3,
  run <id>: failed at install; ~/.dockhand/logs/check-3/...log.gz", with
  "attempt N ended: <why>" for each earlier attempt in that environment,
  and the port's other builds after it, each with its attempt. It goes to
  standard error where the log is printed whole, as before, so the log
  stays as it is.
- A failed check's report lists "Logs:": each failed, interrupted, or
  tests-failed target's log by environment and attempt, .gz once
  compressed, and each attempt tried again with why (`writeFailureLogs`).
- Tart's provider fetches the partial log of the target a lost guest was
  building, within half a minute, where the guest still answers (its
  program stopped or errored), and records it with the interrupted result
  (`partialLog`); a stopped VM's isn't asked for.
- The runner removes an attempt's log directory where it's empty after
  the provider returns (`os.Remove` takes only an empty one).
- Tests: the Logs block and the heading in `TestCheckRunsHereWithoutServe`,
  and the partial logs in `TestTheTargetALostGuestWasBuildingIsInterrupted`.
