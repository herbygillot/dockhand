# 2026-09-26: Xcode images by default, and "unmet"

This follows [the Xcode add-on](2026-09-26-xcode-add-on.md), with three
more of the person's decisions:

- **Xcode need is detected through prerequisites.** A port needs Xcode
  when it asks for Xcode, and also when a prerequisite it depends on
  asks.
- **Xcode images are the default.** A release with its Xcode image builds
  there. Direction decision 23 had the base image as the default, and is
  amended.
- **Choose the representation.** The person asked for a clean way to say
  "this environment can't build the port, because the port needs Xcode".

## The representation

The earlier change had the provider decide at build time, skip targets,
and record them as not run with a free-text detail. That put a fact
known before the check in the provider, and in a result, where "not run"
also means a check that stopped midway. Now:

- **An environment states its developer tools.** `Environment` gained
  `DeveloperTools`: Command Line Tools or Xcode, or unstated for a
  provider whose builders have their own.
  - Tart states it when the check is accepted: Xcode when the release has
    its Xcode image, else the Command Line Tools alone.
  - It's part of the environment, so the check is frozen with it. Results
    say what they were built with: "tart macOS 26 (Tahoe) arm64 with
    Xcode". Evidence from Xcode never stands in for the tools, or the
    reverse.
  - Executions store it (schema 11).
  - `ReleaseProvider.Platforms` became `Environments`.
- **The plan records what an environment can't build, as `Plan.Unmet`.**
  Each entry holds the target, the environment, what the target needs
  (`RequiresXcode`), and the prerequisite it needs it through, if any.
  - It sits beside `Exclusions`, and differs from them: an exclusion is
    one MacPorts CI makes too, and needn't pass; an unmet target still
    needs a check somewhere that has what it needs.
  - The plan decides it, since everything it rests on is known then. The
    runner never sends an unmet target to the provider, so no provider
    decides it, and no provider records it.
- **Its outcome is `unmet`.** Evidence makes it from the plan, as it does
  "excluded", and it's never stored:
  - it isn't a failure, so the run needs attention rather than failing;
  - it isn't "not run", which a later attempt or check can fill in;
  - it leaves the target unchecked for `submit`, which says what it needs
    and where.
- **What would help comes from the provider.** An optional
  `engine.Remedier` lets a provider say how to give an environment what an
  unmet target needs. Tart names the `providers setup tart --xcode`
  command. It shows in the plan, once per environment, and in the run's
  summary. It stays out of what the PR shows.
- **A plan with nothing it can build doesn't start.** `Runnable` is now
  false when every target is excluded or unmet everywhere, and `check`
  says why.

What it replaced:
- the provider-side `Skipper`, `Skip`, and `SkipDependents`;
- Tart's skipping;
- the result's `detail`. Schema 11 drops that column, rather than editing
  schema 10, which a database had already applied.

## "Prerequisite", and unchanged dependencies

**Changed prerequisites.** In a plan, a prerequisite is a changed port
that a target depends on. The check always builds it from source, never
from an archive. So when a prerequisite needs Xcode, its dependents do
too: they're unmet "through" it, directly or not, in each environment by
that environment's own dependencies.

**Unchanged dependencies are not pre-judged, on purpose.** MacPorts
installs them from its binary archives. It checks for Xcode only for a
port it will build, and skips a dependency's build dependencies when its
archive is there. So an unchanged `use_xcode yes` dependency with an
archive needs no Xcode.

Judging them before the check would need each one's archive
availability, which MacPorts decides over the network at install time.
Without that, it would over-require Xcode.

When one has no archive and needs Xcode, MacPorts refuses to build it in
the guest. The target then fails at install, with MacPorts' own reason.

## Flagged: planned with the tools, built with Xcode

A release is still planned in the tools profile, even when it will build
with Xcode. So a Portfile that chooses by `xcodeversion` can plan
differently from how it builds. The facts table has both profiles, so
modelling an Xcode environment in the Xcode profile is a contained next
step. The Mac's own release is planned with its own tools, as before.

## Tests

- **Engine:**
  - `NeedsXcode` per platform;
  - `Unmet` through a prerequisite, per environment;
  - an arm64 environment with the tools alone gets no job, while x86_64
    with Xcode builds everything;
  - the run needs attention, and its summary gives the remedy;
  - "not built: needs Xcode, through libharbor";
  - `submit`'s problem for an unmet target;
  - an unreadable `use_xcode`.
- **Tart provider:**
  - environments with their tools, the Xcode image making Xcode the
    default;
  - a release without its base image still refused;
  - the image following the environment's tools;
  - the remedy.
- **Command line:**
  - the plan's "Not built" lines, and the remedy once per environment;
  - a plan with nothing buildable refused, with the reason.

## Proven on the Mac

The scratch branch revbumping `ssh-askpass-mac`:

- **With the real images**, `check --plan --on tart:tahoe,sequoia` plans
  both "with Xcode".
- **Sequoia without an Xcode image.** A scratch Tart home held an APFS
  clone of `dockhand-base-sequoia` alone (`cp -c`, which only reads the
  original), and was removed afterwards.
  - `check --plan --on sequoia --also tree` planned Sequoia "with the
    Command Line Tools".
  - It showed "Not built ssh-askpass-mac …: needs Xcode", with the
    `providers setup tart sequoia --xcode` command, and tree built.
  - `check --on sequoia` of the port alone didn't start: "nothing in it can
    be built where it asks; see Not built".
- **The scratch database migrated from schema 10 to 11.**
