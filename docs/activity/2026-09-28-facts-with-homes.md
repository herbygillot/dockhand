# 2026-09-28: facts with homes

The fifth step of the private-helper review's item ([review](../reviews/2026-09-28-private-helper-ownership.md), finding 7 and its table; [reconciliation](2026-09-28-private-helper-review-reconciled.md)). Each fact moves to the package that owns it, as an operation over it, in its own commit.

## Which releases run on which architecture

The observer, choosing which platforms to evaluate a Portfile on, knew by itself that arm64 begins at Darwin 20 (`current >= 20`, `major >= 20`). It also took every release to run on x86_64, so it evaluated Golden Gate on Intel. That platform doesn't exist: Apple made macOS 26 the last release for Intel Macs, and MacPorts' only builder for Darwin 27 in the facts table is arm64. `TestProfilesSkipTheDarwinThatNeverShipped` had pinned the Intel profile.

The fact is now `macos.RunsOn(darwin, architecture)`, beside `ProductForDarwin`: Intel's x86_64 until Darwin 25, as every release before Apple silicon is modelled, and arm64 from Darwin 20. The observer keeps its own policy of which profiles to sample. It samples each side of a boundary on the first architecture its release runs on, and on each where the port reads the architecture. So Golden Gate is sampled on Apple silicon alone.

`TestReleasesRunOnTheArchitecturesTheirBuildersDo` holds the rule to the facts table's builders, both ways, wherever the table has them. The profile tests now say Golden Gate isn't Intel, and that a boundary the port doesn't make architecture-dependent gets one architecture each side. Four mutations each fail a test.

MacPorts' own architecture rules, its universal archs and deployment target from Darwin 20 (`macports/platform.go`), mirror what MacPorts does rather than what a Mac runs, so they stay with it.

**Seen, not changed:** a boundary at Golden Gate, `${os.major} >= 27`, samples nothing below it. Its lower neighbor, Darwin 26, never shipped, so the release below the boundary, 25, is never evaluated. That's the observer's sampling policy, on the roadmap's smaller items.

## Provider names

The providers' names, `tart`, `github`, and `command`, were spelled as literals wherever something named one: the engine's reading of `--on`, the description's Tested on words, cleanup and check's word on a fork's branches, the command layer's composition and readiness lines, the configuration's capacities, and each provider's own `Name`. v2 had constants; the earlier code-organization review's finding 14 counted eight sites.

They are now `buildenv`'s, the contract every provider and its callers already share: `buildenv.Tart`, `GitHub`, `Command`, and `Prefix`, the design's provider of a MacPorts installation on this Mac, which v3 names only to say it doesn't have it yet. Nothing need import a provider to name it. The configuration reads its capacities by them, and keeps its own defaults, two checks at once on GitHub and one elsewhere, apart from what a platform allows, such as the two VMs macOS runs.

What stays spelled out is what isn't a provider's name: the Tart executable, the GitHub PortGroup and forge, the configuration's section tags, which Go can't take from a constant, and the words of messages.

The tests keep spelling the names, as people type them. Changing any of the four constants fails tests, `--on prefix` among them, now a case of `TestEnvironmentsAreTheProvidersOnNames`.

## A maintainer's identity

What a maintainers line says was read three ways, none of them in `macports`:
- the port index's selection flattened each port's groups and normalized each spelling (`maintainerIdentities`, `maintainerIdentity`);
- the configuration checked your maintainers line by counting braces over its words (`checkMaintainer`);
- it split the line on spaces and trimmed the braces to find your spellings (`File.Maintainers`).

They are now `macports`' (`maintainers.go`):
- **`ReadMaintainers`** reads a value as MacPorts does: a Tcl list of entries, each a list of the spellings that reach one person, an empty one dropped.
- **`MaintainerIdentity`** normalizes a spelling. It follows MacPorts' own reading of them (`unobscure_maintainers`): a GitHub handle, an address a Portfile obscures as `example.org:ada`, split at its first colon, a MacPorts handle as its `@macports.org` address, and a keyword as itself. It adds Repology's spelling of a GitHub handle, `ada@github`, which selectors accept.
- **`MaintainerKeyword`** says whether a spelling is `openmaintainer` or `nomaintainer`, which name no one.
- **`CheckMaintainers`** checks a line as MacPorts writes one: entries apart by spaces, each a spelling or a braced group of them, and at least one.

Selection compares identities through them, keeping its own matching. The configuration validates and reads the line through them. `create` writes the line as it is, so its grouping and spelling are kept.

**Tightened:** the check reads the line as a Tcl list, and it refuses what Tcl reads specially: quotes, backslashes, `$`, `[`, `]`, `;`, and control characters. Before, `maintainer = "[exec …]"` passed, and `create` wrote it into a Portfile, where MacPorts would run it as a command. A newline would have ended the `maintainers` line. A group that runs into the next word, `{a}b`, is refused as a stray brace. Before, it was misreported as a group left open.

Tests:
- `TestMaintainersReadAsMacPortsReadsThem`;
- `TestAMaintainersSpellingsAreOneIdentity`;
- `TestAMaintainersLineIsCheckedAsMacPortsWritesOne`;
- `TestACheckedMaintainersLineReadsAsItsWords`, which runs each checked line through MacPorts' Tcl as a `maintainers` command, and gets the entries the list reading gets.

The existing selection and configuration tests pass unchanged, apart from two more refused lines. Seventeen mutations each fail a test.

**Seen, not changed:** `port lint` refuses `nomaintainer` beside another maintainer, and `openmaintainer` alone. The configuration doesn't, so `create` could write a line lint refuses. That would be a new rule, not a move.
