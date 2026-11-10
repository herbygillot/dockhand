# A modelled context takes no host answer: what the table lacks is absent, marked `closed`

A core item for v0.9.0, [#110](https://github.com/herbygillot/dockhand/issues/110), placed at the person's word ("I'm fine with A-F", 2026-11-10 13:17Z, in the architecture thread, on architecture's model-from-snapshots assessment, its item F). It joins two of architecture's designs into one step: the assessment's section 4, "Fail closed" (2026-11-10, read at main 2a472cde9), and step 2 of the modelled context on Linux (2026-11-07, read at main d6b2f506), "Off a Mac, the model is closed", made the rule in every modelled context, on a Mac too. The issue and the roadmap's entry say where it stands; this file is the design.

The rest of the Linux design (CI's newest as the home context off a Mac, the `[model]` section and its line, the weekly Linux survey) isn't placed: it waits on the person's three questions to it, the first of which bears on this item (below).

## Why

Decision 1 of docs/oracle.md made a host answer in a modelled context a flag, `host-in-model`, "until phase 5 switches the toolchain source to `table`", and oracle.md marks phase 5 done, so the letter of the decision is already due. The verdict side is closed now: `Tolerate` (portedit/observe/host_access.go) treats `host-in-model` as inconclusive unless explained. What isn't closed is the value: the host's answer enters the evaluation (dispatcher.tcl:163, :187, :433), and for a Base-owned read (no Portfile frame) the ledger keeps nothing, so a host value can land in a conclusive field unseen. The Intel-versus-model survey of 2026-11-09 found 4,670 of 6,114 SDK lookups with no `host_access` entry at all; for `xcrun --sdk macosx` on a Darwin 10 row the M1's host hands back its MacOSX27 SDK path into `configure.sdkroot`. Off a Mac it is worse: `file exists /usr/include` answers Linux's truth.

## The rule

A property of every modelled context: when `model_platform` is called, the dispatcher is told the world is closed, and a call `source_of` would pass to the host (a host path's `file`, `open`, `glob`, `source`; any `exec`, the admitted `HostPrograms` included) is answered as an empty Mac answers it instead: `file exists` and its kin 0, `open` and `source` the no-such-file error, `glob` no match (the error without `-nocomplain`, as on a Mac), `exec` "couldn't execute". The ledger and the record mark it `closed`, a kind beside `host-in-model`, and the Go side treats `closed` as it treats `host-in-model`: inconclusive unless `Tolerate` explains it.

What this buys: no host fact ever enters an observation, Mac or Linux, so the same row gives the same answers everywhere, with no spawns, and one model instead of two. The ledger says which reads the model lacks, with no Mac behind it to hide them. A Portfile that `catch`es a probe takes its absent branch, which is what an empty Mac gives too. A Tcl error raised instead of an answer would reach the same `catch`, so an error buys nothing an answer doesn't, and an answer keeps the evaluation going to record the next read.

What runs in the master interpreter on the worker's behalf is closed too. The helpers toolchain.tcl aliases into a modelled worker from the master (`lookup_helpers`: `macports::get_tool_path`, `macports::get_compiler_version`, `macports::getportresourcepath`) are copied into the worker as the lookups are (`copy_procedure`), reading the context's caches there, so every `exec` they make is the worker's and the dispatcher answers it. As a backstop, under a modelled context the master's own `exec` is closed: it answers "couldn't execute" with the `closed` mark, so an alias this design didn't name can't reopen the gap. #106's host clang run, a version-cache miss that spawned this Mac's clang unmarked, is the case.

The Mac's own native context is untouched: it is the Mac, and reads the Mac.

## Order, and the gate

So that closing loses no coverage, it comes last, after:

- [#101](https://github.com/herbygillot/dockhand/issues/101), a program neither the host nor the fresh installation has, which runs on the host today and is counted `host-in-model` (the false count);
- [#108](https://github.com/herbygillot/dockhand/issues/108), the absent SDK answer where a row lists none, no spawn, marked;
- [#109](https://github.com/herbygillot/dockhand/issues/109), `java_home`, `uname -m` and the `sysctl` keys recorded per release and answered from the row (decision 18's remainder).

The switch is gated on a whole-tree survey whose unexplained host reads are only what the table lacks by design. [#107](https://github.com/herbygillot/dockhand/issues/107)'s probe-as-oracle test, in v0.9.0 too, holds the answers the model does give to real Macs; this item decides what it does with the rest.

## Decision 1, settled

Recorded in docs/decisions.md now, as adopted and not yet built: a modelled context takes no host answer; what the table lacks is absent and marked `closed`. The implementing commit gives docs/oracle.md's decision 1 its settlement, beside the comment at dispatcher.tcl:430 that cites it.

## Open for the person

The Linux design's first question: the closed model answers as an empty Mac rather than raising an error. This design takes that answer, for the reason above; if the person would rather it raised, the rule changes here and nowhere else.

## Tests

The dispatcher's closed answers for each call kind (`file`, `open`, `glob` with and without `-nocomplain`, `source`, `exec` of a `HostPrograms` entry and of another), each with its `closed` mark in the ledger and the record, in a modelled context on a Mac and off one; the native context on a Mac still reading the host; the Go side counting `closed` as host state, `Tolerate` explaining a Base-owned read as it does `host-in-model`; a Portfile `catch`ing a probe taking its absent branch; every alias a modelled worker holds into the master is either a copied procedure or a read-only helper named by this design (an audit over `interp aliases`, so a new alias fails a test); a version-cache miss in a modelled context answers closed and never spawns, in the ledger and the record.

## Size and docs

About a tenth of a batch, Tcl and its tests (the Linux design's estimate), plus the gating survey, inferred. Usage docs: none. The modelled-context notice a person sees is unchanged; `closed` appears in the record and the ledger only. docs/oracle.md's decision 1 paragraph gains its settlement; docs/decisions.md's entry moves from adopted to built.
