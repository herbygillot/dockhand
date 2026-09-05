package cmd

// docs/cli.md against the tree it documents.
//
// cli.md is the CLI surface contract and nothing read it. Three of its
// statements were false by the time S14 landed — `23` and `62` were still
// marked reserved though the hold verbs and the publish slot shipped, and
// the `24` paragraph said no verb could reach that code while `dockhand
// promote --auto` exited with it — and each had been true when it was
// written. A contract that drifts silently is a contract that stops being
// consulted, so the parts a machine can check are checked here.
//
// What is checked is DELIBERATELY NARROW: that every exit code appears in
// the table with its own number, that every reason a refusal names is
// spelled somewhere in the document, and that every verb the tree registers
// is mentioned. Prose is not checked and must not be — a document whose
// sentences a test dictated would be written for the test.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
)

// cliDoc reads the contract. Its absence is a failure rather than a skip:
// a rule pinned against a document that has been moved is a rule nobody is
// checking.
func cliDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../docs/cli.md")
	require.NoError(t, err, "docs/cli.md is the CLI surface contract")
	return string(b)
}

// Every ruled exit code appears in cli.md's tables under its own number.
//
// The rows are `| `NN` | `Name` | …`, so the pairing is what is asserted:
// a code documented under the wrong number, or a name documented without
// one, is the drift this exists to catch. The three that predate the bands
// keep the shell's own meanings and are not table rows.
func TestEveryExitCodeIsDocumentedWithItsNumber(t *testing.T) {
	doc := cliDoc(t)
	for _, row := range []struct {
		code int
		name string
	}{
		{exitcode.PlanDeclined, "PlanDeclined"},
		{exitcode.BranchInFlight, "BranchInFlight"},
		{exitcode.AlreadyCurrent, "AlreadyCurrent"},
		{exitcode.Ambiguous, "Ambiguous"},
		{exitcode.DuplicatePR, "DuplicatePR"},
		{exitcode.PRMerged, "PRMerged"},
		{exitcode.Superseded, "Superseded"},
		{exitcode.Held, "Held"},
		{exitcode.MachineGate, "MachineGate"},
		{exitcode.NoMacPorts, "NoMacPorts"},
		{exitcode.EvalStartup, "EvalStartup"},
		{exitcode.RootRefused, "RootRefused"},
		{exitcode.ToolMissing, "ToolMissing"},
		{exitcode.NoVerifyEnv, "NoVerifyEnv"},
		{exitcode.ProvisionFailed, "ProvisionFailed"},
		{exitcode.VerifierBusy, "VerifierBusy"},
		{exitcode.NotPortsTree, "NotPortsTree"},
		{exitcode.PortNotFound, "PortNotFound"},
		{exitcode.NotARepo, "NotARepo"},
		{exitcode.Drift, "Drift"},
		{exitcode.BranchNotFound, "BranchNotFound"},
		{exitcode.FetchFailed, "FetchFailed"},
		{exitcode.WitnessUnreachable, "WitnessUnreachable"},
		{exitcode.WitnessAPI, "WitnessAPI"},
		{exitcode.LatestUnresolved, "LatestUnresolved"},
		{exitcode.VerifyQueued, "VerifyQueued"},
		{exitcode.VerifyAwaitingSlot, "VerifyAwaitingSlot"},
		{exitcode.PromotionPending, "PromotionPending"},
		{exitcode.VerifyFailed, "VerifyFailed"},
		{exitcode.VerifyBlocked, "VerifyBlocked"},
		{exitcode.VerifyUnsupported, "VerifyUnsupported"},
		{exitcode.VerifyErrored, "VerifyErrored"},
		{exitcode.MintedSubmitErrored, "MintedSubmitErrored"},
		{exitcode.PushedPRFailed, "PushedPRFailed"},
		{exitcode.PRRefreshFailed, "PRRefreshFailed"},
		{exitcode.SweepHardErrors, "SweepHardErrors"},
	} {
		want := fmt.Sprintf("| `%d` | `%s` |", row.code, row.name)
		assert.Contains(t, doc, want,
			"docs/cli.md must carry %s under %d; the table is what scripts are written against", row.name, row.code)
	}
}

// A code the document still calls reserved is a code the document is wrong
// about, once something reaches it. The three that shipped in S14 are named
// so that unreserving the next one is a deliberate edit rather than a thing
// somebody notices a year later.
func TestNoShippedCodeIsStillMarkedReserved(t *testing.T) {
	doc := cliDoc(t)
	for _, row := range []struct {
		code int
		what string
	}{
		{exitcode.Held, "the hold verbs and the prerelease auto-hold ship"},
		{exitcode.MachineGate, "`dockhand promote --auto` exits with it"},
		{exitcode.PromotionPending, "the reconciler's publish slot exits with it"},
		{exitcode.WitnessAPI, "an unanswered forge lookup exits with it"},
	} {
		re := regexp.MustCompile(fmt.Sprintf("\\| `%d` \\| `[A-Za-z]+` \\| \\*reserved:\\*", row.code))
		assert.NotRegexp(t, re, doc, "%d is not reserved any more: %s", row.code, row.what)
	}
}

// Every reason a refusal names is spelled in the document. The band says
// which KIND of problem this is and the reason says WHICH, so a consumer
// filtering on reasons has nowhere but cli.md to learn what they are.
func TestEveryRefusalReasonIsNamedInTheDoc(t *testing.T) {
	doc := cliDoc(t)
	for _, reason := range []string{
		"open-proposal",
		"no-positive-evidence",
		"promote-is-human",
		"machine-publish-disabled",
		"machine-publish-no-verifier",
		"machine-republish",
		"forge-lookup-failed",
		"stealth-suspected",
		"pass-limit",
		"promotion-pending",
		"no-proposal",
		"unknown-member",
		"empty-cohort",
		"unknown-withheld",
		"not-withheld",
		"cannot-force",
		"forced-conflict",
	} {
		assert.Contains(t, doc, "`"+reason+"`",
			"a reason a script can branch on must be documented where the reasons are")
	}
}

// Every verb the tree registers is mentioned in the document, and every
// persistent declaration with it. A verb that ships undocumented is a verb
// users find by reading --help, which is the state cli.md exists to
// replace.
func TestEveryVerbAndDeclarationIsMentioned(t *testing.T) {
	doc := cliDoc(t)
	root, _ := newRoot("test")
	for _, c := range root.Commands() {
		name := c.Name()
		switch name {
		case "help", "completion":
			// cobra's own, not dockhand's surface.
			continue
		}
		assert.True(t, strings.Contains(doc, "`"+name+"`") || strings.Contains(doc, "dockhand "+name),
			"`dockhand %s` ships and cli.md does not mention it", name)
	}
	for _, declaration := range []string{"--auto", "DOCKHAND_AUTO", "AI_AGENT", "--to-pr", "--superseded",
		"--keep-env", "--no-update", "--keep-merged", "--reclaim-orphans", "--exclude", "--force-withheld"} {
		assert.Contains(t, doc, declaration,
			"%s changes what an invocation means and must be documented", declaration)
	}
}
