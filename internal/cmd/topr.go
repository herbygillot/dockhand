package cmd

// --to-pr: asking a write intent for a pull request in the same breath
// as the change, and the boundary that decides what that can mean here.
//
// The flag has exactly two meanings and they are not variations of each
// other. With a local verifier, it binds the minted record to the
// reconciler's publish slot — Destination ToPublished — and the change
// goes out when it has earned a pass, published by the machine. Without
// one there will never be a pass, so nothing will ever take it off that
// road; the only publication left is an immediate one, in this
// invocation, on the authority of the person who typed it.
//
// WHICH MEANS THE FLAG SPENDS RING 3 ONE WAY OR THE OTHER, and the
// boundary below is where that is decided — before a selector expands,
// before a Portfile is read, before anything is minted. That placement
// is the whole design: every refusal here is one the user gets INSTEAD
// of a branch, rather than after one, and a branch minted for a road
// that will not take it is a branch somebody has to notice and clean up.
//
// The table, as ruled:
//
//	human, no verifier   the immediate road: prechecks, mint, publish
//	human, a verifier    24 machine-publish-disabled, nothing minted
//	auto,  no verifier   24 machine-publish-no-verifier, nothing minted
//	auto,  a verifier    24 machine-publish-disabled, nothing minted
//	a selector           refused: one authority publishes one change
//
// Why a verifier present refuses BOTH invokers: with one, --to-pr is a
// request that the MACHINE publish, whoever queued it. Ruling 9 gates
// every publication whose publisher is a machine regardless of who
// asked, and this build's answer is no — so the constant is spent here,
// at the moment the record would be bound, rather than a pass later when
// the branch is already standing and a person is owed an explanation for
// a refusal they can do nothing about.

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
)

// toPRRoad is which of --to-pr's two meanings an invocation got.
type toPRRoad int

const (
	// toPRNone is every invocation that did not ask.
	toPRNone toPRRoad = iota
	// toPRQueued binds the record to the reconciler's slot and mints
	// normally. Unreachable on this build — the gate refuses first — and
	// written out anyway, because it is what the constant's flip turns on.
	toPRQueued
	// toPRImmediate mints and publishes in one invocation, on the
	// authority of the person who typed the verb.
	toPRImmediate
)

// toPRBoundary reads the table above against this run.
//
// The verifier question is asked of the MACHINE — a PATH lookup, no
// provider composed — which is the same cheap gate the drain asks before
// it starts anything. Composing a provider here would list base images
// to answer a question about which sentence to print.
func toPRBoundary(rs *runstate.Context) (toPRRoad, error) {
	if rs.Tools == nil || !rs.Tools.Have(tool.Tart) {
		if rs.Invoker == record.Machine {
			return toPRNone, &MachinePublishNoVerifierError{}
		}
		return toPRImmediate, nil
	}
	// The build's answer about a machine spending ring 3, asked as the
	// machine even when a person typed the verb: what --to-pr binds the
	// change to here is the machine's road, and who queues work for a road
	// is not who walks it.
	if err := engine.GateRing3(record.Machine, rs.MachinePublish); err != nil {
		return toPRNone, err
	}
	return toPRQueued, nil
}

// toPRSelectorRefusal is --to-pr meeting a selector that named more than
// one port.
//
// It is a usage error and not a machine-gate one, because nothing about
// the destination refused it: publishing is one person's judgment about
// one change, and a flag that turned a `maintainer:me` sweep into four
// hundred pull requests would be the single most expensive typo dockhand
// could offer. The remedy is the two verbs it is short for.
func toPRSelectorRefusal(selector string, n int) error {
	return usagef("--to-pr publishes one change on one person's authority; %q names %d ports — sweep them, then `dockhand promote` what you mean to publish",
		selector, n)
}

// sayToPRImmediate names the road before it is walked, because it is the
// one write intent that spends a reviewer's attention as a side effect
// of a verb that usually does not.
//
// Said on stderr and before the plan is realized, so that a person who
// typed --to-pr expecting the queued road — the one --to-pr means on a
// machine that can verify — reads what is actually about to happen while
// it is still theirs to interrupt.
func sayToPRImmediate(rs *runstate.Context) {
	fmt.Fprintln(rs.Err, "--to-pr with no local verifier: this change will be minted and published in one invocation,")
	fmt.Fprintln(rs.Err, "unverified, on your authority — the pull request will say so.")
}
