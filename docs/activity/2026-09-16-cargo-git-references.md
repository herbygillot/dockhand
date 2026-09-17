# Cargo Git references and the port's offline policy

Roadmap item 4 asked for commit-qualified Cargo Git dependencies without inventing a branch or relaxing the maintained-override checks. The Codex control had stopped at the generator: crossterm is pinned with `rev=`, and the Cargo mapping accepted branch-qualified sources only.

## What the tree already does

The cargo PortGroup's `cargo.crates_github` row carries a branch, and `post-extract` writes that value as `branch = "..."` into Cargo's source replacement. Cargo matches a replacement to a lockfile source by URL and selector kind, so a `rev=`-pinned source cannot be replaced through the PortGroup at all. The maintained Codex port shows the consequence: at 0.152.1 its lockfile already pinned five crates by `rev=`, and the port declared no `cargo.crates_github` and emptied `cargo.offline_cmd` with the comment "Disable offline mode to workaround dependencies from Git". The 0.154.0 update kept that workaround; mcfly does the same. `cargo2port` ignores Git sources, so the registry comparison never saw them.

Declaring a `rev=` crate with the commit in the branch slot would therefore produce a Portfile that cannot build offline, and adding per-port config patches would be exactly the special case the roadmap rules out.

## Design

`dependency.GitCrate` now carries a `GitReference` (kind `branch`, `tag`, `rev`, or empty for the default branch) beside the repository and exact commit. Only a branch reference is `Declarable`. `Inspect` derives a `GitPolicy` from the evaluated `cargo.offline_cmd` and whether the Portfile declares `cargo.crates_github`:

| `cargo.offline_cmd` | declaration | policy | effect |
| --- | --- | --- | --- |
| non-empty (default `--frozen`) | any | declared | every Git crate must be declarable; a tag, `rev`, or default-branch pin is refused with the crate, its selector, and the two ways forward |
| empty | present | mixed | branch pins are declared with fetched checksums; the rest are left to Cargo's online resolution |
| empty | absent | online | nothing is declared, preserving the maintainer's shape; every Git crate is left online |

`generateCargo` partitions crates into `GeneratedBlocks.Git` (declared) and `GeneratedBlocks.Online`. Only declared crates take part in the archive-ambiguity and one-branch-per-repository checks, and only they need archive downloads. Preparation reports online crates by name and short commit. The maintained-override comparison still runs on the declared rows, and the evaluator now reads `cargo.offline_cmd` so the policy comes from evaluated state rather than text.

## Validation

- Unit tests cover each selector under each policy, combined or malformed selectors, the summary, and policy derivation from present, absent, and whitespace offline values.
- A preparation integration test pins a crate by `rev=`: with the default offline mode the bump is refused before any Git archive is requested; with `cargo.offline_cmd` emptied it completes without adding a declaration or fetching a Git archive, and the maintainer's comment survives.
- Live: master already carried Codex 0.154.0, so the replay bumped Codex to 0.155.0-alpha.15 on master commit `bffe10341a5`. Preparation completed: version and revision edited, main checksums refreshed, `cargo.crates` regenerated, sixteen `rev=`-pinned crates reported as left to online resolution, and the pinned V8 auxiliary archives untouched. Verification through the ordinary workflow is recorded below.

The dependency, portedit, preparation, and evaluator suites pass.
