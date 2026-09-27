# 2026-09-27: seams in the engine

Roadmap item 4. The [architecture and data-flow review](../reviews/2026-09-27-architecture-and-data-flow.md)'s finding 4: `engine` was becoming the replacement monolith, and all three providers imported it.

## The provider contract is a package of its own

- **Before.** `Provider`, `Job`, `JobTarget`, `Build`, `ErrInfrastructure`, and the optional capabilities were in `engine`, beside `Fork`, `Leftover`, and `CheckBranchPrefix`. The Tart, GitHub, and command providers imported `engine` for them, so the package that drives providers was also one they depended on.
- **Now.** They are `internal/provider`, which was only a directory:
  - `Provider`, `Job`, `Target` (was `JobTarget`), `Build`, and `ErrInfrastructure`;
  - the capabilities the engine looks for: `ReleaseProvider`, `Remedier`, `OwnTestsProvider`, and `LeftoverProvider`;
  - `Leftover` as a provider reports it, `Fork`, and `CheckBranchPrefix`.

  It imports only `model`. The providers import it instead of `engine`, and the engine and the command layer, which composes the providers, import it too.
- **`Leftover` is split.** A provider reports `provider.Leftover`, with its provider, reference, and words. The engine's `engine.Leftover` embeds it, and adds what clean decides: the check it was made for, why it stays, and whether it was removed.
- **Each provider asserts at compile time the capabilities it has.** The Tart provider is a `ReleaseProvider`, `Remedier`, and `LeftoverProvider`; the GitHub provider is an `OwnTestsProvider`. The engine finds capabilities by type assertion, so a signature that drifts fails quietly. While moving `Leftover`, the engine's own test provider stopped being a `LeftoverProvider` that way, and only its tests' expectations caught it.
- **A test holds the line.** `internal/provider`'s boundary test refuses a provider importing `engine`, `command`, `store`, or `coord`, each with why. The command layer's allowed imports gain `internal/provider`, for composition.
- **Names.** Engine code that called a local variable `provider` renames it (`builder`, `lister`, `remover`, `named`, `own`), since the package now has the name.
