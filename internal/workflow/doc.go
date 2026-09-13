// Package workflow accepts Dockhand requests and advances their durable records
// toward the requested destination.
//
// [Engine.BindVerification] and [Engine.BindPreparation] evaluate a committed branch snapshot
// without writing workflow records. Its returned request can be passed to Submit.
// [Engine.Submit] records a queued job and returns an idempotent acceptance
// receipt. [Engine.Control] records cancellation intent. Neither starts a
// provider operation. [Engine.Cycle] applies controls, advances eligible work,
// and processes cleanup. [Engine.Status] projects one consistent state view without
// polling providers or changing records.
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
// Version-bump jobs first checkpoint their resolved release; an automatic selection
// that needs no update completes there without preparation or verification. Both bump actions
// prepare immutable objects, checkpoint their candidate, and
// integrate a new branch under a branch-specific Git lock and ref preconditions.
// Recovery adopts only the recorded candidate. Verification can then build that
// result through the same attempt lifecycle as standalone committed-source jobs.
// Standalone verification creates no contribution; tracked-branch verification
// records successor revisions without redefining the contribution's edited targets.
// Publication, dependent scheduling, and evidence reuse remain unfinished.
// Intake supports more actions than the cycle can currently execute; acceptance
// alone establishes neither provider admission nor successful completion.
package workflow
