# The oracle: scope for the local Mac work

Roadmap step 8. This is the scope settled on 2026-09-25, for a session on a
Mac with MacPorts Base, where the modelling data already collected lives.
Every phase changes the evaluator's Tcl and is proven by a survey, so none of
it can be built or checked in a Linux container.

Sources: the direction record's "MacPorts in a controlled room" and decisions
9–13, 17, 18, and 35 ([contracts direction](reviews/2026-09-23-contracts-direction.md)),
its 2026-09-23 host inventory (`~/.dockhand/surveys/2026-09-23-host-inventory/`
on the Mac), and the [Base design prospective](macports-base-design-prospective.md)
§§4–6.

## What it is

MacPorts Base stays the interpreter. In the worker, every command that can
reach the host is hidden with `interp hide` and aliased to one dispatcher:
`exec`, `file`, `open`, `source`, `glob`, `readdir`, `::env`, pextlib, and
`registry::`. The dispatcher knows the context and answers from exactly one
source:

- **the captured tree**, for reads inside the port tree;
- **the host**, in native contexts only, for reads and a table of read-only
  programs;
- **the platform model**, in modelled contexts;
- **refusal**, for writes, network, and programs not in the table.

It records each answer with its source. A refusal goes into a ledger
outside the Portfile's control flow, which `catch` can't erase, and it
makes the context inconclusive rather than guessed.

It replaces four mechanisms:

- the platform variables overridden after init (`platform.tcl`);
- the execution traces that record only with a Portfile frame on the
  stack (`observation.tcl`);
- the Linux model's stand-in `file`;
- the hook grammar's own program list. Hooks stay judged statically
  (decision 17), but the grammar and the dispatcher share one table.

## Why v3 needs it

1. **Plans the builder would make.**
   - Modelled contexts take this Mac's toolchain; Base asks for it in
     about 22,000 ports.
   - Native contexts read the person's registry and prefix, where a Tart
     guest or a GitHub runner starts from an empty prefix.
2. **Reuse (step 9).** Per-target reuse is keyed on recorded reads. Until
   every read passes one dispatcher, all of `_resources` counts as read.
3. **Safety.** Top-level Portfile code runs `exec` on the host today.
4. **Coverage.** 674 ports read host state; about 275 are worth
   modelling, pyqt5 first.

## Phases

Each phase is proven by `assess --all` over the tree, about 41,700 ports,
compared with the previous baseline.

| # | Phase | Changes results | Acceptance |
|---|---|---|---|
| 1 | Dispatcher in shadow mode: hide and alias, pass the host's answers through, record each with its source. It replaces the trace-based observation. | No | The survey matches the baseline; the recorded reads match the 2026-09-23 inventory; the time cost is measured. |
| 2 | Effects enforced: refuse writes, network, and unlisted programs, with an allowlist of about 20 read-only programs shared with the hook grammar. The ledger survives `catch`. | Barely | Each newly inconclusive port attempted a refused effect. |
| 3 | Workspace rule (decision 35): a read inside the tree materialises on demand, and a read outside it is refused. | No | Evaluation reports the complete set of files it read. |
| 4 | Closed installation: an empty registry, prefix files absent, `PATH` as MacPorts' build environment. | Yes, in native contexts (`qt5_version_info` alone touches 713 ports) | Every changed result is explained. |
| 5 | Toolchain from the facts table (step 6): Xcode and Command Line Tools versions, developer directory, compilers, and SDKs per release. Replaces `ModelVariables`. Adds the two-outcome `java_home` answer (decision 18). | Yes, in modelled contexts | The 180 Java ports come in, confirmed by the survey. |
| 6 | Bootstrap under the environment contract: `mportinit`'s `sw_vers`, `sysctl`, `xcodebuild`, a disposable `portdbpath`, and a `HOME` dockhand owns. May belong to step 6. | Possibly | — |

Out of scope:

- resisting malicious Portfiles, which is an OS sandbox and a separate
  decision;
- dockhand's own Tcl evaluator, rejected;
- running hooks inside the oracle, dropped by decision 17.

## Open decisions, for the Mac session

1. **Modelled toolchain questions before the facts table.** The proposal
   is to let the host answer them, tagged `host-in-model` in the ledger,
   flagged but not refused, so phases 1–4 lose no coverage while phase 5
   waits for step 6. Refusing them instead would leave about 22,000
   ports inconclusive until the table exists.
2. **Phase 4's behaviour change.** `update` on a Mac would plan against an
   empty prefix, as CI and Tart see it, whatever is installed there.
3. **How far "inconclusive" blocks.** The proposal follows the
   prospective's §4: it blocks only the edits that depend on the
   unresolved field. A refused `java_home` blocks a Java port's
   checksums, but not its revbump.
4. **Performance.** The trace cost 28%: 62.6 minutes against 48.8
   untraced. The proposal is at most 10% over untraced for shadow mode.
5. **Proving phases without a person at the Mac.** A self-hosted runner,
   or a GitHub macOS job that installs Base and runs a sampled survey,
   would let each phase be checked in CI.

The modelling data already gathered on the Mac should settle 1 and 4, and
size phase 5, before phase 1 starts.
