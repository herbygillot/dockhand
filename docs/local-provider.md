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

1. The check takes the prefix for itself until step 7 is done, by a lock keyed on the prefix's path, its links resolved, kept in `/var/tmp/dockhand-locks`, a directory every user reaches, sticky as `/tmp` is: a second check on the prefix, serve's and yours, another database's, or another user's, waits, saying whose: "waiting for /opt/local, held by check-12 (pid 4711)".
2. If an earlier check's dockhand ended while its program goes on building in the prefix, as after a crash, the check waits for that program to finish, saying so. If an earlier check left the prefix changed, ending before it could put it back, it's put back first.
3. The revision's tree and its port index are staged in a directory of the check's own under `/var/tmp`, where MacPorts' unprivileged user can read them.
4. MacPorts reads the staged tree through a `macports.conf` of the check's own, named by `PORTSRC`, which MacPorts reads after its own so it wins. Its `sources_conf` lists the staged tree first, then the prefix's own sources with their `[default]`, so the branch's ports are found first and the rest as before. The prefix's `etc/macports` is never written.
5. Before anything changes, what's active is recorded, and `port snapshot --create --note "dockhand check-N attempt 1"` records the prefix.
6. Each target builds as-is: nothing is deactivated first. How many ports were active, and any other package manager on the Mac (Homebrew at `/opt/homebrew`, say), are facts of the build, which the pull request's Tested on line names: "built in /opt/local on the author's Mac with 143 ports active".
7. Afterwards, dockhand activates again each port whose active version changed, and deactivates what the check activated, from the list it recorded. That's faster than `port restore`, which installs each port again. The new images stay installed, inactive and unrequested, for `port reclaim`. The snapshot is deleted once the prefix is put back, and the check's directory in /var/tmp with it: run as root, the program gives what it wrote there to you as it exits, so dockhand removes it with no other root command, and says one it can't remove.

`check --keep`, or `keep = true`, leaves the branch's versions active instead, as `port install` would, and keeps its record and snapshot: `dockhand undo check-N` puts the prefix back as it was, as step 7 does, and the next check there leaves it to that. MacPorts' `sudo port restore --snapshot-id N` brings back the ports requested before and what they need, not the set that was active, so dockhand offers it only where its own put-back can't run, with a `port activate` of the rest from the check's record.

A prefix listed in `clean` is one that's dockhand's own to clean. There each target builds with all but its dependencies deactivated, as in a clean VM and in CI, and everything is put back after. It's tied to paths, so no switch ever cleans `/opt/local`.

## What it costs

- **Root.** Where you can write the prefix's registry, as in a prefix installed without root, nothing needs `sudo`. Otherwise MacPorts builds there as root: a check asks for your password at its start, and again at its put-back where a long build outlasted sudo's five minutes, and runs the build under `sudo`. Every Portfile it builds runs as root on this Mac.
- **Your own `port` commands.** While a check builds, your `port` commands in that prefix wait on MacPorts' lock, and status says so. An upgrade you make there meanwhile is undone when the check puts the prefix back.
- **Binary archives.** MacPorts' archives are built for `/opt/local` alone, so in any other prefix every dependency builds from source, and the plan says so.
- **One at a time.** A prefix takes one check at a time.
- **serve.** serve doesn't check in a prefix unless `serve = true`. In a prefix whose registry is root's, sudo must run the prefix's `port-tclsh` asking no password: every root step runs `sudo -n <prefix>/bin/port-tclsh <guest.tcl> <check directory>`, the program setting MacPorts' `PORTSRC` and `PATH` itself, and dockhand asks sudo about that command, running it on an empty script. One line, added with `visudo`, lets serve check in /opt/local: `<you> ALL=(root) NOPASSWD: /opt/local/bin/port-tclsh`, beside a rule for `port` if you have one, since the program is Tcl that `port`'s commands don't express. It's root for anything a Portfile does, as a rule for `port` is, and no narrower: `port-tclsh` runs whatever script it's given, from a directory you can write. serve refuses where sudo would ask, printing that line with your name and the prefix's `port-tclsh`; dockhand never writes sudoers itself. A prefix MacPorts installed without root, as G3's is, needs no sudo at all, at the cost of MacPorts' binary archives. A check you run yourself needs no rule: it asks for your password.
- **Someone else's pull request.** A branch from `adopt --pr` is refused on `local`, since its Portfiles would run as root on this Mac; check it with `--on tart` or `--on github`.

## When a check ends early

A check you stop, with Ctrl-C or `dockhand cancel`, puts the prefix back as it ends. One whose process ended first, the Mac shut down mid-build, say, leaves the prefix changed. `dockhand status` says so:

```
check-12 stopped before it could restore /opt/local. dockhand undo check-12 puts it back as it was, or the next check there does; a check of the same branch first needs dockhand wait check-12, check --replace, or dockhand cancel check-12.
```

The next check in that prefix puts it back before it builds. Where the guest program dies and dockhand lives, its put-back asks for root again, as at the check's start, on the terminal where there is one, since a long build outlasts sudo's five minutes.

An image of the version the branch builds that's installed already would be activated, not built, so the check stops, saying `port uninstall jq @1.8.1_0`, or a revision bump, lets it build.
