# check follows a check already running for the same files

2026-10-27, the docs dig's I10, its first part (roadmap 93f0e944). A check
asked while the branch's check of the same files was queued or running
refused, "check-42 is already running for these files; dockhand wait
check-42 follows it", for the person to type what it printed.

- check now follows it, as wait does, saying "check-42 is already queued
  for these files; following it.", and ends with its result: with serve
  running it observes, and with none it runs the check here. With `-d` it
  still refuses, leaving the check as it is. A stopped one, and one for
  other files, are refused as before, naming `wait` and `--replace`.
- wait's following moved into `checkrun.Wait`, which wait and check share.
- check's help says so.

TestCheckFollowsACheckOfTheSameFiles; TestCheckRunsHereWithoutServe's
refusal beside a queued check now asks with `-d`.
