# The exiv2 session's notes, whole

On 2026-10-21 the person passed on the exiv2 0.28.9 session's notes in full: twelve frictions, two minor notes, and what it hadn't tried. They were written at v0.4.0-rc4, main at 0a4a05ea. Eight of them reached the prioritization thread on 2026-10-19 and were placed then ([note](2026-10-19-exiv2-field-notes.md)). As the coalescing rule asks (2026-10-19 18:45Z), each was checked again against every section of the roadmap and against main at c643559a; nothing was run. Docs only.

## Where each goes

| | Friction | Already placed | Placed now |
| --- | --- | --- | --- |
| 1 | A failed test can't be diagnosed from the log | Part 1, item 5, done | Nothing |
| 2 | No way into the guest after a failure | Part 1, item 7: `check --keep` and `shell` | Nothing |
| 3 | A test-only edit rebuilds everything | Not taken, with reasons; `shell --test` in item 7 | Nothing |
| 4 | A dependency's failure is reported as the port's own | No | Next, a fix for main: a phase of its own, `dependencies`, with MacPorts' line naming the dependency beside it. Part 2's standing acceptance records the dependency and its version |
| 5a | Logs are mostly the dependency install | Item 5, done: no `-d` where nothing builds from source; `logs --phase` in part 2 | Nothing |
| 5b | Logs aren't named by port in a multi-port check | Partly: item 5 has `logs` print each port's log path | Next, a fix for main: each log named by its target |
| 6 | The log directory's second attempt is unexplained | Item 5, done | Nothing |
| 7 | One diagnostic check grows submit's required environments | No | Next, a fix for main: outside build.default's profile, a target is required only where a check planned it |
| 8a | Columns for releases never checked | Next, done (6500fcde, c643559a) | Nothing |
| 8b | Cells from different checks, unmarked | No | Next, a fix for main: on the terminal, a cell from another check names it |
| 9 | `body-wrap` checked only the tip commit | No | Next, a fix for main: every commit is read already, so the likely cause, inferred, is the "://" exemption; reproduce first |
| 10 | The `--baseline` hint for tests master doesn't run | Item 5, done | Nothing |
| 11 | No long check without `serve` | Item 7: `check -d` starts `serve --drain` | Nothing |
| 12 | Concurrent instances checked by hand | Item 7: status lists dockhand's processes | Nothing |
| m1 | setup's stale-image hint doesn't say whether it matters | No | Not taken |
| m2 | The macOS 12 image's Xcode is too old for libgcc15 | Part 2's standing acceptance (the person's choice, 2026-10-18) | Nothing more |
| | Not tried: one branch on top of another | No | Later |

## Pushing back

- **Pinning or narrowing the required set (7).** The required set is build.default's profile (D17), and narrowing it for a branch lets it skip what submission needs. What grew the set was one rule, `evidence.Plan` taking every check's environments as required of every port. Once only what a check planned binds outside the profile, nothing accidental is left to narrow. The suggestion to say which check set a requirement is kept where one still exists: a port a check planned there and didn't finish.
- **body-wrap reads every commit (9).** `CheckCommits` loops over each commit for submit, tidy, and review alike. What it doesn't flag is any long line holding "://", where the rule's own text spares only a line that is only a URL. That's the likely miss, but the libextractor commit isn't on GitHub, so implementation reproduces it first.
- **An image problem said as one (4).** dockhand can't tell a refusal of the image from any other early failure in general. The dependency named, with MacPorts' own words ("incompatible Xcode/CLT version"), says it; part 2's standing acceptance then makes it a kind a person accepts once.

## Not taken

- **Whether a stale image's MacPorts matters for a check (m1).** That depends on what changed in MacPorts base between the two releases, which dockhand doesn't read. The plan already says the image is behind and that this Mac's MacPorts reads its ports, and `setup tart <release> --rebuild` is the remedy it names. Since 2026-10-16 the Xcode images all have 2.12.7.
- **A setup warning for libgcc15 (m2).** setup can't know what a person will check. The check says it where it matters, through note 4's fix, and an Xcode 14.2 image was declined on 2026-10-18, since it would stop matching the builder.

## The size

v0.5.0 stays about 28.5 to 32.5 batches: the five fixes for main count in none of its batches, and part 2's standing acceptance needed the failing dependency recorded anyway. The fixes can go while v0.4.0's tag waits. The largest is note 7's, in evidence, a few dozen lines with its tests.
