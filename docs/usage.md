# Using dockhand

This is the guide to dockhand v3: how its commands fit together, and the choices each one leaves you. Each page is also in the binary: `dockhand usage` prints the first, and `dockhand usage <topic>` each of the others, so an agent with dockhand and no checkout of it reads the same text. `dockhand <command> --help` is the reference for any one command, and `dockhand config` shows every setting in effect. [Design v3](design-v3.md) gives the reasons, and [Decisions](decisions.md) the rules dockhand holds to. The v2 guide is `docs/usage.md` at the tag `v2-final`.

| Page | `dockhand usage` | What it covers |
| --- | --- | --- |
| [Getting started](usage/setup.md) | (no topic) | the one-time setup, branches, the straight path from an update to its pull request, status, cleaning up, and every setting and limit |
| [Editing by hand](usage/hand-edits.md) | `hand-edits` | `edit`, `checksums`, `revbump`, finishing an update dockhand refused, and trying a branch's port with MacPorts |
| [Unattended work](usage/unattended.md) | `unattended` | `bump`, `serve`, what holds a submission nobody looks over, and `decide` |
| [Checks](usage/checks.md) | `checks` | what a check builds and where, its results, reuse, logs, baselines, and the providers |
| [Reviewing pull requests](usage/review.md) | `review` | someone else's pull request: `adopt --pr` and `review` |
| [Many ports at once](usage/batches.md) | `batches` | `outdated` and the selections `update`, `ship`, and `serve` take |
| [A new port](usage/new-port.md) | `new-port` | `create` |
| [For scripts and agents](usage/agents.md) | `agents` | `--json`, exit codes, what's asked without a terminal, a scratch setup, the environment, and the commands that act beyond this Mac |
