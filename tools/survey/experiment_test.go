package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

func TestExperimentReportStoresOnlyProductSummary(t *testing.T) {
	root, source := recordTree(t)
	dbpath := filepath.Join(t.TempDir(), "survey.db")
	journalpath := filepath.Join(t.TempDir(), "survey.jsonl")
	journal, err := OpenJournal(journalpath)
	require.NoError(t, err)
	record, err := OpenRecord(t.Context(), dbpath, "current", false, RunInfo{Tree: root}, root, "", journal)
	require.NoError(t, err)
	require.NoError(t, record.Begin(source))
	platform := model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}
	summary := &macports.MachineReadExperiment{Outcome: "differs", Varied: []int{0}, Field: "distfiles"}
	original := macports.PortObservation{MachineReadExperiment: summary, Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"original question", "closed", "base", "sysctl hw.activecpu"}}}}
	port := Port{Selector: "fixture", observed: []observed{{Platform: platform, Modeled: true, Observation: original}}}
	port.Experiments = experimentReports(port.observed)
	require.NoError(t, record.Record(port))
	require.NoError(t, record.Close())
	require.NoError(t, journal.Close())
	db, err := sql.Open("sqlite", dbpath)
	require.NoError(t, err)
	defer db.Close()
	var encoded, question string
	require.NoError(t, db.QueryRow(`SELECT summary FROM machine_read_experiment`).Scan(&encoded))
	require.Contains(t, encoded, `"Field":"distfiles"`)
	require.NoError(t, db.QueryRow(`SELECT question FROM machine_read_original WHERE ord=0`).Scan(&question))
	require.Contains(t, question, "sysctl hw.activecpu")
	data, err := os.ReadFile(journalpath)
	require.NoError(t, err)
	require.Contains(t, string(data), `"Experiments"`)
	require.NotContains(t, string(data), "1099511627776")
	require.NotContains(t, string(data), "/dockhand-standin/")
}
