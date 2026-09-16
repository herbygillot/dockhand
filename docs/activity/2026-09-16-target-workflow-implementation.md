# Target workflow implementation — 2026-09-16

## Recorded outcomes

Added input-version observations to newly bound jobs and distinguished standalone verification from a prepared update in status and progress. Legacy records retain their original evidence; missing version observations are not inferred from today's checkout. Candidate commits are no longer labeled as existing branches before integration is confirmed. Failed preparation and already-current results explicitly report that no update branch/build was produced. Removed duplicated subport names from target labels.

Added regression fixtures for the reported Terraform failure followed by a passing unchanged-checkout verification, and interrupted branch integration. Status remains a read-only projection. No user database or ports-tree edits were made.

Validation: workflow suite passed; focused status and evidence-reuse CLI regressions passed. The initial CLI run exposed one expected assertion on the old success wording, which was updated to assert standalone provenance.

## Named target resolution

Added macports/selection to map names through an exact-source PortIndex and validate the indexed owner using native evaluation. Production bump/preview, verify, assess, and outdated share the resolver. Grouped survey uses the same indexed-name mapping. The editor resolves the selected target separately from its primary Portfile evaluation, preserving sibling fidelity. Removed the public --subport flags.

Validation: native named/versioned/generated-subport and stale-index regressions passed. Portedit, assess, outdated, and app suites passed. CLI regressions now expect indexed subport discovery to succeed, index progress on stderr, and a configured prefix to provide portindex as well as port-tclsh; all affected cases passed. Input-version storage also passed a workflow binding/storage round-trip check.

## Early contribution identity and retries

Added schema 15 and transactional contribution creation before preparation. Equivalent concurrent requests join one job through a durable request association; terminal failures can be retried under the same contribution with the original source and existing checkpoints. Integration initializes generated-commit provenance once under candidate validation. Already-current updates close an empty contribution without claiming branch/build/PR work. Receipts identify the accepted source, so retries do not print a newly fetched source they will not use.

Migration tests use temporary databases and verify legacy preparation association, standalone evidence preservation, and foreign keys. New tests cover concurrent acceptance/replay, retained source on retry, and conflicting versions/contributions. The state suite and focused preparation/migration tests passed; the full workflow run identified only the old expectation that a no-update job had no contribution, updated to assert a closed contribution.

The real named Terraform assessment completed as candidate-checked for explicit 1.16.2 on both Darwin 25 architectures. Its first full PortIndex took 3m42s. Name lookup now uses the existing exact-tree incremental cache for subsequent source revisions, independent of the contribution base; a complete Git diff governs reuse.
