// Package observe runs a Portfile's contents through MacPorts in modeled
// contexts and judges what the evaluation depended on. A Session observes
// baseline and candidate contents across the platform profiles their
// boundaries call for, caches baseline declarations, and reads immutable
// projections that its caller provides. The pure half decides which host
// reads are benign: PortGroup reads that only shape the build, toolchain
// probes, and the formatting reads that cannot select sources. portedit
// composes it; it knows nothing about edits, downloads, or results.
package observe
