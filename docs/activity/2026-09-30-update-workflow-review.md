# 2026-09-30: port update workflow and decision review

Reviewed the update workflow at `073a880b449be4bfc2a9a8a507328453b5579700`, following release selection, preparation/fidelity, archive and dependency handling, upstream assessment, checks, and human/unattended submission. Checksum refreshes, dependent revision bumps, and batch/serve entry points were included. Concurrent uncommitted work was excluded by reading and testing an isolated export.

The [review](../reviews/2026-09-30-update-workflow.md) agrees with most of the edit-safety design. Its main recommendation is a current, scoped update assessment that preserves source identity, applicability, coverage, uncertainty, and the checks needed to resolve concerns. Findings distinguish implementation gaps from disagreements with deliberate publication policies. They cover incomplete comparisons, unsafe suppression heuristics, runtime compatibility, historical assessment reuse, mutable Git source identity, release-selection assumptions, validation coverage, and efficiency.

The [characterization patch](../reviews/2026-09-30-update-workflow-probes.patch) contains ten isolated probes. All passed while asserting the current behaviors and limitations, rather than asserting proposed fixes. The patch was checked against the isolated export. Existing sourcecompare, upstream, preparation, portedit, and planning suites passed; focused engine, command, and Tart guest tests also passed. HTTP fixture suites needed loopback permission after the sandbox initially refused their listeners. No live build, external publication, or full repository test run was performed.

Added only the review, its probe patch, and this activity note. Application code, existing tests, and the roadmap were left unchanged.
