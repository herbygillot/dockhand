# 2026-09-22, evening: the open items and the roadmap against the code

A pass over everything the day's notes and the roadmap carried as open,
each checked against the tree at `9b00714d`, with the whole-tree survey
rerun at the baseline's commit by a second agent to measure the day's
coverage work as a whole. Where the check found something to do, it was
done in this pass; where it found a claim stale, the roadmap says so.

## Done in this pass

- **Dead code.** `make deadcode`, clean after 2026-09-17 and due again
  at a milestone, named five unreachable functions: the day's own
  `Engine.publicationDestination`, orphaned when the contribution owner
  took over publication destinations; the recognizer's `pureCondition`
  wrapper, orphaned by the effect rule; and three older ones,
  `Journal.Held`, `NewContext`, and the cycle's `due`. All five are
  removed and the check is clean.
- **The roadmap.** Every section reread against the code. The Next
  queue's first item is built through the effect rule and records the
  one decision left in it. The carried paragraph no longer carries the
  contribution owner, the `portedit` session, or the smaller items that
  were done, and names the three that stay as notes. The coverage
  paragraph's numbers are the survey's, below.

## Open, and why

- **The `exec` policy.** 180 Java ports stop at `exec
  /usr/libexec/java_home -V` inside the PortGroup's JVM discovery. The
  refusal names it; admitting an `exec` of a literal system program whose
  output is only captured is a policy decision, recorded as such on the
  roadmap.
- **Next items 2 through 5.** Nothing to fetch, composed versions, host
  reads that only decide a note, and the PortGroup inclusion map: none
  started, each checked for a partial start and found none. On item 4
  the observer already tolerates `notes-append` and its kin as benign
  sinks, so php's stopped subports are stopped by something the read
  reaches beyond the note; the survey's php lines are the next thing to
  read before it is sized again.
- **Multi-target contributions.** Agreed 2026-09-21, unbuilt, and the
  item the command pipeline over the `r.build` prologues waits on.
- **Behind a measurement.** The workspace design's interposer proof and
  blob store; the `portedit` package split now that the session exists;
  the state-boundaries review's narrower reads; the survey's lost cores.
- **Carried from earlier reviews.** The cycle's sequential long
  operations and the execution updater's reflection; the instance
  coordination foundations; expiring credentials; the dependent
  verification follow-up; the three needs-design entries, review
  authority, permissive human-edit capture, and evidence across
  registrations, each still awaiting the decision it names.
- **Notes.** The category enumeration in `eval.Resolve`, reachable only
  through the bare evaluator; `workspace.Adopt`, a constructor for tests;
  `Engine.PreparationInput`, a wrapper for one test; the reader's
  `staged` map, waiting for a second staging site.
- **From the exercise runs.** Whether t7063 fails on rc2 in the VM, and
  whether #34835 stays open; both are the maintainer's.

## Closed by the other session

The fixture-program helper the text-file-busy note left open across
fourteen packages landed as a shared test support package
(`cbe88cbf`), and its own test-fixture hygiene commits beside it.

## The survey

The whole tree, 41,733 ports, assessed at the baseline's commit
`842164fd` by a second agent with the binary at `9b00714d`, journaled to
`2026-09-22-effect-rule.jsonl` beside the earlier surveys, outside the
checkout, and compared port by port with the rerun of that morning.

| | before | after |
|---|---|---|
| input-found | 39,283 | 39,459 |
| unknown | 969 | 1,047 |
| unsupported | 1,481 | 1,227 |
| Ports refused for a hook and accepted now | | 272 |
| Ports with a worse outcome than before | | 0 |
| Ports guarded before and not now | | 0 |
| Ports whose guards changed | | 155, every one an addition |
| Wall time | 48m49s | 69m40s |
| Cores, user and system time over wall | 8.4 | 7.2 |

The compatibility claim holds across the cohort: no port came out
worse, no guard was lost, and the 155 changed guard lists are hooks
recognized beside ones already recognized, the MPI family's second hook
among them. The day's grammar work moved 176 ports from unsupported to
input-found and 78 to unknown, where the fetch is no longer what stops
them. Unsupported ports with a fetch problem are 213, from 427 at the
baseline: 180 behind the Java PortGroup's `exec`, 77 with a post-fetch
hook, 14 with a custom fetch procedure, eight Go ports off github.com,
five R ports with an unbraced condition, one `bun` port.

The wall time is the one number that went the wrong way, and it is not
explained. The parallelism the roadmap already carries as unmeasured
fell again, from 8.4 cores to 7.2, and the system time, 13,957 seconds
against 16,096 of user time, is heavy for what the walk adds, a few
string lookups per hook. The desktop was busy through the run, and the
survey ran under a second agent while this session ran the test suite
and a dead-code sweep, so the machine is a candidate as much as the
code; the profile the roadmap asks for is the way to know, and it is
now the first thing to run before the next survey.
