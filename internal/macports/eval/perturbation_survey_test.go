//go:build surveymeasure

package eval

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/stretchr/testify/require"
)

func TestSurveyStandinAnswersHaveTheirOwnProvenance(t *testing.T) {
	e := liveEvaluator(t)
	e.Recording = true
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/effects/Portfile", `PortSystem 1.0
name effects
version 1
set r [list [sysctl hw.activecpu] [findBinary probe-110-only] [binaryInPath other-110-only] [_mportsearchpath probe-110-only {/usr/bin} 0 1] [_mportsearchpath probe-110-only {/usr/bin}]]
lappend r [file exists /dockhand-standin/bin/probe-110-only] [file executable /dockhand-standin/bin/probe-110-only]
lappend r [file exists /host-only-110] [catch {sysctl unknown.key}]
description {*}$r
`)
	bound, err := tree.Select(model.Target{Name: "effects", Portfile: "devel/effects/Portfile"})
	require.NoError(t, err)
	ctx := WithStandinAnswers(t.Context())
	got, err := e.Observe(ctx, bound, macports.ObservationRequest{Platform: monterey, Declarations: true, SelectedOnly: true})
	require.NoError(t, err)
	values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["description"])
	require.Empty(t, errs)
	require.Equal(t, []string{"1099511627776", "/dockhand-standin/bin/probe-110-only", "/dockhand-standin/bin/other-110-only", "/dockhand-standin/bin/probe-110-only", "1", "1", "1", "0", "1"}, values)
	p := got.Ports["effects"]
	sources := map[string]bool{}
	for _, x := range p.Ledger {
		if x.Source == "standin" {
			sources[x.Command] = true
		}
	}
	for _, name := range []string{"sysctl", "findBinary", "binaryInPath", "_mportsearchpath", "file"} {
		require.True(t, sources[name], name)
	}
	count := 0
	for _, x := range p.Declarations {
		if x.Command == "dockhand.host-access" && len(x.Values) > 1 && x.Values[1] == "standin" {
			count++
		}
		if x.Command == "dockhand.host-access" && len(x.Values) > 3 && x.Values[1] == "closed" {
			require.NotContains(t, x.Values[3], "probe-110-only")
			require.NotEqual(t, "sysctl hw.activecpu", x.Values[3])
		}
	}
	require.GreaterOrEqual(t, count, 7)
	require.Empty(t, surveyScript(t.Context()), "unselected tagged evaluations remain normal")
	normal, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: monterey, Declarations: true, SelectedOnly: true})
	// The unhandled first closed sysctl ends this Portfile; the variant is per call.
	require.Error(t, err)
	_ = normal
}
