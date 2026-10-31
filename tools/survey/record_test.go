package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// recordTree is a Git tree with one PortGroup in _resources, at its
// commit.
func recordTree(t *testing.T) (string, model.Source) {
	t.Helper()
	root := t.TempDir()
	group := filepath.Join(root, "_resources", "port1.0", "group", "fixture-1.0.tcl")
	require.NoError(t, os.MkdirAll(filepath.Dir(group), 0o700))
	require.NoError(t, os.WriteFile(group, []byte("set fixture 1\n"), 0o600))
	testsupport.Git(t, root, "init", "-q")
	testsupport.Git(t, root, "add", ".")
	testsupport.Git(t, root, "commit", "-q", "-m", "fixture")
	commit := strings.TrimSpace(testsupport.Git(t, root, "rev-parse", "HEAD"))
	return root, model.Source{Commit: model.ObjectID(commit)}
}

func TestTheRecordKeepsEachContextsFactsAndContinues(t *testing.T) {
	t.Parallel()
	root, source := recordTree(t)
	file := filepath.Join(t.TempDir(), "survey.db")
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "survey.jsonl"))
	require.NoError(t, err)
	record, err := OpenRecord(t.Context(), file, "current", true, RunInfo{Tree: root}, root, "", journal)
	require.NoError(t, err)
	require.NoError(t, record.Begin(source))
	record.Bootstrap(eval.Bootstrap{Reads: []eval.BootstrapRead{{Command: "open", Path: "/opt/local/etc/macports/macports.conf"}},
		Aliases: []eval.WorkerAlias{{Name: "registry_active", Target: "registry::active"}}})
	record.Bootstrap(eval.Bootstrap{Reads: []eval.BootstrapRead{{Command: "open", Path: "/elsewhere"}}})

	work := filepath.Join(t.TempDir(), "tree")
	native := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	modelled := model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}
	port := Port{Selector: "fixture", portfile: "devel/fixture/Portfile",
		Assessment: portedit.Assessment{Outcome: portedit.Unknown, Findings: []portedit.Finding{{Check: "fetch", Status: portedit.Passed, Code: "fetch-checked"}}},
		observed: []observed{
			{Platform: native, Root: work, Info: macports.PortInfo{Name: "fixture", Options: map[string]string{
				"version": "1.0", "dockhand.code": "procedures 0123 options 4567",
				"dockhand.portgroups": "{fixture 1.0 <source>/_resources/port1.0/group/fixture-1.0.tcl}"}},
				Observation: macports.PortObservation{
					Reads:     []macports.PathRead{{Command: "source", Kind: "tree", Source: "host", Path: work + "/devel/fixture/files/extra.tcl", Count: 1}},
					Execs:     []macports.CommandRun{{Line: "/usr/bin/uname -s", Source: "host", Count: 2}},
					Ledger:    []macports.LedgerEntry{{Command: "registry_active", Source: "fresh", Subject: "zlib", Count: 1}},
					Distfiles: []macports.Distfile{{Name: "fixture-1.0.tar.gz", URLs: []string{"https://a.invalid/f", "https://b.invalid/f"}}},
					Hooks:     []macports.Hook{{Procedure: "userproc-post-destroot-0", Origin: work + "/devel/fixture/Portfile:9", Body: "system true"}}}},
			{Platform: modelled, Modeled: true, Err: errors.New("no such file")},
		}}
	require.NoError(t, record.Record(port))
	require.NoError(t, record.Finish(10, 20, 5))
	require.NoError(t, record.Close())

	db, err := sql.Open("sqlite", file)
	require.NoError(t, err)
	defer db.Close()
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(query, args...).Scan(&n), query)
		return n
	}
	require.Equal(t, 2, count(`SELECT count(*) FROM evaluation`))
	require.Equal(t, 1, count(`SELECT count(*) FROM evaluation WHERE outcome = 'failed' AND error = 'no such file' AND modeled = 1`))
	require.Equal(t, 1, count(`SELECT count(*) FROM field WHERE name = 'code:procedures' AND value = '0123'`))
	require.Equal(t, 0, count(`SELECT count(*) FROM field WHERE name = 'dockhand.code'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM read WHERE path = '<source>/devel/fixture/files/extra.tcl' AND kind = 'tree'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM exec WHERE command = '/usr/bin/uname -s' AND count = 2`))
	require.Equal(t, 1, count(`SELECT count(*) FROM registry WHERE alias = 'registry_active' AND subject = 'zlib'`))
	require.Equal(t, 2, count(`SELECT count(*) FROM distfile WHERE ord = 0`))
	require.Equal(t, 1, count(`SELECT count(*) FROM hook WHERE origin = '<source>/devel/fixture/Portfile:9'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM portgroup p JOIN tree_file f ON f.blob = p.blob AND f.path = '_resources/port1.0/group/fixture-1.0.tcl' WHERE p.version = '1.0'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM tree_file WHERE path = '_resources/port1.0/group'`), "a directory is kept by its tree")
	require.Equal(t, 1, count(`SELECT count(*) FROM bootstrap_read`), "one run's bootstrap is recorded once")
	require.Equal(t, 1, count(`SELECT count(*) FROM worker_alias WHERE target = 'registry::active'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM finding WHERE code = 'fetch-checked'`))
	require.Equal(t, 1, count(`SELECT count(*) FROM toolchain WHERE darwin = 24`), "a modelled context's toolchain is the facts table's")
	require.Equal(t, 1, count(`SELECT count(*) FROM run WHERE finished IS NOT NULL AND cores = 2.5 AND ports = 1`))
	data, err := os.ReadFile(filepath.Join(filepath.Dir(journal.path), "survey.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(data), `"Selector":"fixture"`, "the journal line is written beside")

	again, err := OpenRecord(t.Context(), file, "current", true, RunInfo{Tree: root}, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, again.Begin(source))
	require.True(t, again.Has("fixture"), "a rerun of the mode leaves out what it holds")
	require.NoError(t, again.Close())
	off, err := OpenRecord(t.Context(), file, "current", false, RunInfo{Tree: root}, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, off.Begin(source))
	require.False(t, off.Has("fixture"), "recording off is a run of its own")
	require.NoError(t, off.Close())
	other, err := OpenRecord(t.Context(), file, "current", true, RunInfo{Tree: root}, root, "", nil)
	require.NoError(t, err)
	require.ErrorContains(t, other.Begin(model.Source{Commit: model.ObjectID(strings.Repeat("b", 40))}), "use another file")
	require.NoError(t, other.Close())
}

// Every question's query reads the record's shape, a comparison's with
// the other record attached.
func TestTheQueriesReadTheRecord(t *testing.T) {
	t.Parallel()
	root, _ := recordTree(t)
	file := filepath.Join(t.TempDir(), "survey.db")
	record, err := OpenRecord(t.Context(), file, "current", true, RunInfo{Tree: root}, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, record.Close())
	db, err := sql.Open("sqlite", file)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, name := range []string{"guest", "older"} {
		_, err := db.Exec(`ATTACH ? AS `+name, file)
		require.NoError(t, err)
	}
	queries, err := filepath.Glob("queries/*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, queries)
	for _, query := range queries {
		data, err := os.ReadFile(query)
		require.NoError(t, err)
		rows, err := db.Query(string(data))
		require.NoError(t, err, query)
		require.NoError(t, rows.Close())
	}
}
