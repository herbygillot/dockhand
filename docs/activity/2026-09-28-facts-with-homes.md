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

## GitHub's addresses

Which repository a GitHub address names, and the addresses of a repository and a pull request, were built and read in four places:
- the engine found MacPorts' remote with a reader of its own (`namesRepository`), laxer than the forge's (`NameFromRemote`);
- `create` read a project's page address itself (`githubName`), without checking the name;
- submit built someone else's remote itself (`theirRemote`);
- the command layer built pull request pages itself, in status and in JSON.

They are now pure operations in the GitHub layer, `internal/github`, beside `ValidRepositoryName`:
- **`RemoteRepository`** is the forge's strict reader, lifted from `NameFromRemote`. It takes HTTPS and SSH remotes that name a repository exactly, and now a trailing slash too, which Git accepts. The forge's `NameFromRemote` stays the engine's seam, since the tests' remotes are local paths standing for GitHub's. Finding MacPorts' remote needs no GitHub login, so it reads through the function directly.
- **`PageRepository`** reads a page's address, as a person copies it from a browser, and checks the name. `ErrNotGitHub` marks an address elsewhere, which `create` words in its own terms.
- **`Remote`** is a repository's Git address over SSH or HTTPS.
- **`PullRequestURL`** and **`PullRequestChecksURL`** are a pull request's pages, which the command layer renders.

The review suggested the engine supply the pull request's address to the command layer. The command layer calls the GitHub layer instead. The engine knows no forge but GitHub, and the stored pull request names none, so a detour through it would add nothing yet.

**What changes:**
- MacPorts' remote is no longer recognized over `http://` or `git://`. GitHub stopped serving `git://` in 2022, and only `init`'s sentence reads the remote: master is fetched from MacPorts' URL either way. The earlier review narrowed its finding 13 to this.
- A remote dockhand makes up for someone's repository now ends in `.git` in the advice it's shown in, as the push address always did.
- `create` refuses a page whose owner or name GitHub wouldn't allow, such as one with a space, as naming no repository.

Tests:
- `TestARemoteNamesItsRepositoryExactly`, which covers the forge's cases, the engine's, and more refusals;
- `TestAPageNamesItsRepository`;
- `TestGitHubsAddresses`;
- `TestTheirRemoteIsOneThatPushesThereOrTheirAddress`;
- the upstream remote's cases, now through a real remote;
- the checks page in `status --attention`, and the page in status JSON.

Three of twelve mutations first went uncaught: which form someone's remote takes, and both pages. Nothing had pinned them as literals either. The last three tests pin them, and all twelve now fail a test.

## The ports tree's layout

The layout of a ports tree was known at over twenty sites in the engine, `macports`' own packages, `reuse`, and the Tart provider, as the earlier review's finding 29 and this review's finding 7 counted:
- each port in `category/port`, its Portfile there;
- a category being a top-level directory beginning with neither `.` nor `_`;
- what ports share in `_resources`, with the PortGroups in `port1.0/group/name-version.tcl`.

`_resources` had an exported constant in `reuse`, a private one in the workspace, and literals elsewhere. The category rule was a regex in two places and prefix checks in seven.

They are now operations in `macports` (`layout.go`), beside `ValidName`:
- **`ResourcesDirectory`** and **`PortGroupDirectory`**.
- **`IsCategory`** is MacPorts CI's rule for a top-level directory. MacPorts' own walk of the tree, `mporttraverse`, skips only `_`. CI's rule and dockhand's also skip dotfile directories, such as `.github`, which hold no ports.
- **`PortDirectoryOf`** is the port directory a path lies in, if any.
- **`ValidPortfilePath`** says whether a path is exactly a port's Portfile.
- **`PortGroup`**, with its **`Path`**, is MacPorts' `name-version.tcl`. **`PortGroupAt`** reads a path back, taking the version from after the last hyphen when it begins with a digit, as the impact reading did.

Each consumer keeps its own policy over them:
- **CI's scope** is the engine's `portChange`, which `diff` reads too: a port directory changes with its Portfile or its `files/`.
- **tidy's grouping** takes any file in a port directory.
- **update** falls back to a file's own directory outside every port.
- The Git-tree and file-system walks stay where they are, now asking `IsCategory`.
- A changed PortGroup is matched to Portfiles by the path MacPorts would read for their `PortGroup` lines. Before, it was matched by joining the name and version.

The engine's `Scope` now says what it changes as a person reads it (`Changed`), the ports' names and `_resources`. Both `status` and `work` had built that list themselves.

The Tart provider's own port-name check is `macports.ValidName`, which also refuses tabs, other control characters, and malformed UTF-8. It adds the archive site's restriction explicitly: MacPorts reads an archive at `<site>/<port>/<archive>`, a URL, so `#`, `?`, and `%` are refused. Guest commands were already quoted for the shell. `TestKeptArchivesGoToTheGuestSigned` now covers the refusals, which nothing tested before.

**What changes:**
- A Portfile path under `_resources` or a dotfile directory is no longer a port's, where the workspace and a target's selection took one before.
- A directory name with `..` inside it, such as `foo..bar`, is no longer refused by the workspace, whose check read any `..` as a parent directory.
- A file in the PortGroup directory that isn't `name-version.tcl` is taken as shared code that reaches every port, which is what the plan does with any other shared file it can't place.

Tests:
- `TestThePortsTreesLayout`;
- `Scope.Changed` in the scope test, and `TestAnUpdatedFileIsInItsPortsDirectory` for update's fallback;
- the evaluator's refusal of a three-part selector that isn't a port's Portfile, by its message: `tree.Select` refused one anyway, less clearly;
- the existing scope, tidy, impact, workspace, index, and selection tests, which pass unchanged.

Of 22 mutations, 20 fail a test. Three had gone uncaught until the last three tests above: `Changed` without `_resources`, update's fallback, and the selector's check, which was as untested before as after. The other two change nothing observable: a bare name given `/Portfile` is never a port's Portfile, and in the index's coverage check, a changed file outside every port would mark only a directory no entry has.
