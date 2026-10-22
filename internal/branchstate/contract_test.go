package branchstate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// contractCases are the facts and actions the Next contract is held to,
// one for each refusal Evaluate makes; each step of part 4 adds those its
// judgment brings.
var contractCases = []struct {
	name   string
	facts  Facts
	action Action
	mode   Mode
}{}

// A refusal's Next moves the branch on: another verb, or the same once
// something named has changed, never the refused action asked again with
// nothing to wait for (architecture, 2026-10-22).
func TestARefusalsNextAdvances(t *testing.T) {
	for _, c := range contractCases {
		verdict := Evaluate(c.facts, c.action, c.mode)
		if !verdict.Refused {
			continue
		}
		require.NotNil(t, verdict.Next, "%s: a refusal says what moves it on", c.name)
		require.True(t, verdict.Next.Advances(c.action), "%s: %+v asks %s again with nothing to wait for", c.name, *verdict.Next, c.action)
	}
	require.True(t, Next{Verb: "check"}.Advances("submit"))
	require.True(t, Next{Verb: "submit", After: AfterLook}.Advances("submit"))
	require.False(t, Next{Verb: "submit"}.Advances("submit"))
}
