# 2026-09-24: a moved Tart home's pool shares its artifact directory

Found by the first real bump after step 2, `bump flyctl` against the
person's own state database: every reservation failed with "UNIQUE
constraint failed: provider_pools.directory".

A Tart execution pool is its Tart home (`verify/tart.poolOf`), and its
artifacts go under the database's `artifacts/tart`. When dockhand's home
moved from `~/.tart` to `~/.dockhand/tart`, the new home's pool needed the
directory the old home's pool was already recorded with, and schema 2
made a pool's directory unique. The scratch database the step 2 proofs
used had never held the old pool, so they did not meet it.

Nothing needs one pool per directory: what a pool writes there is named
for the pool (resource directories are `dockhand2-` and a digest of the
pool and execution), for a request (the locks), or uniquely (the
`.preparing-` directories), and retention removes only its own resource.
Schema 25 rebuilds `provider_pools` without the constraint; a scope is
still one pool, and a pool still refuses a different directory for
itself.

`TestPoolDirectoryMigrationLetsAMovedHomeShareItsDirectory` migrates a
schema 24 database with work in it, keeps its pools and executions, and
registers a moved home's pool on the same directory.

Before migrating the person's database, a backup was written to
`~/.dockhand/state-before-schema-25.db` with `dockhand db backup`.
