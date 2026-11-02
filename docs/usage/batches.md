# Many ports at once

A batch begins with a selection, then a plan for how far to take it. Keep
discovery separate from execution while deciding its scope. The examples use an
existing ports checkout and its configured identity:

```sh
export MACPORTS_TREE="$HOME/Source/macports-ports"
dockhand config
dockhand outdated --mine
```

Use your actual clone path. `--mine` needs the `maintainer` setting to match how
your ports name you. If it is missing, Dockhand explains the setting to add. See
`dockhand usage configuration` for the file and `dockhand usage` for first
setup.

## Select the ports you intend

`outdated`, `update`, and `ship` accept the same index selectors. Repeated or
comma-separated values within a selector are alternatives. Maintainer selection
is combined with category selection, and exclusions remove matching maintainers:

```sh
dockhand outdated --maintainer nomaintainer --category devel
dockhand outdated --maintainer alice,bob \
  --category devel,sysutils --not-maintainer openmaintainer
```

The first asks about unmaintained ports in devel. The second asks about ports
maintained by alice or bob, in devel or sysutils, excluding ports whose
maintainers include openmaintainer. `--mine` contributes your configured
identities to the maintainer alternatives; it can be combined with categories
and exclusions. `--not-maintainer` needs a positive selection beside it.
Selection uses master's index as fetched, rather than whichever files happen to
be checked out in a branch.

For a short explicit list, name ports instead:

```sh
dockhand outdated jq yq
```

Do not combine port arguments with index selectors, and give each port as an
argument of its own: `outdated "airspy airspyhf"` is refused once, as several
port names in one argument. A selected port is a candidate for discovery, not
a promise that Dockhand can safely update and publish it. The report
distinguishes ready updates, held or uncertain releases, and ports it could
not check. `--current` expands the report to current ports and the
explanations for other outcomes. An obsolete stub, a port whose `replaced_by`
names another, isn't checked: its row says `replaced_by py-ansible`, and
`update` of it names the port that replaced it. A newer tag under another
prefix than the port's, as `v0.12` where `github.setup` names none, is said on
the port's row; the Portfile's prefix still decides what `update` takes. A
look whose only match is a prerelease reads the port current and says so,
`upstream's newest, 3.2.17RC1, is a prerelease, which a port on a stable
release doesn't take`. A GitHub repository renamed or transferred is said on
a current port's row under `--current`, and on `outdated <port>`'s one line,
as `GitHub answers extrawurst/gitui as gitui-org/gitui now, which
github.setup could name`; `update --follow-moves` writes the new name.

## Preview a batch, then choose its stopping point

A selection that looked sensible in discovery may contain competing work,
missing release files, or a major version you did not expect. Inspect the split
into branches before changing files:

```sh
dockhand update --mine --plan
dockhand ship --mine --to check --plan
```

Use the same selectors in the preview and the execution you choose. For example,
this prepares edits without committing, then a separate workflow can review and
commit them:

```sh
dockhand update --mine
```

On a terminal it shows the planned branches and asks before starting. Without a
terminal it requires `--yes`. `update` commits nothing. If you want the complete
pipeline, choose one of ship's stages:

```sh
dockhand ship --mine --to commit
dockhand ship --mine --to check
dockhand ship --mine --to submit
```

These are alternative stopping points, not three required commands. `commit`
prepares and tidies the updates; `check` also builds their committed files;
`submit` also publishes eligible changes. The default is `submit`, so use an
explicit stage when publication is not intended.

On a terminal ship asks about the initial plan. With `--yes`, or without a
terminal, it runs unattended and holds findings a person must review; `--yes`
does not waive later holds. `--plan` and `--yes` cannot be combined. A preview
may fetch master, query upstream, and populate caches even though it does not
apply the planned changes. `dockhand usage unattended` covers these execution
choices.

## How ports become branches and commits

Selection-based `update`, `ship`, and serve group updates by Portfile. Several
independently versioned subports in one Portfile go into one branch, named for
their directory, rather than competing branches that edit the same file. Tidy
makes a commit for each recorded update, in order, when the edit chain still
stands. Additional hand edits can mean the history needs a different grouping;
inspect tidy's plan when that happens.

You can request several ports in one branch directly:

```sh
dockhand update kubectl-1.34 kubectl-1.35 --new
```

This names one branch for their shared directory when they share a Portfile.
Where each commit updates one port to the same version, submit titles the pull
request for them, as `ace, tao: update to 8.0.8`. A version is supplied with a
lone port: `update jq 1.8.1` means a version update because the second word is
not a port name. Choose currently existing port names in your checkout.

Discovery and batch planning avoid repeating existing work. A port already
changed by an open branch is left out of a new selection batch, with its branch
named. An update closed or archived unmerged is not proposed again at that same
release; a newer release makes it a candidate again. To resume such existing
work, inspect and name its branch instead of expecting another `--mine` run to
select it.

## Follow progress and resume individual changes

Ship carries each selected change to its queued check before it waits for
results, then runs the checks together, here where no serve leads and handed to
serve where one does, saying first which run together and how many at once, as
`check-820, check-821, and check-822 are checked as a batch, 2 at once on
tart`. A provider's capacity is one for the batch's checks, serve's, and a
foreground check's alike: `providers.command.capacity`, or on Tart as many
checks as the Mac runs VMs, two. A check that waits for a slot says so once,
naming the checks holding it and the process running each, as `check-822 waits
for a slot on tart: Tart takes as many checks as the Mac runs VMs, 2, and
check-820 (a foreground dockhand, pid 4242, v0.8.0, for 2 minutes) and check-821
(a foreground dockhand, pid 4242, v0.8.0, for 2 minutes) are using them`, which
ship prints beside the change's name. Each change is finished, submitted
where ship goes on to submit, as its own check ends, in the order they end. At
the normal output level a batch gives each change a row and lists the ones
needing attention below it. `-v` expands the stages, and `--json` retains each
change's partial result.

If some changes stop, the rest continue. The batch exit is 0 when all requested
changes complete, 3 when some complete and others stop, and 1 when none
complete. A single-target failure retains its own failure or attention code.
Inspect each stopped change's reason rather than treating the aggregate exit as
a rollback: edits, commits, checks, and successful submissions already completed
remain.

```sh
dockhand status
dockhand status --branch kubectl
dockhand ship --branch kubectl --to check
```

Use the actual branch name from the batch's output. A branch-specific ship
resumes from its current state; it can follow an active check that covers the
request and use passing evidence that still stands. `ship --passing --plan`
previews branches eligible for submission, and `ship --passing` carries those
forward. The current selection may have changed upstream since the first run, so
an identical discovery command is not a frozen batch manifest.
`dockhand usage branches`, `dockhand usage checks`, and
`dockhand usage recovery` explain the records and the next operation for each
state.

## Uncertain releases and incomplete discovery

An uncertain release is neither established as current nor safe to select
automatically. For example, an apparently newer version may have a tag older
than the port's current tag. The report names the release set aside and the
explicit update to consider after inspecting it. A port ahead of every upstream
version also needs investigation. Do not replace that judgment with a loop that
repeatedly asks for the newest version.

A release marked unpublished has no usable release assets yet. `update` cannot
prepare it, and unattended selection skips it. Wait for upstream to publish the
files or investigate the project's release process. A subport can move with a
sibling sharing its source and version; the report identifies that sibling
instead of treating every subport as an independent release lookup.

If no port could be checked, discovery fails rather than asserting that none is
outdated. An interrupted `outdated` can print partial observations; an
interrupted selection for `update` or `ship` starts no batch from that
incomplete discovery. Inspect the reported problems, then rerun discovery once
the cause is addressed.

## Budget GitHub requests

Large selections share the GitHub login's allowance with your other sessions and
tools. Dockhand looks up shared sources once, caches conditional reads, and
lists a GitHub repository's tags with `git ls-remote`, outside the API's
allowance, but a broad selection can still use substantial capacity. Before
discovery, it compares an estimate with the remaining hourly allowance. On a
terminal it asks before exceeding that estimate; without a terminal it refuses
unless `--over-allowance` is supplied.

`--over-allowance` permits the attempt; it does not raise GitHub's limit. Prefer
a narrower maintainer/category selection, inspect the reset time, or wait. When
a read reaches a rate limit that lifts within two minutes, Dockhand can wait and
try it once more. Otherwise it reports the reset time and avoids further GitHub
requests until then. Remote writes are not automatically retried. A partial
report is useful evidence, but not a reason to repeatedly restart the same large
look.

An explicit ship batch is not bounded by `serve.submit_limit`; that setting
limits serve's daily automatic submissions. Start with `--to check` when you
want to review a large selection before opening pull requests. For publication
choices and accepted failures, continue with `dockhand usage publishing`; for
scheduled daily selections, use `dockhand usage unattended`.
