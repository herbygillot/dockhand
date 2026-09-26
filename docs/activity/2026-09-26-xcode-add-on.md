# 2026-09-26: Xcode is an add-on

Settled on 2026-09-26: Xcode is an optional add-on, as it was in v2. For
ports that need Xcode, a check on Tart doesn't try to build them without
it. It tells the person to make the Xcode image instead. This closes the
oracle's open question, which profile a modelled context is: it is the
tools profile, dockhand's base images'. It also amends direction decision
6, which refused the whole check.

## What a check does now

- **The plan reads each release's `use_xcode`**, as MacPorts decides it
  there. Each target keeps the platforms where it needs Xcode
  (`PlanTarget.NeedsXcode`, `needs_xcode` in `--json`). A `use_xcode` that
  can't be read leaves the plan unresolved; it never counts as no.
- **One image per release** (decision 23). A release builds in its Xcode
  image, `dockhand-xcode-<release>`, when any of its targets needs Xcode
  and the image exists. Otherwise it builds in the base image.
- **Without the Xcode image:**
  - the port isn't built;
  - what depends on it isn't built either, rather than built against an
    old build of it;
  - each is recorded as not run, with a reason ("needs Xcode", "needs
    libharbor, which isn't built") and a log that names the command
    making the image;
  - when nothing is left to build, no VM starts.
- **Said before, and after.**
  - Before: `check --plan`, and a check or `submit` as it starts, prints a
    "Not built" line for each such port, with the command:
    `dockhand providers setup tart <release> --xcode <Xcode .xip, or a
    folder of them>`.
  - After: the results read "not run: needs Xcode", on the terminal and
    in the pull request.
  - The run needs attention rather than failing, since nothing failed.
    `submit` still needs those ports checked.
- **Other providers are unchanged.** GitHub's runners have Xcode, and a
  command provider's script decides for itself.

## What it took

- **Engine.**
  - A result has a `detail` column now (schema 10), for why a target
    wasn't run.
  - An optional `engine.Skipper` is a provider that says, before a check,
    which targets it won't build.
  - `SkipDependents` adds what depends on them, so the plan's lines and
    the provider's results agree.
  - The run's summary names each target not run, with its reason, rather
    than "did not finish".
- **The Tart provider** chooses the release's image, and records its
  skips before the guest starts. The guest program is unchanged.
- **`providers setup tart --xcode`** is back. It makes the release's Xcode
  image from an Xcode `.xip`, or a folder of them, with the newest Xcode
  the release runs. It takes up to 65 GB (Monterey's Xcode image is
  34 GB). `providers` lists the releases with Xcode images.
- **Tart is installed with `sudo port install tart`** in `providers`'
  messages. MacPorts has the port, and this Mac's Tart came from it.

## A gap found on the way: planning another release

Reading Sequoia's `use_xcode` from this Tahoe Mac failed: "evaluation
requires the native platform". The planner asked for a whole session on
the release it planned, and on a Mac a session is always the Mac's own
release. So `check --on tart:sequoia` could never plan, for any port. The
Tart provider's own proof used only Tahoe, and the roadmap claimed more
than was true.

The planner now models another release the way the oracle's contexts
are modelled: in the Mac's own session, through the observation API, with
the tools from the facts table. The directory's main port is found
natively, and its subports come from the modelled evaluation.
`TestAPlanReadsEachReleaseInItsOwnContext` uses real MacPorts: a port
asking for Xcode before macOS 13 needs it on Monterey and not on Sequoia.
The test fails on the old code with that same error.

One limit: the Mac's own release is read with its real tools. A Mac
without the Command Line Tools would find that every port needs Xcode on
its own release. Base decides that, from whether the tools' `make`
exists, and MacPorts itself needs the tools.

## Tests

- **Engine:**
  - `NeedsXcode` per platform;
  - an unreadable `use_xcode` left unresolved;
  - skips and their dependents;
  - a run whose skipped targets need attention and don't fail, with
    their words.
- **Tart provider:**
  - the Xcode image chosen when it exists;
  - without it, targets and their dependents skipped, with their logs,
    and the rest built in the base image;
  - no VM when nothing is left;
  - the cost named for an Xcode image.
- **Command line:**
  - the plan's "Not built" lines;
  - `setup tart --xcode`;
  - Xcode images in `providers`.

## Proven on the Mac

In a scratch clone and database, `ssh-askpass-mac` (the xcode PortGroup,
`use_xcode yes`) was revbumped:

- **`check --plan --on tart:tahoe,sequoia,monterey --also tree --also
  pv`:**
  - ssh-askpass-mac needs Xcode on all three releases;
  - tree and pv on none;
  - Sequoia and Monterey were modelled.
- **`check --on tahoe` passed** in a clone of `dockhand-xcode-tahoe`.
  - Its log shows `DEVELOPER_DIR` set to Xcode.app and `/usr/bin/xcodebuild`
    building against the macOS 26 SDK.
  - The clone was deleted.

A port skipped for want of an Xcode image wasn't shown live. Every
release here has its Xcode image, and images are never removed to stage
a test.
