package cmdtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// OnePort stands in for MacPorts: every directory defines one port, named
// after it.
type OnePort struct{}

// WithScript configures a command provider that reports jq with the given
// outcome.
func WithScript(t *testing.T, w World, outcome string) {
	t.Helper()
	script := filepath.Join(w.Home, "bin", "build-ports")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o755))
	phase := ""
	if outcome == "failed" {
		phase = `, "phase": "install"`
	}
	testsupport.WriteExecutable(t, script, `#!/bin/sh
echo "building from $1"
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "`+outcome+`"`+phase+`}]}
JSON
`)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[providers.command]\nrun = \"~/bin/build-ports\"\nname = \"my build box\"\n"), 0o644))
	UsePorts(Stands(t), OnePort{})
}

func (OnePort) Ports(_ context.Context, _ model.Source, directory string, _ model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	return []macports.PortInfo{{Name: filepath.Base(directory), Options: map[string]string{}}}, nil
}

func (OnePort) Directory(context.Context, model.Source, string) (string, error) {
	return "", errors.New("no such port")
}

// cells are hand-built results as the evidence's cells: recorded, but for
// a result not run, whose cell is of that kind.
func Cells(results []model.TargetResult) []engine.Cell {
	var cells []engine.Cell
	for _, result := range results {
		kind := engine.CellRecorded
		if result.Outcome == model.OutcomeNotRun {
			kind = engine.CellNotRun
		}
		cells = append(cells, engine.Cell{TargetResult: result, Kind: kind})
	}
	return cells
}

// withOutcomeScript configures a command provider that reports jq as
// ~/outcome says, "passed", or "failed" and the phase, install unless
// named, so a test can change what the next build does.
func WithOutcomeScript(t *testing.T, w World, config string) func(string) {
	t.Helper()
	script := filepath.Join(w.Home, "bin", "build-ports")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o755))
	testsupport.WriteExecutable(t, script, `#!/bin/sh
read -r outcome phase < "$HOME/outcome"
[ "$outcome" = failed ] && phase=", \"phase\": \"${phase:-install}\""
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "$outcome"$phase}]}
JSON
`)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[providers.command]\nrun = \"~/bin/build-ports\"\n"+config), 0o644))
	UsePorts(Stands(t), OnePort{})
	return func(outcome string) {
		require.NoError(t, os.WriteFile(filepath.Join(w.Home, "outcome"), []byte(outcome), 0o644))
	}
}
