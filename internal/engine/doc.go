// Package engine is the engine under every write verb: a plan becomes
// a branch, the branch is verified, and what verification learned is
// settled into the record — the same way whichever intent produced the
// plan and whichever verb asks, because the unit of operation is the
// branch (D21), not the invocation that made it.
//
// mint realizes a plan as one commit under dockhand's branch
// namespace, entirely in the object database; the user's HEAD and
// working tree are never touched. submit stages that commit's portdir
// and hands it to the verify provider, recording the running job
// THROUGH the ledger. settle polls what is running and writes what it
// learns back under the notes lock; supersedeStale and Discard release
// what superseded or abandoned commits still hold. changedPort says
// what a branch actually changed, by evaluation, because a portdir's
// name is not that.
//
// What a verification record IS belongs to record — the wire format,
// its states, the strict codec. Where one LIVES belongs to ledger,
// which owns the notes ref and the lock over it. What one is WORTH
// belongs to verdict, which decides from values and never looks at
// anything. What stays here is the effectful sequencing that carries
// those three out, and the sentences the verbs say while it does:
// recordRun writes through the ledger and then tells the user what was
// recorded, because what a verb says belongs to the verb.
//
// The package sits above the domain packages and below cmd, and it
// takes Deps — the run's streams, seams and services, each a func the
// run resolves once. Deps is built by exactly one constructor,
// runstate's, so the run's memos are the engine's memos; nothing here
// imports runstate, and a Context never reaches this far. The domain
// packages — planners, styles, evaluation, the vendored families —
// take only what they need, a Prefix, an Evaluator, a tempdir.Root,
// because a planner that accepted a Context would be a planner that
// could reach the command line. The engine never sees a flag either:
// parsing is the CLI's business, and what arrives is already parsed —
// a Policy, a platform.Release.
//
// What the engine refuses, it refuses with a type: BranchInFlightError,
// VerifyDeferredError, VerifyFailedError. Each is its own kind of
// outcome — a decline, a machine that could not start, a port that does
// not build — and the exit table gives each its own code.
package engine
