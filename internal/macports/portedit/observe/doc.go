// Package observe runs a Portfile's contents through MacPorts in modeled
// contexts and judges what the evaluation depended on. A Session observes
// baseline and candidate contents across the platform profiles their
// boundaries call for, caches baseline declarations, and reads immutable
// projections that its caller provides. The pure half decides which host
// reads are benign: PortGroup reads that only shape the build, toolchain
// probes, formatting reads, and machine reads varied by a private evaluator
// operation. Tolerate reads that operation's summary without evaluating code. portedit
// composes it; it knows nothing about edits, downloads, or results.
package observe
