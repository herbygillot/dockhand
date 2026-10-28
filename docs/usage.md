# Using Dockhand

Dockhand's long-form workflow guide is embedded in the binary. Run
`dockhand usage` for first setup and the path from an upstream release to a pull
request; run `dockhand usage <topic>` for a complete workflow or a particular
set of choices. The same pages are linked below.

`dockhand <command> --help` is the quick command reference. `dockhand config`
shows the configuration selected by the invocation. An agent with only the
binary can read every topic without a Dockhand source checkout.

| Page | Command | Use it to |
| --- | --- | --- |
| [From first run to a pull request](usage/setup.md) | `dockhand usage` | First setup and a release update through to its pull request |
| [Branches and worktrees](usage/branches.md) | `dockhand usage branches` | Find, start, adopt, rebase, restore, and retire work |
| [Editing by hand](usage/hand-edits.md) | `dockhand usage hand-edits` | Edit Portfiles, refresh checksums, and request rebuilds |
| [Builds and tests](usage/checks.md) | `dockhand usage checks` | Choose files, targets, tests, and build environments |
| [Publishing and review updates](usage/publishing.md) | `dockhand usage publishing` | Shape commits, assess evidence, and submit or update a PR |
| [Recovery and diagnosis](usage/recovery.md) | `dockhand usage recovery` | Diagnose holds, failed checks, interruptions, and stale evidence |
| [Many ports at once](usage/batches.md) | `dockhand usage batches` | Select many ports and manage partially completed work |
| [Unattended work](usage/unattended.md) | `dockhand usage unattended` | Bump, background checks, serve, and unattended holds |
| [A new port](usage/new-port.md) | `dockhand usage new-port` | Create and finish a Portfile for a new project |
| [Reviewing pull requests](usage/review.md) | `dockhand usage review` | Inspect, adopt, check, and review another pull request |
| [Scripts and agents](usage/agents.md) | `dockhand usage agents` | Explicit context, JSON, exit codes, scratch state, and automation |
| [Configuration](usage/configuration.md) | `dockhand usage configuration` | Profiles, holds, service settings, paths, and retention |

Start with the default guide, then choose the workflow you need. Each page
states its starting context, shows commands with expected outcomes, and names
related topics using commands that also work from the binary. Topic names are
stable entry points, not a second listing of flags.

[Design v3](design-v3.md) explains the model, [architecture](architecture.md)
maps it to code, and [decisions](decisions.md) records the standing choices.
Those are repository references; the usage pages supply the context needed for
their workflows directly.
