# The Tart provider

`check --on tart` builds in a fresh clone of one of dockhand's Tart images,
one clone for each macOS release and attempt, deleted afterwards (Design v3
§7). It follows MacPorts CI's order (decisions 11 and 22). For each target,
in dependency order:

1. everything installed is deactivated;
2. the target is linted;
3. its dependencies are installed and activated, from MacPorts' binary
   archives where they exist;
4. it is fetched, checksummed, and installed;
5. its declared tests run. As in CI they are advisory unless the check
   says `--tests required`.

A target whose changed dependency failed is recorded as blocked, not built
against an old build of the dependency. Each target's result is recorded
as it finishes, so a guest lost midway keeps what it finished, and the
next attempt builds only the rest.

## Choosing releases

| `--on` | Builds on |
|---|---|
| `tart` | this Mac's macOS |
| `tart:sonoma,tahoe`, or `tart:14,26` | those releases, each a guest of its own |
| `tart:all` | every release dockhand has an image for |
| `tahoe` | a bare release name means Tart |

Several releases must all pass. `[check] on = ["tart"]` in
`~/.dockhand/config.toml` makes Tart the default. Without it, a check uses
your own script if `[providers.command]` is set up, and Tart on this Mac's
release otherwise.

## Images

Each release needs `dockhand-base-<release>`, such as `dockhand-base-tahoe`,
in dockhand's own Tart home, `~/.dockhand/tart` (or `$DOCKHAND_TART_HOME`).
Each image holds macOS, the Command Line Tools of the release's pinned
generation (from the [facts table](../tools/facts/README.md)), and
MacPorts. A check naming a release without an image stops before it starts,
naming the image it needs.

v3 can't make images yet: `dockhand providers setup tart <release>` is the
command that will, and it comes next. `v2-final`'s `setup` makes its images
in `~/.tart`, which v3 doesn't read.

## Settings

```toml
[providers.tart]
capacity = 1          # checks serve runs at once; 1 when unset
test_timeout = "45m"  # a target's tests; 30 minutes when unset
```

macOS runs two VMs at most, yours among them, so a check waits for a slot
when two are already running.

## What it reports

Each target's log is copied into the check's log directory (`dockhand
logs`). When the guest's Command Line Tools differ from the facts table's
row for its release, the check says so as a drift report, which never
changes a result (decision 10).
