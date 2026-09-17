# gc over every registration

One state database tracks several repositories, but `gc` collected only the registration of the current checkout. A retained verification VM belonging to a temporary cohort-exercise checkout survived every routine `gc` run because nothing ran gc from that directory; had the directory been deleted, no command could have released the VM, and deleting the database would have orphaned a 28 GB image.

## Change

`state.Store.Repositories` lists every registration, oldest first. `app.Collect` takes `CollectOptions`: without `AllRepositories` it behaves as before; with it, it opens no checkout, runs the retention cycle for each registration with the same providers, tags every item with its registration, and reports each registration it visited with whether its Git common directory still exists. `gc --all-repositories` exposes it and prints `Registration <id>: <common dir> (checkout missing)` lines before the items. Index caches are still collected once, since they are shared.

Retention itself is unchanged: an indefinitely retained failed environment was already releasable by explicit `gc`, and age thresholds and live claims still apply. The gap was reach, not policy.

## Cleanup performed

Against the real development database, which was at schema 14: backed up to `~/.dockhand/backups/state-schema14-202609162116.db`, migrated to 18, then collected per registration with `--older-than 0s`. Two retained resources were released and their diagnostics pruned, the cohort-exercise registration's retained VM `dockhand2-a1745cf35b41ca658022ec75` was released, the artifact directory fell from 508 MB to 1.3 MB, and the index cache kept only the master seed. `tart list` shows no `dockhand2-*` verification VMs; the base, golden, and Xcode images are setup assets and were left in place. `gc --all-repositories --dry-run` now reports all three registrations with nothing eligible.

## Checks

- Store test: registrations list empty, then both registrations oldest first.
- CLI test: `--all-repositories --dry-run` from a directory that is not a checkout reports a present and a missing registration and no eligible cleanup; the JSON form carries both registrations with the missing flag; plain `gc` still requires a checkout.
- `go test ./... -count=1` and `go vet ./...`: passed.
