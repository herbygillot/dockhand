# Global Git executable selection

Dockhand now accepts the inherited global `--git PATH` flag. An embedding caller's configured executable takes precedence over `GIT_BIN` while constructing the command; the parsed flag can override either. When all are absent, the Git package continues to resolve `git` through `PATH`.

Paths containing a directory component are made absolute against the invocation's working directory before repository commands can change their working directory. Bare executable names retain ordinary executable-path lookup. Explicitly empty flag values are rejected, and shell completion treats the value as a filename.

The existing `app.Config.GitExecutable` and `git.Repository` boundary already route source inspection, preparation, branch integration, status selection, verification materialization, and publication pushes through one executable selection. No second Git invocation path was added.

The CLI configuration tests cover defaults, environment values, embedding configuration, flag precedence, relative-path resolution, inherited help, and empty-value rejection. The implementation, tests, and documentation were authored for dockhand2; no code, comments, or tests were copied from dockhand v1.
