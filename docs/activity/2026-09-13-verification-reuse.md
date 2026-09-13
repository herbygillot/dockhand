# Verification reuse — 2026-09-13

## Implemented behavior

A new verification can reuse a conclusive pass for identical complete tree contents and matching build inputs. Verifying working edits, committing exactly those contents, and then verifying the branch completes without another execution. The same decision is used for a prepared bump's result tree. Commit, base, and contribution revision identities remain provenance; they do not substitute for tree equivalence.

Applicability requires matching target and subport, explicit variants, provider, platform, image/environment digest, verifier digest, source-build and test policies, full provider settings, and artifact inputs. JSON key order and empty versus absent variant maps do not cause mismatches. Provider settings otherwise participate conservatively, including operational settings that might not change build output. A missing verifier identity or incomplete/non-passing evidence cannot establish reuse.

The lookup considers the latest 32 original terminal attempts with evidence for the repository, tree, and target, ordered by attempt creation time and ID. The newest attempt with identical inputs decides: a pass can be reused, while a failed, canceled, or inconclusive result prevents fallback to an older pass. A different configuration's result does not block a compatible result. Older compatible history beyond the bound may be missed, causing a new build. A recent target-only lookup supplies a comparison when no tree candidate exists. This is an indexed bounded-result query, not a promise that SQLite visits at most 32 index entries.

`verify --fresh` durably requires a new execution. Its passing result can satisfy later jobs. Reattaching with `wait` or continuing with `start` preserves the accepted choice. Human progress/status explains reuse or the mismatch; JSON includes the original attempt and evidence separately from the job's own attempts. Reuse has no new attempt, submission, admission timestamp, or VM. Trace attachment reports the reuse without replaying old logs. Original evidence and resource cleanup remain attached to the original execution.

## Organization and coordination

`verify/reuse.go` owns pure input and evidence comparison. `workflow/reuse.go` selects candidates through the backend-independent `state.Reader` query. Initial planning writes the plan, completed job, explanation, and original-attempt reference together. Competing drivers recheck durable state under the existing SQLite transaction; failed writes roll back the complete decision. Applied cancellation takes precedence over reuse.

Schema 5 adds `jobs.reused_attempt` and `jobs.reuse_detail`, plus indexes for repository/tree/target lookup. The store checks the reference's repository, original passing outcome, source tree, completed job state, and absence of the referencing job's own attempts. A recorded reference cannot be replaced or removed. Full applicability policy remains in `verify`. No evidence cache table, provider operation, extra package, dependency, or global lock is introduced. Writable opening migrates prior schemas transactionally; read-only opening still requires the current schema.

The driver now finishes local planning before calling provider capabilities. A reuse hit needs no driver provider calls. On a miss, the first eligible job may checkpoint its queued attempt, check capabilities outside the database write transaction, and reread ownership and cancellation before claiming work. Subsequent jobs share that cycle's capability observation. Original claim/call/record semantics, capacity enforcement, uncertain submission recovery, and independent cleanup remain intact.

Tart's accepted build configuration now records a verifier digest derived from the guest program, launch description, and an explicit host-protocol version marker. Host execution changes outside those inputs must advance the marker. New submission refuses a recorded nonempty digest that differs from the current implementation. Legacy records without the digest remain readable and executable, but cannot supply reusable evidence. Source binding and prepared-image identity checks still occur before acceptance. A reused pass reports testing of recorded inputs; it does not prove that external upstream servers or binary archives have remained unchanged.

## Validation

Pure applicability tests cover all compared input dimensions, changes in provenance, JSON ordering, nil/empty normalization, missing verifier identities, and incomplete/negative evidence. SQLite tests cover bounded candidate selection, repository isolation with overlapping source identities, negative-result inclusion, schema preservation, indexed query plans, and migration rollback.

Workflow tests verify working edits followed by a real Git commit of the same contents, database reopening, reuse with no configured provider, durable original evidence, idempotent intake, mismatches, forced execution, newer failed/canceled attempts, failed completion writes, competing store connections, and cancellation both before reuse and while provider capabilities are being checked. A prepared-bump test proves that reuse compares its result tree instead of its original source. The existing concurrent-driver test now also checks the planning checkpoint before capability observation.

The CLI integration test uses native MacPorts evaluation, a fixture image descriptor, and seeded original evidence to exercise real command submission, reuse output, trace reattachment, and durable `--fresh` intake. Tart tests reject changed verifier code before VM cloning and admit matching configuration. These tests do not claim a live VM build.

`make test-race`, `make vet`, `make build BINARY=/private/tmp/dockhand2-reuse`, and `git diff --check` passed. The additional prepared-bump regression passed separately with the race detector. The temporary binary's `verify --help` exposes `--fresh`. The existing local `dh2` binary was preserved.

## Provenance

All new code, tests, and documentation were authored for v2 by extending existing v2 workflow, state, verification, and CLI boundaries. No v1 code, comments, or tests were copied. No new dependencies were added.
