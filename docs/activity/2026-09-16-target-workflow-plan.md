# Target workflow planning — 2026-09-16

Reviewed the current CLI, contribution acceptance/integration, SQLite change records, name resolution, source interpretation, release selection, and native Tcl version adapter in response to the Terraform usability report. The earlier read-only DB inspection found a failed automatic bump followed by a passing standalone verification of unmodified master; there was no prepared Terraform contribution.

Authored [the implementation plan](../target-workflow.md), reordered [the roadmap](../roadmap.md), and linked the superseding plan from [bump coverage](../bump-coverage.md) and [CLI design](../cli-design.md). The plan includes truthful outcomes, common port/subport resolution, early contribution identity, transactional continuation/retries, explicit checkout verification, general HTTP regex livecheck discovery, and the subsequent Wasmer/coverage work. It retains human editing, exact evidence applicability, independent pinned versions, multi-repository state, and isolated dependent verification.

Two implementation details need coordinated changes: the current integration path creates the contribution only after branch creation and treats GeneratedCommit as immutable from insertion; current archive-release validation assumes an explicit requested version. The plan calls out their new lifecycle rules rather than treating the work as CLI aliases.

No application code, local state records, ports-tree contents, or remote resources were changed. Existing untracked reviews and logo files were left alone. Validation is documentation diff/link checking; application tests are not warranted for this planning-only change. No commits were made in this pass.
