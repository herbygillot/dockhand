// Command facts harvests the toolchain facts table, internal/macos's
// facts.json, and regenerates it (decisions 9-13 of the contracts
// direction).
//
// It has five commands. buildbot reads the header of a recent install-port
// log from each of MacPorts' buildbot builders. tart probes each of
// dockhand's Tart images through a disposable clone, running probe.tcl, the
// same probe the 2026-09-23 facts came from, under the image's port-tclsh.
// runner runs probe.tcl on the Mac it runs on, one of GitHub's macOS
// runners, once with each Xcode selected; host does the same on any other
// Mac, whose probes are kept in probes/. generate turns what they found,
// with the readings of Apple's installers kept in installers/, into the
// table, which is never edited by hand. It is a developer tool, independent of the dockhand CLI and of
// any database.
package main
