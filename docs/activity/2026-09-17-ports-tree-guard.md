# Refusing checkouts that are not ports trees

The user found a `gh` contribution recorded against `~/Source/dockhand2`, dockhand's own repository, and asked whether that could happen again. It could: `app.Build` registered whatever Git repository the working directory was in, and a bump then fetched MacPorts master into that repository's object store, so the tool's checkout carried a 494 MiB pack of ports history and a stale registration with two stopped jobs. The ports-tree validation added on 2026-09-16 only guarded commands that read the local tree (`outdated`, `assess`); bumps read the fetched master and never asked.

## The guard

`app.openPortsTree` now stands in front of every command that resolves a repository from the tree: `Build` (bumps, verify, publish, wait, cancel, start, refresh, abandon), `FilteredStatus`, previews, corrections, assessment, discovery, and the single-repository `gc` path. It opens the checkout, validates the working tree with the existing `macports.ValidatePortsTree`, and, when the working tree is sparse or unpopulated, accepts a checked-out branch whose tree holds a `<category>/<port>/Portfile` two levels down (three `ls-tree` calls at most). Anything else is refused with the existing message naming the checkout and `--tree`, before the state database is created or the repository registered. `setup`, `auth`, and `db` still need no ports tree; the all-repositories `gc` path still visits registrations that already exist.

Test fixtures that were bare `git init` repositories now carry a Portfile, and a new CLI test drives twelve commands at a Go repository and checks that each is refused and no database appears.

## What remains in this checkout

The stray registration, its two jobs, and the fetched objects predate the guard. The objects are unreachable and `git gc --prune=now` after expiring the reflog would drop them; the registration is inert now that nothing can select it from this directory.
