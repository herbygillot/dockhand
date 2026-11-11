# A modelled context takes no host answer: what the table lacks is absent, marked `closed`

A core item for v0.9.0, [#110](https://github.com/herbygillot/dockhand/issues/110), placed at the person's word ("I'm fine with A-F", 2026-11-10 13:17Z, in the architecture thread, on architecture's model-from-snapshots assessment, its item F). It joins two of architecture's designs into one step: the assessment's section 4, "Fail closed" (2026-11-10, read at main 2a472cde9), and step 2 of the modelled context on Linux (2026-11-07, read at main d6b2f506), "Off a Mac, the model is closed", made the rule in every modelled context, on a Mac too. The issue says where it stands; this file is the design.

Adjusted 2026-11-10, at the person's adoption of architecture's right-sizing of the modelled context (2026-11-10 17:07Z): the gate is lighter and #109 is no longer a prerequisite (below), and the rest of the Linux design (CI's newest as the home context off a Mac, the pins, the `[model]` section, the stated-context line, the weekly Linux survey) is cut until someone runs dockhand on Linux; its first question is answered by this design's rule, and the other two are moot. Off a Mac, `DefaultModel()` stays as it is: Linux's only consumer is dockhand's own tests, which need the same answers on any host, and this gives them that.

Revised 2026-11-10, at the person's word ("Revise all", 23:24Z, in the architecture thread), after Codex's design read of this file at main 9a0214ab and architecture's check of it: the rule is unchanged, and "The contract" below says exactly what closes, which the first writing left to the reader. Two of its sentences were wrong and are corrected: the switch is the context being modelled, not a call of `model_platform`, and a Base-owned read does leave a ledger entry.

## Why

Decision 1 of docs/oracle.md made a host answer in a modelled context a flag, `host-in-model`, "until phase 5 switches the toolchain source to `table`", and oracle.md marks phase 5 done, so the letter of the decision is already due. The verdict side is closed now: `Tolerate` (portedit/observe/host_access.go) treats `host-in-model` as inconclusive unless explained. What isn't closed is the value: the host's answer enters the evaluation (dispatcher.tcl:163, :187, :433). The ledger counts every such call (dispatcher.tcl:438), but a read no Portfile owns emits no `dockhand.host-access` event (observation.tcl:93), and the event is all the verdict reads, so a host value can land in a conclusive field unseen. The Intel-versus-model survey of 2026-11-09 found 4,670 of 6,114 SDK lookups with no `host_access` entry at all; for `xcrun --sdk macosx` on a Darwin 10 row the M1's host hands back its MacOSX27 SDK path into `configure.sdkroot`. Off a Mac it is worse: `file exists /usr/include` answers Linux's truth.

## The rule

A property of every modelled context: the dispatcher is told the world is closed, and a call that would reach the host (a host path's `file`, `open`, `glob`, `source`; any `exec`, the admitted `HostPrograms` included) is answered as an empty Mac answers it instead: `file exists` and its kin 0, `open` and `source` the no-such-file error, `glob` no match (the error without `-nocomplain`, as on a Mac), `exec` "couldn't execute". The ledger and the record mark it `closed`, a kind beside `host-in-model`, and the Go side treats `closed` as it treats `host-in-model`: inconclusive unless `Tolerate` explains it.

What this buys: no host fact ever enters an observation, Mac or Linux, so the same row gives the same answers everywhere, with no spawns, and one model instead of two. The ledger says which reads the model lacks, with no Mac behind it to hide them. A Portfile that `catch`es a probe takes its absent branch, which is what an empty Mac gives too. A Tcl error raised instead of an answer would reach the same `catch`, so an error buys nothing an answer doesn't, and an answer keeps the evaluation going to record the next read.

What runs in the master interpreter on the worker's behalf is closed too. The helpers toolchain.tcl aliases into a modelled worker from the master (`lookup_helpers`: `macports::get_tool_path`, `macports::get_compiler_version`, `macports::getportresourcepath`) are copied into the worker as the lookups are (`copy_procedure`), reading the context's caches there, so every `exec` they make is the worker's and the dispatcher answers it. As a backstop, under a modelled context the master's own `exec` is closed: it answers "couldn't execute" with the `closed` mark, so an alias this design didn't name can't reopen the gap. #106's host clang run, a version-cache miss that spawned this Mac's clang unmarked, is the case.

The Mac's own native context is untouched: it is the Mac, and reads the Mac.

## The contract

What "no host answer" means in the code as it stands, read at main 9a0214ab.

**What switches it on: the context being modelled.** That is `modeled || session_modeled`, as observation setup already decides it (observation.tcl:166). It is not a call of `model_platform`, which is made only off a Mac (evaluator.go:154), and it is not a toolchain being present, which is what today's marking tests (dispatcher.tcl:433, and `note` at :163 and :187). A release with no row, Darwin 9, has an empty toolchain and is modelled all the same.

**Which path is the host's: one reading, the verdict's.** `source_of` can't decide it. To it every `glob` is `enumeration`, every `exec` is `process`, a relative path is `relative`, and a root is matched as a prefix of the text (dispatcher.tcl:119 to :149). The verdict side already reads a path properly: `host_event` normalizes it in the worker's directory, lets Tcl's own library through, tests the normalized roots, and flags a final symbolic link (observation.tcl:36 to :56). Closure uses that reading, so the answer and the flag can't disagree, and the record's third reading (`kind_of` and `patterns`, dispatcher.tcl:196 to :228) is folded into it. Under it:

- A read stays open where it is of the captured Portfile and PortGroups, the tree's own files, or Tcl's and Base's libraries.
- A relative path is resolved before it is judged.
- A link at any step of the path that leaves the roots makes it the host's.
- A `glob` is judged by its `-directory`, its `-path`, and where its patterns start.

**`source` needs its own refusal.** It is traced, not dispatched, so that the Portfile's frame still owns the reads beneath it (observation.tcl:97), and it is not in the dispatcher's guard list (dispatcher.tcl:493). Closing dispatch leaves it open. The refusal has to keep those frames, and has to hold in a worker that isn't observed, where today `source` has neither dispatch nor trace (observation.tcl:171). Whether the trace's entry can refuse, or `source` has to be wrapped, is implementation's to try.

**The helpers bring their answers with them.** Today the three helpers are aliases into the master (toolchain.tcl:385), so they read the master's caches and nothing is wrong. Once they are copied into the worker, as this design has it, the two caches `ModelVariables` fills from the row, `compiler_version_cache` and `tool_path_cache` (platform.go:275, :285), have to come too. They are in neither list the copy reads (toolchain.tcl:347 to :351), and the caches that are listed start empty in each worker. Without them a copied helper misses an answer the table has, and closes it.

**The verdict needs a path that doesn't exist yet.** `Tolerate` reads flags and `dockhand.host-access` declarations and never the ledger (portedit/observe/host_access.go:151). A read no Portfile owns emits no event, and dispatch reports a host read to the observation only once the Portfile's `PortSystem` has returned (dispatcher.tcl:440). So "the Go side treats `closed` as it treats `host-in-model`" has nothing to read. A closed read emits its event whoever owns it, saying which of the Portfile, a PortGroup or Base asked, and `Tolerate` weighs it as it weighs `host-in-model`: one that reaches a fetch field leaves the context inconclusive, and a build-only probe can be explained.

## Order, and the gate

It comes after:

- [#101](https://github.com/herbygillot/dockhand/issues/101), a program neither the host nor the fresh installation has, which runs on the host today and is counted `host-in-model` (the false count). It is still to do: `run` answers "couldn't execute" only for a program this Mac has and a fresh installation lacks (dispatcher.tcl:449);
- [#108](https://github.com/herbygillot/dockhand/issues/108), the absent SDK answer where a row lists none, no spawn, marked. It is in (c5d4df440).

#109 (`java_home`, `uname -m` and the `sysctl` keys from the row) was a prerequisite and closes with the right-sizing: `java_home` reaches dependencies, not a fetch field, and `build.jobs` is build-only, so `Tolerate` explaining those reads, a small improvement, does what the facts would, and an absent JDK is what a fresh guest has.

The switch is gated on one before-and-after survey that counts outcome changes in update and checksums contexts, each read by its cause, since those are the plans a person accepts from the model. It reports three things: the contexts that move between conclusive and inconclusive, either way; the fetch plans whose values change while the context stays conclusive, which a count of the newly inconclusive would miss and which matter most; and evaluations that now fail. Its inputs are fixed and written down with the result, in this file: the ports tree's commit, Base's version, the rows, the variants, and the evaluator's settings, so the same survey can be run again. It is no longer gated on every unexplained host read being one the table lacks by design: every further survey finds new gaps, and almost none reach a fetch field. [#107](https://github.com/herbygillot/dockhand/issues/107)'s probe test, in v0.9.0 too, holds the platform variables and toolchain answers the model does give to real Macs; this item decides what it does with the rest.

The plan filed on 2026-11-11 takes #107 just before this item, since both change internal/macports/platform.go first. It takes one part of [#39](https://github.com/herbygillot/dockhand/issues/39) with it: the one test table for exec admission, which today is tested twice (macports/programs_test.go:69 and eval/refusal_test.go:73). This item changes what admission answers in a modelled context, so the table is written here, with the native context's answers and the closed ones as its rows.

## Decision 1, settled

Recorded in docs/decisions.md now, as adopted and not yet built: a modelled context takes no host answer; what the table lacks is absent and marked `closed`. The implementing commit gives docs/oracle.md's decision 1 its settlement, beside the comment at dispatcher.tcl:430 that cites it.

## Answered by the person

The Linux design's first question, whether the closed model answers as an empty Mac or raises an error: as an empty Mac, as this design has it (2026-11-10 17:07Z, with the right-sizing).

## Tests

The dispatcher's closed answers for each call kind (`file`, `open`, `glob` with and without `-nocomplain`, `source`, `exec` of a `HostPrograms` entry and of another), each with its `closed` mark in the ledger and the record, in a modelled context on a Mac and off one; the native context on a Mac still reading the host; the Go side counting `closed` as host state, `Tolerate` explaining a Base-owned read as it does `host-in-model`; a Portfile `catch`ing a probe taking its absent branch; every alias a modelled worker holds into the master is either a copied procedure or a read-only helper named by this design (an audit over `interp aliases`, so a new alias fails a test); a version-cache miss in a modelled context answers closed and never spawns, in the ledger and the record.

With the contract, these too:

- **A release with no row on a Mac.** A test pins today's behaviour for Darwin 9, the host answering (eval/host_access_test.go:68), and this changes what it asserts.
- **Paths:** an absolute and a relative path outside the roots; a link as the last step and as an earlier one; `glob` with `-directory` and with `-path`; and the captured Portfile, a PortGroup and Tcl's library still read.
- **`source`** of a host path refused, in an observed worker and an unobserved one, with the Portfile's frame still owning the reads beneath a `source` that is allowed.
- **A cache hit beside the cache miss.** The table's clang version and path are still answered once the helpers are copied; the tests that hold this today (eval/installation_test.go:183, :302) keep passing.
- **Contexts in turn in one interpreter:** native, modelled, native, and the reverse, with neither the closure nor a cache carried over. Each `Observe` starts its own interpreter today, so only tests share one, and an empty set of overrides doesn't undo an earlier one (observation.tcl:167).
- **The verdict:** a closed read Base owns that reaches a fetch field leaves the context inconclusive; a build-only one is explained; both keep their ledger entries.
- **Exec admission as one table** ([#39](https://github.com/herbygillot/dockhand/issues/39)'s first item): each program kind admitted, refused or closed, native and modelled, replacing the two copies above.

## Size and docs

About a fifth of a batch, Tcl and Go and their tests, plus the gating survey, inferred. It was a tenth before the contract was written out: the one path reading, `source`'s refusal, the helpers' caches and the event for a read Base owns are each work the first estimate didn't count. Usage docs: the paragraph on other macOS releases in docs/usage/hand-edits.md, which lands before this in its pre-#110 wording, gains its "never with this Mac's answer" clause in the implementing commit. The modelled-context notice a person sees is unchanged; `closed` appears in the record and the ledger only. docs/oracle.md's decision 1 paragraph gains its settlement; docs/decisions.md's entry moves from adopted to built.
