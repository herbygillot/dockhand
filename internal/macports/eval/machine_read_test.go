package eval

import (
	"context"
	"encoding/json"
	"errors"
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
	}
}
