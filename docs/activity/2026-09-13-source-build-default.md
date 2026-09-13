# Opt-in source builds — 2026-09-13

Changed the shared Cobra build option so `--from-source` defaults to false on `verify`, `bump`, and `bump-revision`. Available MacPorts binary archives can satisfy the target and its dependencies. Explicit `--from-source` still requests MacPorts’ global source-only mode. Already installed dependencies are not forcibly rebuilt.

This avoids unnecessarily compiling available dependency binaries during verification pipelines. Accepted jobs retain their recorded `BuildConfig.FromSource`; the change applies to newly submitted requests. Updated CLI documentation, flag help, and the record field comment to state the scope accurately. No provider, state, or workflow changes were needed, and no v1 code or comments were copied.

Validation: the existing CLI suite passed, and generated help for all three commands reflects the opt-in flag. No new tests were added for this small default/help change. The changes remain uncommitted with the preceding explicit-version implementation.
