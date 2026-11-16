package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

func TestToleranceCountsEveryOriginalRead(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	require.NoError(t, os.MkdirAll(filepath.Join(base, "port1.0"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(base, "port1.0/portbuild.tcl"), []byte("default build.jobs {[portbuild::build_getjobs]}\n"), 0600))
	read := macports.Declaration{Command: "dockhand.host-access", Values: []string{"sysctl cpu", "closed", "base", "sysctl hw.activecpu"}, Frames: []macports.SourceFrame{
		{Command: "default_check build.jobs {} read"},
		{File: filepath.Join(base, "port1.0/portutil.tcl"), Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		{Command: "subst {[portbuild::build_getjobs]}"},
		{Command: "portbuild::build_getjobs"},
		{File: filepath.Join(base, "port1.0/portbuild.tcl"), Command: "sysctl hw.activecpu"},
	}}
	port := macports.PortObservation{HostAccess: true, ModeledHostAccess: true, BaseLibrary: base, Declarations: []macports.Declaration{read}, Problems: []string{"sysctl cpu"}, MachineReadExperiment: &macports.MachineReadExperiment{Outcome: "same", Varied: []int{0}}}
	observation := macports.Observation{Modeled: true, Snapshot: macports.Snapshot{Root: root}, Ports: map[string]macports.PortObservation{"fixture": port}}
	verdict := measureTolerance(t.Context(), observation, "fixture", []byte("build.args -j${build.jobs}"), nil)
	require.Equal(t, toleranceVerdict{Evaluated: true}, verdict)
	// Equality and a varied CPU read must not cover the next original read.
	port.Declarations = append(port.Declarations, macports.Declaration{Command: "dockhand.host-access", Values: []string{"unvaried", "closed", "portfile", "file exists /outside/unvaried"}})
	port.Problems = append(port.Problems, "unvaried")
	observation.Ports["fixture"] = port
	verdict = measureTolerance(t.Context(), observation, "fixture", nil, nil)
	require.Equal(t, toleranceVerdict{Evaluated: true, Inconclusive: true, Left: true}, verdict)
	observation.Modeled = false
	verdict = measureTolerance(t.Context(), observation, "fixture", nil, nil)
	require.Equal(t, toleranceVerdict{Evaluated: true, Inconclusive: true}, verdict)
	verdict = measureTolerance(t.Context(), observation, "fixture", nil, errors.New("not evaluated"))
	require.False(t, verdict.Evaluated)
	require.NotEmpty(t, verdict.ErrorCode)
	counts := toleranceCounts{}
	counts.add(&toleranceVerdict{Evaluated: true, Inconclusive: true, Left: true})
	counts.add(&verdict)
	require.Equal(t, toleranceCounts{Evaluated: 1, Inconclusive: 1, Left: 1, NotEvaluated: 1}, counts)
}

func TestToleranceVerdictIsRecordedBesideEqualExperiment(t *testing.T) {
	root, source := recordTree(t)
	file := filepath.Join(t.TempDir(), "record.db")
	journal, err := OpenJournal(file + ".jsonl")
	require.NoError(t, err)
	record, err := OpenRecord(t.Context(), file, "current", false, RunInfo{Tree: root}, root, "", journal)
	require.NoError(t, err)
	require.NoError(t, record.Begin(source))
	v := &toleranceVerdict{Evaluated: true, Inconclusive: true, Left: true}
	o := observed{Platform: model.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}, Modeled: true, Tolerance: v, Observation: macports.PortObservation{MachineReadExperiment: &macports.MachineReadExperiment{Outcome: "same", Varied: []int{0}}}}
	port := Port{Selector: "fixture", observed: []observed{o}}
	port.Experiments = experimentReports(port.observed)
	require.NoError(t, record.Record(port))
	require.NoError(t, record.Close())
	require.NoError(t, journal.Close())
	db, err := sql.Open("sqlite", "file:"+file+"?mode=ro&immutable=1")
	require.NoError(t, err)
	defer db.Close()
	var evaluated, inconclusive, left bool
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT evaluated,inconclusive,"left" FROM tolerance_context`).Scan(&evaluated, &inconclusive, &left))
	require.True(t, evaluated && inconclusive && left)
	data, err := os.ReadFile(file + ".jsonl")
	require.NoError(t, err)
	require.Contains(t, string(data), `"Tolerance":{"Evaluated":true,"Inconclusive":true,"Left":true}`)
	require.Contains(t, string(data), `"Outcome":"same"`)
}
