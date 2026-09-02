// Package lifecycle is the engine under every write verb: a plan
// becomes a branch, the branch is verified, and what verification
// learned is settled into the record — the same way whichever intent
// produced the plan and whichever verb asks, because the unit of
// operation is the branch (D21), not the invocation that made it.
//
// MintFromPlan realizes a plan as one commit under dockhand's branch
// namespace, entirely in the object database; the user's HEAD and
// working tree are never touched. SubmitVerification stages that
// commit's portdir and hands it to the verify provider, recording the
// running job as the commit's Note — the verdict set kept under
// refs/notes/dockhand/verify, keyed by sha and local to the machine
// that wrote it. SettleRuns polls what is running and writes what it
// learns back under the notes lock; CancelStale and DiscardBranch
// release what superseded or abandoned commits still hold. ChangedPort
// says what a branch actually changed, by evaluation, because a
// portdir's name is not that.
//
// The package sits above the domain packages and below cmd, and it is
// the one thing besides an Action that takes a runstate.Context: the
// engine needs the run's streams, seams, and services throughout, and
// threading each of them separately through every function would
// reconstruct the Context by hand. The Context stops here. The domain
// packages — planners, styles, evaluation, the vendored families —
// take only what they need, a Prefix, an Evaluator, a tempdir.Root,
// because a planner that accepted a Context would be a planner that
// could reach the command line. The engine never sees a flag either:
// parsing is the CLI's business, and what arrives is already parsed —
// RealizeOpts, a platform.Release.
//
// What the engine refuses, it refuses with a type: BranchInFlightError,
// VerifyDeferredError, VerifyFailedError. Each is its own kind of
// outcome — a decline, a machine that could not start, a port that does
// not build — and the exit table gives each its own code.
package lifecycle
