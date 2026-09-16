// Package portindex prepares, reads, and retains MacPorts indexes for immutable
// source trees.
//
// Stage reconciles mirror or cached indexes with the selected source and installs
// a full and quick index into a materialized tree. Cache profiles include the
// indexer identity and platform, and staging and collection share a profile lock.
// Readers expose selector metadata, dependency relationships, and coverage gaps;
// callers evaluate concrete targets and decide verification policy.
package portindex
