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
// That keeps this package from becoming v2's cli, which grew by repeating
// one sequence thirteen times and by reaching past its layer to work out
// verdicts (docs/reviews/2026-09-21-architecture-and-organization.md).
// TestCommandTalksToTheEngine holds the line mechanically: the package
// imports only what it binds, renders, and composes, and reads and writes
// records only through the engine.
//
// The package is also v3's composition root: settings.go wires the
// providers and GitHub's login into the engine it opens.
package command
