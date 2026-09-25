// Package model defines dockhand's v3 records and the vocabulary the domain
// packages share (docs/design-v3.md §3).
//
// A Branch is the unit of work: a Git branch of the ports tree, its base,
// title, lifecycle, and at most one pull request. A Revision is an immutable
// candidate source, either a commit or a numbered snapshot of working files.
// A Plan freezes what a check of one revision covers: its changed targets in
// dependency order, the extras and prerequisites it builds, the exclusions
// with their reasons, and anything left unresolved. A Run is one accepted
// check request; a GuestExecution owns one provider environment for that run
// and holds a TargetResult per target as each finishes.
//
// The package is a leaf: it imports nothing from dockhand, so every other
// package can use its vocabulary. Records are ordinary values; validation
// and state rules live beside them, and progression belongs to the engine.
package model
