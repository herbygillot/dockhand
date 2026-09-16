// Package changeset captures Git-backed sources and describes changes between
// immutable objects.
//
// It composes checkout or branch capture, explicit-base deltas, single-commit
// inspection, and correction candidates using git.Repository. Callers interpret
// changed paths as MacPorts targets and decide how candidates become tracked
// revisions; changeset leaves branch adoption and durable bookkeeping to them.
package changeset
