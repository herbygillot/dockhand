package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/stretchr/testify/require"
)

func TestMachineReadExperimentNeverEscapesStandinData(t *testing.T) {
	const marker = "PRIVATE-STANDIN-MARKER"
	var output []string
	ctx := progress.Observe(progress.WithReporter(t.Context(), func(u progress.Update) { output = append(output, u.Message) }), func(u progress.Update) { output = append(output, u.Message) })
	before := macports.Observation{Ports: map[string]macports.PortObservation{"fixture": {Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"question", "closed", "base", "sysctl hw.activecpu"}}}}}}
	for _, fail := range []bool{false, true} {
		result := performMachineReadExperiment(ctx, before, map[string][]int{"fixture": {0}}, nil, func(ctx context.Context) (macports.Observation, error) {
			progress.Report(ctx, marker)
			if fail {
				return macports.Observation{}, errors.New(marker)
			}
			return macports.Observation{Snapshot: macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": {Name: marker}}}, Ports: map[string]macports.PortObservation{"fixture": {Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{marker, "standin", "base", "sysctl hw.activecpu"}}}}}}, nil
		})
		data, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(data), marker)
		require.NotContains(t, strings.Join(output, ""), marker)
		for name, summary := range result {
			p := before.Ports[name]
			p.MachineReadExperiment = &summary
			before.Ports[name] = p
		}
		cache := map[string]macports.Observation{"original": before}
		encoded, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), marker)
		path := filepath.Join(t.TempDir(), "record.json")
		require.NoError(t, os.WriteFile(path, encoded, 0600))
		stored, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(stored), marker)
	}
}

func TestOneStandinCoversTwoOriginalReadsOfTheSameCall(t *testing.T) {
	info := macports.PortInfo{Name: "fixture", Fetch: &macports.FetchSemantics{Kind: "standard"}}
	question := macports.Declaration{Command: "dockhand.host-access", Values: []string{"cpu", "closed", "base", "sysctl hw.activecpu"}}
	before := macports.Observation{Snapshot: macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": info}}, Ports: map[string]macports.PortObservation{"fixture": {Declarations: []macports.Declaration{question, question}}}}
	standin := question
	standin.Values = append([]string(nil), question.Values...)
	standin.Values[1] = "standin"
	calls := 0
	result := performMachineReadExperiment(t.Context(), before, map[string][]int{"fixture": {0, 1}}, nil, func(context.Context) (macports.Observation, error) {
		calls++
		return macports.Observation{Snapshot: before.Snapshot, Ports: map[string]macports.PortObservation{"fixture": {Declarations: []macports.Declaration{standin}}}}, nil
	})["fixture"]
	require.Equal(t, 1, calls)
	require.Equal(t, "same", result.Outcome)
	require.Equal(t, []int{0, 1}, result.Varied)
}
