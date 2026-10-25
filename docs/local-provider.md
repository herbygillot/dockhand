# The local provider

`check --on local` builds a revision in a MacPorts prefix on this Mac: `/opt/local`, or one of your own. It runs the program Tart's clones run, under the prefix's own `port-tclsh`, so it builds in MacPorts CI's order: each target linted, its dependencies installed, then fetched, checksummed, and installed from its source, with its declared tests after (Design v3 §7). Its results are recorded like any other provider's, and reused in the same prefix.

It's the way to check on a Mac without Tart, an Intel Mac among them, and the way to try a branch's port in the MacPorts you use every day.

## Setting it up

Nothing to set up for `/opt/local`. Dockhand never installs MacPorts: another prefix is one you made with MacPorts' own source install, such as `./configure --prefix=/opt/macports-test --with-no-root-privileges`. A prefix whose MacPorts predates `port snapshot` (2.9) is refused, since a check there couldn't put it back.

In `~/.dockhand/config.toml`:

```toml
[providers.local]
prefix = "/opt/local"          # where --on local builds; /opt/local when unset
keep = false                   # leave the branch's versions active after a check, as check --keep does
clean = ["/opt/macports-test"] # prefixes that are dockhand's own to clean
serve = false                  # let serve check there
test_timeout = "30m"           # a target's tests' bound, as Tart's
build_timeout = "6h"           # a target's build's bound, as Tart's
```

`--on local:/opt/macports-test` names another prefix, as `tart:sonoma` names a release. Several `--on` flags mean all must pass, so `--on local:/opt/local --on local:/opt/macports-test` builds in both, each its own environment with its own evidence.

## What it does

1. If an earlier check left the prefix changed, ending before it could put it back, it's put back first.
2. The revision's tree and its port index are staged in a directory of the check's own under `/var/tmp`, where MacPorts' unprivileged user can read them.
3. MacPorts reads the staged tree through a `macports.conf` of the check's own, named by `PORTSRC`, which MacPorts reads after its own so it wins. Its `sources_conf` lists the staged tree first, then the prefix's own sources with their `[default]`, so the branch's ports are found first and the rest as before. The prefix's `etc/macports` is never written.
4. Before anything changes, what's active is recorded, and `port snapshot --create --note "dockhand check-N attempt 1"` records the prefix.
5. Each target builds as-is: nothing is deactivated first. How many ports were active, and any other package manager on the Mac (Homebrew at `/opt/homebrew`, say), are facts of the build, which the pull request's Tested on line names: "built in /opt/local on the author's Mac with 143 ports active".
6. Afterwards, dockhand activates again each port whose active version changed, and deactivates what the check activated, from the list it recorded. That's faster than `port restore`, which installs each port again. The new images stay installed, inactive and unrequested, for `port reclaim`. The snapshot is deleted once the prefix is put back.

`check --keep`, or `keep = true`, leaves the branch's versions active instead, as `port install` would, and keeps the snapshot: `sudo port restore --snapshot-id N` puts the prefix back as it was, and the check says N.

A prefix listed in `clean` is one that's dockhand's own to clean. There each target builds with all but its dependencies deactivated, as in a clean VM and in CI, and everything is put back after. It's tied to paths, so no switch ever cleans `/opt/local`.

## What it costs

- **Root.** Where you can write the prefix's registry, as in a prefix installed without root, nothing needs `sudo`. Otherwise MacPorts builds there as root: a check asks for your password once, and runs the build under `sudo`. Every Portfile it builds runs as root on this Mac.
- **Your own `port` commands.** While a check builds, your `port` commands in that prefix wait on MacPorts' lock, and status says so. An upgrade you make there meanwhile is undone when the check puts the prefix back.
- **Binary archives.** MacPorts' archives are built for `/opt/local` alone, so in any other prefix every dependency builds from source, and the plan says so.
- **One at a time.** A prefix takes one check at a time.
- **serve.** serve doesn't check in a prefix unless `serve = true`, which needs a sudo rule that asks no password for the prefix's `port-tclsh`: root for anything a Portfile does.
- **Someone else's pull request.** A branch from `adopt --pr` is refused on `local`, since its Portfiles would run as root on this Mac; check it with `--on tart` or `--on github`.

## When a check ends early

A check you stop, with Ctrl-C or `dockhand cancel`, puts the prefix back as it ends. One whose process ended first, the Mac shut down mid-build, say, leaves the prefix changed. `dockhand status` says so:

```
check-12 left /opt/local changed, ending before it could put it back: sudo port restore --snapshot-id 7 puts it back, or the next check there does.
```

The next check in that prefix puts it back before it builds.

An image of the version the branch builds that's installed already would be activated, not built, so the check stops, saying `port uninstall jq @1.8.1_0`, or a revision bump, lets it build.
