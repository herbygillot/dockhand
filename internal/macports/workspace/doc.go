// Package workspace projects one immutable git tree onto a directory MacPorts
// can read. The tree is the truth; the directory is a lazily materialized,
// scoped projection of it: a port's directory and _resources for preparing
// that port, the whole tree for indexing, staging, and the survey. An edit is
// never written into a workspace; it is an Overlay, a sibling projection
// sharing the base's tracked files by hardlink with the edited ones replaced.
// An interpreter session opened on a base's tree serves its overlays too,
// since a macports.Tree names its base root.
//
// A sparse scope rests on one property of the ports tree: evaluating a
// Portfile reads only its own directory and _resources. That is supported by
// a survey of the tree, not proven by the evaluator's read tracing, so it is
// confined by policy:
//
//   - A sparse scope serves only the path that prepares one port and asks
//     nothing else of the tree. Whatever resolves a bare name, generates an
//     index, evaluates arbitrary ports, or packs the tree holds the whole
//     tree. Name resolution and index generation widen a projection before
//     they list it, and the indexer refuses a root that is not whole, since a
//     truncated index would be cached under the tree id. Installing an
//     already cached index into a sparse root is allowed.
//   - Tracked files are written once, 0600 or 0700, and never opened for
//     writing again; Overlay is the only way to change contents. An overlay
//     replaces tracked regular files only, and adds and deletes nothing.
//   - An overlay is built from the base's tree entries, never from a walk of
//     its directory, so untracked files such as the installed index stay out
//     and symlinks are re-created rather than hardlinked, which on macOS
//     would link their targets. Its scope is the edited port directories and
//     _resources, however wide the base is, and an unedited _resources is one
//     symlink to the base's. It lives beside its base, on one filesystem.
//   - What writes into a root is the index: installation lands each file by
//     rename, so a concurrent reader never sees it truncated, and generation
//     over a root resets its Portfiles' modification times, which is how the
//     indexer learns what changed.
//   - The scope travels with the macports.Tree as its Projection; nothing
//     looks a workspace up by its root path.
//
// A Registry hands out one workspace per repository and source within a
// process, so consumers working on the same tree share one materialization,
// and closes it when the last holder releases it. Overlays are not
// registered.
package workspace
