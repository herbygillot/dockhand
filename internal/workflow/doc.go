// Package workflow accepts Dockhand requests and advances durable records toward
// their requested destinations.
//
// Binding freezes source, targets, verification requirements, and publication
// inputs before acceptance. Submit and control intake record durable intent.
// Cycle claims eligible work, invokes preparation, verification, or publication,
// and records the resulting checkpoints. Status and progress projections read
// consistent state snapshots.
//
// Files are organized into five groups: the engine and cycle kernel; request,
// submission, and control intake; input binding in *_bind.go and correction
// helpers; phase execution in *_run.go with planning, policy, selection, and
// recording helpers; and read-only status and progress projections. Cleanup and
// retention use the same kernel and claim rules as phase execution.
//
// The state store is the handoff between intake and execution. Action invocations
// and persistent drivers use the same Engine; callers own process lifetime,
// repeated cycles, waiting, and presentation. This package owns progression and
// bookkeeping after acceptance, including evidence reuse, dependent coverage,
// correction integration, and publication recovery.
//
// External actions follow a claim, call, and record sequence. Short transactions
// establish intent and ownership; external calls run outside the write transaction;
// a later transaction checks the claim before adopting results. Provider
// idempotency and reconciliation remain necessary because rejecting a stale write
// cannot prevent a paused driver from making a late external call. Cleanup has
// separate claims and remains eligible after a job finishes.
//
// Preparation checkpoints candidates before guarded branch integration.
// Verification records isolated attempts against frozen inputs, and publication
// uses the accepted destination and applicable evidence for the revision.
// Acceptance, provider admission, and successful completion are distinct
// milestones.
package workflow
