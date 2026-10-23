// Command corpus replays MacPorts' own version bumps through dockhand's
// update, against a pinned slice of the ports tree: its PortGroups and
// about a dozen exemplar ports, each as it was before a bump, beside the
// Portfile the bump made (testdata/corpus). Unit tests model each
// PortGroup; the corpus runs the real ones, where v0.5.0's four plan-path
// refusals were.
//
// refresh rewrites the slice from a ports checkout; replay runs each row's
// update <port> <version> --new --plan with a dockhand binary, in a git
// repository made of the slice, and compares the plan's version,
// checksums, and dependency blocks with the bump's, as words where order
// means nothing.
package main
