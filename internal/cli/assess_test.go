package cli

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/assess"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/stretchr/testify/require"
)

func TestAssessFrozenSourceWithoutStateDownloadsOrCatalogs(t *testing.T) {
	config, repo, downloads, catalogs := automaticCLI(t, "1.0")
	before, err := repo.ReadRefs(t.Context(), "refs/")
	require.NoError(t, err)
	path := filepath.Join(repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("dirty user edits"), 0600))
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	for _, version := range []string{"", "2.0", "v2.0"} {
		args := []string{"assess", "fixture", "--json"}
		if version != "" {
			args = append(args, "--at", version)
		}
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, config), stderr.String())
		var result assess.Result
		decodeResult(t, stdout.Bytes(), &result)
		require.Len(t, result.Ports, 1)
		port := result.Ports[0]
		require.Equal(t, "1.0", port.CurrentVersion)
		require.NotEmpty(t, result.Source.Commit)
		if version == "" {
			require.Equal(t, portedit.InputFound, port.Outcome)
			require.Nil(t, port.Release)
		} else {
			require.Equal(t, portedit.CandidateChecked, port.Outcome, "%+v", port.Findings)
			require.Equal(t, "v2.0", port.Release.Tag)
		}
		require.Zero(t, downloads.Load())
		require.Zero(t, catalogs.Load())
		require.NoDirExists(t, filepath.Dir(config.DBPath))
		after, err := repo.ReadRefs(t.Context(), "refs/")
		require.NoError(t, err)
		require.Equal(t, before, after)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "dirty user edits", string(data))
		entries, err := os.ReadDir(scratch)
		require.NoError(t, err)
		require.Empty(t, entries)
	}
}

func TestAssessKeepsIndependentFailuresAndDeduplicates(t *testing.T) {
	config, _, _, _ := automaticCLI(t, "1.0")
	var out, stderr bytes.Buffer
	err := Run(t.Context(), []string{"assess", "missing", "fixture", "fixture", "--json"}, Streams{Out: &out, Err: &stderr}, config)
	require.ErrorContains(t, err, "unknown")
	require.Equal(t, 1, ExitCode(err))
	var result assess.Result
	decodeResult(t, out.Bytes(), &result)
	require.Len(t, result.Ports, 2)
	require.Equal(t, portedit.Unknown, result.Ports[0].Outcome)
	require.Equal(t, portedit.InputFound, result.Ports[1].Outcome)
	out.Reset()
	err = Run(t.Context(), []string{"assess", "fixture", "--at", "does-not-exist", "--json"}, Streams{Out: &out, Err: &stderr}, config)
	require.Error(t, err)
	decodeResult(t, out.Bytes(), &result)
	require.Equal(t, portedit.Unknown, result.Ports[0].Outcome)
}

func TestAssessValidatesSelectorsBeforeOpeningRepository(t *testing.T) {
	for _, args := range [][]string{
		{"assess"}, {"assess", "fixture", "--all"}, {"assess", "--all", "--category", "devel"},
		{"assess", "--maintainer", "*"}, {"assess", "fixture", "other", "--at", "2"},
		{"assess", "--all", "--at", "2"}, {"assess", "fixture", "--at", ""},
	} {
		config := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "missing", "state.db")}
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git rev-parse")
		require.NoDirExists(t, filepath.Dir(config.DBPath))
	}
}

func TestAssessHumanOutputExplainsScope(t *testing.T) {
	config, _, _, _ := automaticCLI(t, "1.0")
	var out, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"assess", "fixture", "-v"}, Streams{Out: &out, Err: &stderr}, config))
	require.Contains(t, out.String(), "fixture: ready")
	require.Contains(t, out.String(), "input: devel/fixture/Portfile:")
	require.Contains(t, out.String(), "candidate: not-tested")
	require.Contains(t, out.String(), "verification: not-tested")
	require.Contains(t, stderr.String(), "working-tree edits are excluded")
}

func TestAssessIndexedSelectionKeepsSubportsAndCoverageProblems(t *testing.T) {
	if _, err := exec.LookPath("portindex"); err != nil {
		t.Skip("native portindex required")
	}
	t.Setenv("HOME", t.TempDir())
	config, repo, downloads, catalogs := automaticCLI(t, "1.0")
	head, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	before, data, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	data = append(data, []byte("maintainers @contributor\nsubport fixture-extra {}\n")...)
	tree, err = repo.EditTree(t.Context(), tree, []git.FileEdit{
		{Path: "devel/fixture/Portfile", Before: before, After: data, Mode: before.Mode},
		{Path: "devel/broken/Portfile", After: []byte("PortSystem 1.0\nerror broken\n"), Mode: 0100644},
	})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{head}, Message: "assessment selection fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: head}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	for _, args := range [][]string{
		{"assess", "--all", "--json"},
		{"assess", "--maintainer", "contributor@github", "--category", "devel", "--json"},
	} {
		var out, stderr bytes.Buffer
		require.Error(t, Run(t.Context(), args, Streams{Out: &out, Err: &stderr}, config))
		var result assess.Result
		decodeResult(t, out.Bytes(), &result)
		require.Equal(t, commit, string(result.Source.Commit))
		require.Len(t, result.Ports, 3)
		require.Equal(t, "devel/broken/Portfile", result.Ports[0].Selector)
		require.Equal(t, portedit.Unknown, result.Ports[0].Outcome)
		require.Equal(t, "index-coverage", result.Ports[0].Findings[0].Code)
		require.Equal(t, "fixture", result.Ports[1].Selector)
		require.Equal(t, portedit.InputFound, result.Ports[1].Outcome)
		require.Equal(t, "fixture-extra", result.Ports[2].Selector)
		require.Equal(t, portedit.InputFound, result.Ports[2].Outcome)
	}
	require.Zero(t, downloads.Load())
	require.Zero(t, catalogs.Load())
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}

func TestAssessJournalResumesAndRefusesAnotherCommit(t *testing.T) {
	config, _, _, _ := automaticCLI(t, "1.0")
	journal := filepath.Join(t.TempDir(), "survey.jsonl")
	args := []string{"assess", "fixture", "--json", "--journal", journal}
	var out, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), args, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	var result assess.Result
	decodeResult(t, out.Bytes(), &result)
	require.Len(t, result.Ports, 1)
	require.Zero(t, result.Skipped)
	lines := journalLines(t, journal)
	require.Len(t, lines, 2, "the source, then one line per port")
	require.Contains(t, lines[0], `"Source":{"Commit":"`+string(result.Source.Commit))
	require.Contains(t, lines[1], `"Selector":"fixture"`)
	require.Contains(t, lines[1], `"Outcome":"`+portedit.InputFound+`"`)

	out.Reset()
	require.NoError(t, Run(t.Context(), args, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	decodeResult(t, out.Bytes(), &result)
	require.Empty(t, result.Ports)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, lines, journalLines(t, journal), "a rerun appends nothing it already holds")
	out.Reset()
	require.NoError(t, Run(t.Context(), []string{"assess", "fixture", "--journal", journal}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	require.Contains(t, out.String(), "Assessed 0: 0 ready, 0 candidate ready, 0 blocked, 0 unsupported, 0 unknown. 1 already in the journal.")

	data, err := os.ReadFile(journal)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(journal, []byte(strings.Replace(string(data), string(result.Source.Commit), strings.Repeat("0", 40), 1)), 0o644))
	err = Run(t.Context(), args, Streams{Out: &out, Err: &stderr}, config)
	require.ErrorContains(t, err, "holds an assessment of commit")
}

func journalLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}
