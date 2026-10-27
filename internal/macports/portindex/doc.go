// Package portindex prepares, reads, and retains MacPorts indexes for immutable
// source trees.
//
// Stage reconciles mirror or cached indexes with the selected source and installs
// a full and quick index into a materialized tree. Readers expose selector
// metadata, dependency relationships, and coverage gaps; callers evaluate
// concrete targets and decide verification policy. Consumers that resolve names,
// discover dependents, select ports, or stage a source for verification ask a
// Source for the index and never choose a cache layout of their own.
//
// The cache rests on these rules:
//
//   - Every consumer shares one cache root, which compose names and which is
//     independent of the store. Within it a generation is identified by its
//     source tree and its indexing environment: the indexer's digest, the
//     MacPorts Base it loads, the platform, and the variables the indexer is
//     told. The requesting command, the branch, the seed, and the mirror's
//     address are not part of it.
//   - The files alone never certify a generation. Its recorded metadata must
//     name the tree, and a strict request, which a candidate makes, never
//     reuses a generation that was not built strictly.
//   - Completed generations are immutable. A build runs in a temporary
//     directory, both PortIndex and PortIndex.quick are checked, and the
//     directory is published whole; an interrupted build leaves the previous
//     generation usable.
//   - A candidate derives from the generation of its recorded base. Only
//     upstream trees, a base or a tree that is its own base, advance the
//     latest pointer, which is a seed hint and never stands in for an exact
//     match.
//   - An incremental pass reindexes the port directories of the full Git tree
//     diff from its seed. A change under _resources requires a full pass, and
//     a seed whose diff cannot be established is passed over. A strict
//     incremental pass that reports parse failures is accepted only when
//     every changed directory and its subports are indexed and no unchanged
//     entry was lost.
//   - The mirror's index is a bootstrap seed for a non-strict request only. It
//     is bracketed to a master commit by its Last-Modified less a margin, the
//     target commit must not predate it, and the result keeps its provenance.
//   - Generation needs the whole tree: a sparse projection is widened first,
//     and the indexer refuses one that is not whole, since a truncated index
//     would be cached under the tree for every later process. Installing a
//     cached generation into a sparse root is allowed.
//   - Installation copies the index into each root by rename, so a reader of
//     a shared root never sees a partial file, and a staged copy outlives the
//     cache entry it came from.
//   - Builders of one generation serialize on its own lock, which the indexer
//     inherits. Users hold the environment's lock shared; collection takes it
//     exclusively, skips an environment in use, and keeps the latest seed.
//     A cache miss is recovered by regeneration.
package portindex
