# Global ports-tree and MacPorts-prefix options

## Scope

Committed focused status views as `f6ce0bf`. Added the requested global ports-tree and local MacPorts-prefix options.

## Implementation

- `--tree PATH` / `-T PATH` feeds the existing `app.Config.Repository`. The CLI defaults it from nonempty `MACPORTS_TREE`, otherwise the invocation's current directory.
- `--prefix PATH` / `-P PATH` feeds `app.Config.MacPortsPrefix`. The CLI defaults it from nonempty `MACPORTS_PREFIX`; omission preserves discovery of `port-tclsh` on `PATH`.
- Explicit flags override environment defaults. A Go caller supplying a nonempty configuration field keeps that value ahead of the environment; flags still override it. Environment interpretation stays at the CLI boundary rather than spreading into workflow or storage.
- Resolve directory values against the invocation's working directory before evaluation can use an isolated snapshot directory. Accept paths with spaces, reject explicit empty values, and register directory completion. Parsing/help perform no directory creation or existence checks.
- Remove the previous `-P` shorthand for publication to give it to the global prefix flag. Publication remains `--publish`.
- Reuse the existing application wiring for previews, submission, status, and driver service construction. The prefix selects the local evaluator installation; the guest prefix remains part of Tart configuration and recorded provider settings.

All added code and tests were authored for v2. No v1 code, comments, or tests were copied. No dependency, package, or database change was needed.

## Validation

Focused CLI tests passed for defaults, empty environment variables, configured callers, explicit flag precedence, long/short forms, inherited help, directory completion annotations, explicit empty values, and status selection from outside a checkout. Integration tests used real Git, SQLite, and native MacPorts through a fixture installation path containing spaces: environment-based preview, explicit prefix override without fallback, and short flags through a completed revision-bump job with verification explicitly skipped.

The full `make test-race` suite, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` passed. The built bump help shows both inherited flags and publication without a shorthand. No live VM or remote publication was used.

The tree environment variable was renamed to `MACPORTS_TREE` to match `--tree`, as requested. Runtime defaults, help, documentation, and existing path tests use the new name.
