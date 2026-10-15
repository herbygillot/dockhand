// Package workspace projects one immutable git tree onto a directory MacPorts
// can read. The tree is the truth; the directory is a lazily materialized,
// scoped projection of it: a port's directory and _resources for preparing
// that port, the whole tree for indexing, staging, and the survey. An edit is
// never written into a workspace; it is an Overlay, a sibling projection
// sharing the base's tracked files by hardlink with the edited ones replaced.
// An interpreter session opened on a base's tree serves its overlays too.
// See docs/workspace-design.md for the invariant a sparse projection rests on
// and the consumers that must hold the whole tree.
package workspace
