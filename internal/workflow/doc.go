// Package workflow accepts Dockhand requests and advances durable records toward
// their requested destinations.
//
// Binding methods freeze source, target, verification, and publication inputs
// without writing workflow state. Their implementations use the _bind.go suffix.
// [Engine.Submit], branch adoption, and control intake establish durable intent.
// [Engine.Cycle] applies controls, claims eligible work, runs phase executors,
// and reconciles cleanup. Phase executors use the _run.go suffix; shared phase
// invariants use _policy.go. Status and progress functions are read-only
// projections of one consistent state view.
//
// The state store is the handoff between request intake and driver execution. Action
// invocations and persistent drivers can use the same engine. Callers own
// process lifetime, repeated cycles, waiting, and presentation; this package
// owns progression and bookkeeping after acceptance.
//
// External actions follow a claim, call, and record sequence. Short state
// transactions establish intent and ownership, provider calls run outside the
// write transaction, and a later transaction checks the claim before adopting results.
// Provider idempotency and reconciliation are still required: rejecting a stale
// state write cannot prevent a paused driver from making a late external call.
// Cleanup has its own claims and remains eligible after a job finishes.
//
// Version-bump jobs first checkpoint their resolved release; an automatic
// selection that needs no update completes there without preparation or
// verification. Both bump actions prepare immutable objects, checkpoint their
// candidate, and integrate a new branch under a branch-specific Git lock and ref
// preconditions. Recovery adopts only the recorded candidate. Verification can
// then build that result through the same attempt lifecycle as standalone
// verification jobs, including tree-only working snapshots.
// Standalone verification creates no contribution; tracked-branch verification
// records successor revisions without redefining the contribution's edited targets.
// Matching passing evidence can settle a new job with a reference to its original
// attempt before any provider call. Forced verification creates a new execution.
// Publication plans and reconciles a prepared or adopted revision with its remote
// destination. Verification plans one isolated attempt for each target and
// settles the job after every target reaches a terminal outcome. Acceptance alone establishes neither
// provider admission nor successful completion.
package workflow
