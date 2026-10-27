package view

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// The preview says what would hold serve's submission, which a person's
// own goes ahead of: a major version, a raised floor, and a port its
// holds name first, each once, under Serve (the M1's C15 on rc2).
func TestThePreviewSaysWhatServeWouldHold(t *testing.T) {
	t.Parallel()
	after := model.Concern{Origin: model.FromHolds, Port: "opkssh", Rule: "after", Subject: "acceptance-first",
		Detail: "opkssh waits for acceptance-first's update to 0.19.1, as its holds say (holds.ports.\"opkssh\".after): it has no pull request open at that version"}
	plan := engine.SubmitPlan{Branch: model.Branch{Name: "dockhand/opkssh-0.19.1"},
		Majors: []model.Concern{{Origin: model.FromHolds, Port: "opkssh", Rule: "major", Detail: "opkssh moves from 0.16.0 to a new major version, 1.0.0"}},
		Floors: []model.Concern{{Origin: model.FromHolds, Port: "opkssh", Rule: "floor", Detail: "opkssh now needs macOS 13 or later"}},
		After:  []model.Concern{after, after}}
	var out bytes.Buffer
	WriteSubmitPlan(&out, plan, false)
	require.Contains(t, out.String(), "  Serve    would hold it:\n"+
		"           opkssh moves from 0.16.0 to a new major version, 1.0.0\n"+
		"             answer it: dockhand decide major -p opkssh --because \"…\"\n"+
		"           opkssh now needs macOS 13 or later\n"+
		"             answer it: dockhand decide floor -p opkssh --because \"…\"\n"+
		"           "+after.Detail+"\n"+
		"             answer it: dockhand decide after -p opkssh --because \"…\"\n", "each with the decision that answers it (the person's voice)")

	// One the person accepted holds nothing, and is said as decided.
	plan.Decided = map[string]model.Decision{"opkssh floor": {Subject: "opkssh floor", Answer: model.Accepted, Words: "macOS 12 is past its support"}}
	out.Reset()
	WriteSubmitPlan(&out, plan, false)
	require.NotContains(t, out.String(), "opkssh now needs macOS 13 or later")
	require.Contains(t, out.String(), "  Decided  opkssh floor: acceptable because macOS 12 is past its support\n")
	require.Equal(t, 1, strings.Count(out.String(), "waits for acceptance-first's update to"))
	require.Contains(t, out.String(), "ready to submit", "a person's own submission goes ahead")

	out.Reset()
	WriteSubmitPlan(&out, engine.SubmitPlan{Branch: model.Branch{Name: "dockhand/jq-1.8.1"}}, false)
	require.NotContains(t, out.String(), "Serve")
}
