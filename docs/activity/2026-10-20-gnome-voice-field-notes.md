# The gnome-voice session's field notes, coalesced

On 2026-10-20 the person passed on the notes of another session that used
dockhand to troubleshoot gnome-voice, a port already broken on master. Field
testing added its own take the same minute. As the coalescing rule asks
(2026-10-19 18:45Z), each item was checked against every section of the
roadmap.

What worked, in the session's words: `check --also gnome-voice` built an
unchanged port from master, with `--plan` showing what would build;
`logs check-482 gnome-voice` started at the port's own build, after nearly
200,000 lines of dependency installs, and listed where each step starts,
"the most useful feature I used"; `--replace` said clearly that it had
cancelled the queued check, and the "Next:" hints were good.

## Where each goes

| | Friction | Already placed | Placed |
| --- | --- | --- | --- |
| 1 | status is very wide: one long "Needs you" message sets every row's width, and the commands end near column 400 (field testing sees the same with stale checks) | No | Next, a fix for main: the command goes on its own line, and a message wraps at the terminal's width |
| 2 | `check -d` with no serve queued the check and said "Start dockhand serve to run it."; `--replace` was needed to run it here | Yes: part 1, item 7, "A long check without a terminal" (the exiv2 notes' 7) | Item 7, cited as the second case |
| 3a | `logs check-482 gnome-voice` during the build said "recorded no log", which reads as a failure | Only as the README's known issue | Next, a fix for main: it says the target is running, and what it's doing |
| 3b | No way to follow a running check's log | No | Item 7, "Following a running target's log": the driver copies the log to the host as it grows, and `logs --follow` prints it; about half a batch |
| 4a | Kept archives reused for the 11 direct dependencies alone; indirect ones fetched again, 4 built from source | No | Next, a fix for main, if check-482's journal shows one of the 4 had a kept archive: `dependencyInstalls` (engine/reuse.go) offers direct dependencies and an earlier result's active ports, so a target's first check misses its dependencies' dependencies |
| 4b | Say up front how much a check will install or build | Partly: Decided, "Saying which dependencies build from source", at the guest | Later: the plan's count and a packages.macports.org hint, waiting since item 7's `check -d` takes most of the need away |
| 5 | `port lint` reported 0 warnings on trailing spaces | Not dockhand's | Nothing |

Not taken: a cache on the host of MacPorts' own archives, which a guest
fetches again each check, until a log shows fetching them is what takes the
time.

## The size

Item 7 goes from about 2.5 batches to about 3, so part 1 is about 12.5 and
v0.5.0 about 28.5 to 32.5. The three fixes for main are a few lines each and
can go while v0.4.0's tag waits.
