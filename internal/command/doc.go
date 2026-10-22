// Package command is dockhand's command line, rebuilt for Design v3
// (docs/design-v3.md).
//
// A command parses its arguments, finds its context (the branch, from the
// worktree, --branch, or a prompt), asks what only a person can answer,
// and renders what the engine reports. The engine decides. Anything that
// chooses what is eligible, enforces a guardrail or a publication rule,
// runs several engine operations as one with its own rule for a failure
// partway, or keeps state belongs in the engine, whose tests then hold
// it; the command calls it and says what happened.
//
// That keeps the command line from becoming v2's cli, which grew by repeating
// one sequence thirteen times and by reaching past its layer to work out
// verdicts (docs/reviews/2026-09-21-architecture-and-organization.md).
// TestCommandTalksToTheEngine holds the line mechanically: its packages
// import only what they bind, render, and compose, and read and write
// records only through the engine.
//
// It's split into packages, one concept each (v0.5.0 part 4, item 2): a
// package per verb or family of verbs (author, check, status, …), which
// imports no other but the composites, ship and bump; and below them the
// layers they share, view (what the engine returns, shown), checkrun
// (a queued check seen through), host (the Mac, or a test's stand-in),
// settings (what every command shares, and the engine it opens),
// branchflags (the branch a command works in), and cmdtest (the tests'
// harness). This package is the root, newRoot's command tree, which
// registers each verb, and RunWith, the command line a test binary runs
// through cmdtest. TestCommandTalksToTheEngine holds the shape too.
package command
