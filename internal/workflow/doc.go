// Package workflow accepts Dockhand requests and advances their durable records
// toward the requested destination.
//
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
// The current execution path verifies one resolved target against an existing
// committed revision with an explicit build configuration. Preparation,
// publication, dependent scheduling, and evidence reuse remain unfinished.
// Intake supports more actions than the cycle can currently execute; acceptance
// alone establishes neither provider admission nor successful completion.
package workflow
