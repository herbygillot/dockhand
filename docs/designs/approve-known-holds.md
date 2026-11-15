# Approve a known hold for the ports of one ship: `--approve`

A feature for v0.10.0, [#266](https://github.com/herbygillot/dockhand/issues/266). The issue says where it stands; this file is its design. Not yet built.

It came from a ten-port batch an agent drove on 2026-11-15: the person chose one port's major update by name, but `ship` holds a major version before it edits, so the port left the batch and was done by hand. The first draft was `--major PORT=VERSION`. The person asked for a general approval of holds, one-time and standing; architecture agreed on the general flag and pushed back on standing approval from a publishing command and on approving what has not been shown, and the person chose "Known holds only" (2026-11-15 06:10Z, the architecture thread's card) and accepted the placement (07:13Z).

## What exists today (read at e36a3c8b)

- An update nobody looks over is held before the edit when the major version moves (`majorHold`, internal/engine/servesubmit.go:737; the rule is `major-version`). No branch is made, so there is nothing to `decide` on.
- Standing approval is in the configuration: `holds.majors = "allow"` and `holds.allow = [rules]` for every port, and `[holds.ports."<port>"]` with `majors` and `allow` for one port.
- A hold found by the update (a changed license, a moved source or tag) leaves a branch, and `dockhand decide <rule> <port> --because "…"` records the person's answer, which lets the hold go for unattended work and is printed in the pull request.
- Nothing says "these ports, this hold, this once" before the edit.

## In short

```
dockhand ship mise rio neonmodem --to submit --approve major-version
```

`--approve RULE[,RULE]` on `ship` and `bump` lets the named rules' holds go for the ports named in that command, this once. It takes only rules whose finding is known before the edit, which today is `major-version`. It is recorded as a decision, so a `serve` running in the background honors it.

## Rules

1. **Known holds only.** A rule is accepted by `--approve` only where dockhand knows the finding before it edits, so the plan can show exactly what is approved. Today that is `major-version` (from 1.4.2 to 2.1.0). A rule found by comparing upstream, such as `license-changed`, `source-moved` or `tag-moved`, is refused by name, with the reason and the way that works: the port stops after its edit and takes `dockhand decide`. "Reviewed" in a MacPorts pull request must stay true.
2. **The ports named in the command.** With ports named, the approval covers them and nothing else. A `ship` or `bump` that names no port (everything outdated) refuses `--approve`: an approval is for what the person chose.
3. **Tied to what it met.** The approval is for the update this invocation found, and its record holds the version. A newer major version later is held again.
4. **Only the named rules.** Every other hold stays. In that batch the port would have been edited and then stopped on `license-changed`, with a branch to read and decide on, in place of being refused before any edit.
5. **A flag is acted on or refused by name** (the rule of [#195](https://github.com/herbygillot/dockhand/issues/195)). An unknown rule, a rule rule 1 excludes, or a rule that holds none of the named ports is refused before anything starts, naming it. Rule names are the ones `holds.allow` takes; there are no short forms.
6. **Shown before and after.** `--plan` lists each port it covers, as "major update 1.4.2 to 2.1.0, approved with --approve", in prose and in JSON. Without the flag, the held line names the flag, and the configuration lines for standing approval.
7. **Recorded as a decision.** The branch gets a decision for the rule with the version as its subject and "approved with --approve" as its reason, the record `dockhand decide` writes. It shows under "Decided", goes into the pull request's "Reviewed:" block, lapses when the port's files change, and is what `serve` and a resumed `ship` read. One mechanism, not a second kind of approval.
8. **Standing approval stays in the configuration.** `holds.allow` and `holds.majors` for all ports, `[holds.ports."<port>"]` for one. Nothing here writes it, and it is not kept in the database: it is policy the person reads and edits. There is no `--always-approve`: a command that publishes doesn't also change policy. The held line prints the exact lines to add.

## Not taken

- `--always-approve`, and standing approval in the database: rule 8.
- Approving a hold dockhand has not shown yet (the card's two other options): rule 1, the person's choice.
- `--major PORT=VERSION`, the first draft: one rule only, and the person named the ports already.
- Asking at a prompt in the middle of a batch: a batch must run without a terminal.
- Letting `decide` record a decision before a branch exists: a second place for decisions and a second lapse rule.

## Open, for a later look

- A command that writes the standing lines (`dockhand config` setting `holds.ports`), if printing them proves not enough. Its own item.
- Other rules that become "known before the edit" as dockhand learns to report them in `--plan`. Each is added to rule 1's list by name, with its test.

## Tests

- The flag lifts `major-version` for the named ports and for no other port.
- Refused before any effect: an unknown rule, `license-changed` and each other excluded rule with its pointer to `decide`, a rule that holds no named port, no port named.
- Another hold on the same port still holds after the edit.
- `--plan` and its JSON show the approval; `--plan` records nothing.
- The decision is on the branch, in "Decided" and in the pull request's text; it lapses when the files change; a later, newer major version is held again.
- `serve` submits a branch approved this way and holds one that isn't; a resumed ship needs no flag.
- No configuration key is written.
- Acceptance: one row, a batch of two with one major update approved.

## Usage docs, same commit

docs/usage/batches.md (the paragraph on a major version you did not expect gains the flag), docs/usage/unattended.md and docs/usage/configuration.md (one-time approval beside the `holds` keys, and which rules it takes), docs/usage/agents.md (an agent never adds `--approve` on its own; it is the person's word), and the help of `ship` and `bump`.

## Order

After [#195](https://github.com/herbygillot/dockhand/issues/195) (v0.9.0), which builds the change request once and makes every flag acted on or refused by name; this flag is then one more field of that request.
