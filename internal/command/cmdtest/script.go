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
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
echo "building from $1"
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "`+outcome+`"`+phase+`}]}
JSON
`), 0o755))
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
