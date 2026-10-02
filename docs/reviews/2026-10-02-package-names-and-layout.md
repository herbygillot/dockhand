# Package names and layout

Reviewed the 73 internal Go packages at **`034417d0d4378376c6529512491ee89b5fea57f1`** on 2026-10-02, using an isolated source export while development continued in the checkout. This is a naming and ownership proposal based on package responsibilities, exported APIs, and production import relationships. It is not another behavioral audit, and it implements no moves.

**Yes: several names and locations could be more informative.** The current structure is already mostly sound. I would change names that obscure the subject of an operation, and move packages whose location implies the wrong owner. The recent `evidence` and `macports/prdescription` extractions are good additions. The planned `macports/newport → macports/portcreate` rename also goes in the right direction; the roadmap already records it as the person's decision for batch 38.

## Changes I would prioritize

All paths in the tables are relative to `internal/`.

| Current | Preferred | What it actually does / why this is better |
| --- | --- | --- |
| `version` | **`buildinfo`** | Reads the running Dockhand binary's embedded module version, Git revision, and modified state. It does not interpret port versions. `buildinfo.Current()` distinguishes it immediately from `macports/version`. |
| `text` | **`textedit`** | Owns byte spans, source positions, and checked replacement edits. It is not general text formatting or terminal output. `textedit.Span` and `textedit.Apply` describe its API well. |
| `macports/dependency` | **`macports/depblock`** | Inspects, validates, regenerates, and edits `go.vendors`, `cargo.crates`, and `cargo.crates_github` blocks, including invoking their helper tools. It does not own the port dependency graph, build ordering, or dependency assessment. The existing name suggests all three. |
| `macports/portedit/archives` | **`macports/distfetch`** | Fetches MacPorts distfiles, computes/verifies checksums, applies direct-fetch restrictions, and falls back to the MacPorts mirror. Engine's revision assessment and archive diff use it, as does preparation. It is shared fetching infrastructure, rather than a private implementation detail of editing. |
| `preparation` | **`macports/portprep`** | Turns a proposed Portfile edit into an immutable Git candidate: manages its workspace, resolves/rechecks the release, calls the editor, writes Git objects, and checks the stored candidate. The current root-level name does not say what is being prepared. |
| `outdated` | **`macports/updatescan`** | Runs upstream discovery for a selected set of ports from a captured source. Its results include current, uncertain, unknown, moved-branch, and own-version cases, as well as available updates. Name the operation, not one possible result. Keep the user-facing `outdated` command. |
| `tart/channel` | **`tart/guestssh`** | Runs Apple's SSH client, manages guest/image trust and authentication, multiplexes commands, and verifies transfers. “Channel” hides the concrete transport and also reads like a Go concurrency abstraction. Keep it Tart-specific: its image keys, bootstrap account, and trust conventions are part of its contract. |
| `macports/newport` | **`macports/portcreate`** | Already planned. Pairs naturally with `portedit` and identifies the operation. Keep first-Portfile policy here: build-system detection, initial metadata, category guesses, and generated installation instructions. |

The strongest structural move is **`portedit/archives → macports/distfetch`**. It makes the dependency graph easier to explain: both editing and assessment use the downloader, and the downloader uses generic `fetch` transport. Today the path makes assessment appear to reach into the editor for a shared operation. Its current production consumers are engine, preparation, and portedit.

Keep the archive-related concepts separate:

| Package | Responsibility |
| --- | --- |
| `archive` | Walks/extracts archive containers. |
| `fetch` | HTTP/FTP transfer mechanics, response and redirect handling, and bounded/stalled I/O. |
| `macports/distfetch` (proposed) | Acquires the bytes a Portfile declares, with its checksums and mirror policy. |
| `macports/distfiles` | Associates evaluated fetch observations with exact checksum declarations and ownership across contexts. |
| `macports/binaryarchive` | Makes built archives into signed entries MacPorts can install. |
| `buildenv/staging` | Packages a captured ports tree, its index, and provider payload for a build environment. |

Those distinctions justify separate packages. A generic `artifacts` or `archives` umbrella would make the names shorter at the cost of losing them.

`portprep` and `portedit` should also remain distinct. The former owns an immutable candidate's Git/workspace lifecycle; the latter produces an evaluated edit within a supplied workspace. Locating both under `macports` does not mean combining them. `macports/workspace` and `portindex` already show that this namespace can contain MacPorts operations that use Git and files, alongside pure facts and rules.

## Secondary changes worth considering

These are useful when the relevant area is already changing; I would not make them prerequisites for further feature work.

| Current | Possible improvement | Judgment |
| --- | --- | --- |
| `macports/selection` | **`macports/portresolve`** | Its main object augments the evaluator with lookup in the correct source's PortIndex, then validates that the named target is what the Portfile defines. Resolution is more specific than selection. `portresolve.Reader` would be a reasonable mechanical rename; `Resolver` is worth considering if reshaping the API. |
| `macports/survey` | **`macports/portscan`**, with `Workspace → Snapshot` | Opens a fixed revision, selects ports/filters, reports index coverage, and holds the projection alive. “Survey” is not wrong, but `portscan.OpenAt` and `portscan.Snapshot` make the subject and lifetime clearer. Its current `Workspace` wraps another `workspace.Workspace`, which is the more important naming ambiguity. |
| `history` | **`branchhistory`** | Coordinates recoverable branch-history transitions for tidy, rebase, and restore. The longer name distinguishes it from querying Git history or retaining build history. A modest improvement, not a missing boundary. |
| `planning` | **`checkplan`** | Pure planning of a check's targets, environments, ordering, and exclusions. More specific at the import site. Keeping `planning` is also reasonable, especially after its recent extraction; the contract is already clear. |
| `github` | **`githubclient`** | Shared authenticated SDK access, credential discovery/device flow, rate limiting, address helpers, and a `gh` CLI wrapper. This would distinguish it from `forge/github` without relying entirely on aliases. I would not call the entire existing package `githubapi`, because it also invokes the CLI. |
| `tart/host` | **`tart/vm`**, if also clarifying `Machine` | Primarily controls VM lifecycle and guest-agent execution. A `vm.Manager` would say what the current `host.Machine` does: it manages several VMs; it is not itself one VM. Keep `host` if making only a path change would leave that type distinction less clear. |

Keep port resolution and port scanning as separate operations. `selection.Reader.Resolve` binds one requested name to an evaluated target. `survey.OpenAt` owns a captured source and a filtered collection with coverage problems and cleanup. Their shared use of an index is not a reason to merge their lifetimes. Better object names would help more than combining these small packages.

Likewise, preserve the two GitHub roles: a shared client is used by authentication, Actions, and forge operations; `forge/github` adapts observations and writes to the forge contract. Moving the shared client under `forge/github` would give the Actions provider a misleading dependency. There is a separate ownership question around `github.CLI.MarkReady`, which performs a PR operation, but moving that method is a behavior-preserving boundary change, not a reason to relabel all GitHub code at once.

## Proposed layout of the affected areas

This tree shows the prioritized changes, leaving the secondary renames above optional. It omits unchanged leaf packages.

```text
internal/
  command/                    CLI and composition
  engine/                     application operations and durable workflows
  model/                      shared records and vocabulary
  store/sqlite/               persistence contract and implementation
  coord/                      sessions, leases, leadership
  history/                    branch-history transitions
  planning/                   check planning
  evidence/                   what recorded checks establish
  reuse/                      whether a previous build can stand

  project/                    facts read from upstream source
  sourcecompare/              differences between those readings
  upstream/                   release/tag/livecheck selection and verification

  macports/
    portcreate/               first-Portfile generation and policy
    portprep/                 an edit prepared as an immutable candidate
    portedit/                 evaluated Portfile editing
      observe/                editor's modeled-context observations
    portfile/                 source inspection and literal rewrites
    depblock/                 generated Go/Cargo dependency blocks
    distfiles/                fetch/declaration correspondence
    distfetch/                declared source downloads and mirror fallback
    updatescan/               discovery across a selected ports snapshot
    selection/                resolving indexed names before evaluation
    survey/                   selected snapshot and its lifetime
    assess/                   meaning of upstream changes for a port
    prdescription/            MacPorts PR document composition and merge
    ...

  buildenv/{tart,ghactions,script,staging}/
  tart/{host,guestssh,provision}/
  forge/{github,gitlab}/
  github/                     shared GitHub client support
  buildinfo/                  this Dockhand binary's identity
  textedit/                   byte spans and source-preserving edits
  ...
```

I would retain the relatively shallow layout. A folder should denote a subject or an implementation owner. Adding general `core`, `domain`, `services`, or `utils` folders would classify code by abstract layer while making the useful subjects harder to find. A namespace folder also creates no Go import restriction by itself; keep the existing boundary tests.

## Names and boundaries I would retain

The remaining packages group coherently by responsibility:

| Area | Packages and their work |
| --- | --- |
| Application and state | `command` handles CLI interaction and composition; `engine` coordinates application operations; `model` owns shared records and vocabulary; `store`/`store/sqlite` own transactional persistence; `coord` owns sessions and fenced leases. |
| Check decisions | `planning` selects/order targets; `evidence` interprets recorded outcomes; `reuse` checks whether recorded build inputs still apply. Their questions differ, so I would retain three packages. |
| Upstream facts | `project` reads language/build/license facts and caches readings; `sourcecompare` describes differences; `upstream` resolves releases, tags, tracked branches, and livecheck; `pypi` reads registry metadata. Keep generic source readers outside `macports`. |
| MacPorts facts and execution | `macports` contains evaluated values, source contexts, vocabulary, and contracts; `eval` supplies the native implementation; `portindex` owns indexed metadata; `workspace` owns source projections; `installation` observes/installs Base; `version` handles port-version spelling and classification. |
| MacPorts rules | `assess` judges changes; `fetchguard` analyzes fetch-hook effects; `fidelity` checks source binding and intended metadata changes; `patchcheck` tests patch applicability; `portsource` interprets forge conventions in evaluated Portfiles. |
| MacPorts output | `commitmsg` composes shared message conventions; `commitrules` checks messages and Portfile changes; `prdescription` composes and preserves the PR template. Writing and checking related documents need not be one package. |
| Build providers | `buildenv` defines the contract, with `tart`, `ghactions`, and `script` implementations and `staging` for transported source. Keep them apart from `tart`'s VM/image mechanics and `tart/provision`'s image recipes. |
| External systems | `forge` and its `github`/`gitlab` adapters; `git` operations; `macos` platform/tool/storage operations; `credential` and `credential/keychain`; `signify` signing format. Their system names are useful. |
| Language and infrastructure | `tcl/syntax`, `tcl/shell`, and `tcl/rpc` separate parsing, child-process lifetime, and the framed protocol. `atomicfile`, `filelock`, `subprocess`, `scratch`, and `testsupport` have precise mechanical responsibilities. |
| Reporting | `progress` carries human-readable progress; `buildlog` interprets and manages build logs. These names remain accurate as their current operations expand. |

I would leave `portedit/observe` where it is for now: it is used by the editor and its contract includes judgments about the host reads that affect editing. Promote it only when an independent consumer needs the same modeled-observation operation. Its generic verb is sufficiently qualified by its current parent.

`project` is growing, particularly its CMake and ecosystem-specific readers. If that becomes difficult to maintain, `project/cmake`, `project/python`, or `project/cargo` could own parser-specific records and behavior, while `project` retains archive/root selection and assembled readings. That would be a focused extraction with real interfaces. Renaming the whole package to `manifest` would be inaccurate: it reads license text and build files too.

`macos` similarly covers both release facts and operational tool/storage code. A future split into focused toolchain or storage packages could be useful when those responsibilities grow independently. Its current name and roughly 1,100 production lines do not by themselves justify that split.

## Migration details

Start with `buildinfo`, `textedit`, and `depblock`, plus the already-planned `portcreate`. Then promote `distfetch` and relocate `portprep` and `updatescan`. Keep path/name changes separate from API changes or workflow fixes so a reviewer can verify each step mechanically. Apply secondary names when their owners are already being changed.

Two details matter beyond imports:

- **`version → buildinfo` changes a linker symbol path.** Update `Makefile:19`, the `-ldflags -X` example in `docs/development.md:9`, and the package's own example. A successful ordinary build alone would not validate a packager's version override.
- **The documentation already contains old names.** `docs/architecture.md` still refers to `engine.Counts` and `engine/body.go`; `tart/doc.go` refers to `verify/tart`, and the forge package comments refer to `publish`. These are stale ownership descriptions at this snapshot. Update the package comments and architecture map alongside moves, as well as import-boundary allowlists, test package declarations, scripts, and active documentation. Historical reviews should continue to describe the commits they reviewed.

Keep public command names, configuration keys, JSON fields, database schemas, and provider IDs unchanged for these internal naming changes. Package renaming should not silently become a user-facing migration.

No application code was changed, and no runtime tests were needed for this proposal. Validation consisted of the package/import inventory, reading the relevant APIs and implementations, and checking the documentation artifact. No Git worktree was created.
