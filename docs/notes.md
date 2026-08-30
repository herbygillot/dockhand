# Notes

Working notes moved out of the README, which is now an introduction for
newcomers rather than a status page. These are a point-in-time snapshot
of where the implementation stood and what remained unsettled; unlike
`decisions.md`, nothing here is maintained. Read it as a record, and
check the code before trusting any claim in it.

## Implementation state, as of 2026-08-28

**The core loop runs.** The design's central claim executes end to end
on real ports: snapshot through `port-tclsh`, locate the value's span in
the CST, edit by byte-span replacement, re-snapshot, and the observed
`Delta` equals the predicted one exactly — through both a literal
`version` line and a PortGroup carrier (`go.setup`), with the no-op case
yielding the empty delta. Read-as-Tcl/write-as-text, total-subport
fidelity, and blast-radius-as-prediction are demonstrated, not asserted.

**The parser is proven.** The four-level Tcl CST — Script → Commands →
Words → Segments, byte spans at every level, brace bodies opaque behind
script/list lenses — is a pure function: bytes in, spans out, no I/O.
Validated against a full ports tree (20,037 Portfiles, zero parse
errors, tiling invariant intact), guarded by a checked-in fixture suite
(200 varied Portfiles, every PortGroup, 50 Tcl scripts from base and
mpbb), and cross-checked against a real `tclsh` by a differential
harness. It was deliberately the first thing built: the highest-leverage
component, and the fastest way to put the language decision and the
read/write asymmetry under real load. Still ahead: fuzzing and index
sub-segmentation.

**The first commands exist.** The `dockhand` binary ships `classify` —
the tractability census over a ports tree, resolving targets the way a
MacPorts user writes them (port and subport names via the tree's own
PortIndex, paths, categories) — and `doctor`, the machine capability
probe. Exit codes distinguish whose problem a failure is: the
invocation, the machine, the tree, or the operation.

## Deleted artifacts

Two were removed pending redesign and are not yet replaced: the workflow
diagram (discover → plan → edit → verify → publish) and the interface
sketch.

## Open questions

- `Plan` is necessarily partial above T2, since a T3 patch does not exist until an external generator has run.
- `Metadata` is variant-relative **and** subport-relative, so it is keyed by a pair; the current sketch flattens both. `Diff` must also handle subports being added and removed, not only fields changing.
- Whether patchfile refresh belongs in scope at all.
- The tractability ladder is five rungs, but only T0, T3 and T4 are ever defined. T1 and T2 are used throughout and stated nowhere.
- The design names three verification propositions. Field evidence produced a case none of them span — a faithful edit, a green build, declared dependencies, and a wrong result.
- ~~A cohort conflicts with the one-PR-per-port norm~~ — resolved by PR evidence: the norm is one PR per logical change, and the unit reviewers police is the commit. What remains of the cohort shape is its build-ordering constraint, plus the new commit-plan requirement on `promote`.
- What is the decline rate on computed versions? It decides whether a constrained evaluator gets built at all.
- Is diff minimality intent-relative? `Obsolete` reduces a Portfile to a template, so its diff is maximal by construction and template conformance is the right test instead.
