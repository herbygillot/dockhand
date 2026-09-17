package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
)

func rows() []workflow.Contribution {
	return []workflow.Contribution{
		{Port: "jq", Change: "1.7 -> 1.8.1", Phase: "publication", State: "published", Next: "PR open, 3 checks pending", PullRequest: "https://github.com/macports/macports-ports/pull/1", ChangeID: "change_jq", Branch: "dockhand/bump/jq",
			History: []workflow.ContributionJob{{JobID: "job_1", Action: record.Bump, State: record.JobCompleted, AcceptedAt: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}}},
		{Port: "deno", Change: "verification", Phase: "verification", State: "building on macOS 26", Next: "verification pending", Active: &workflow.ActiveJob{JobID: "job_2", Action: record.Verify, Detail: "Verification running"}},
	}
}

func key(k string) tea.KeyMsg {
	if k == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestTableShowsRowsAndExpandsIdentifiersOnSelection(t *testing.T) {
	m := newModel(Options{Poll: func(context.Context) (workflow.Overview, error) { return workflow.Overview{}, nil }})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(snapshotMsg{overview: workflow.Overview{Contributions: rows()}})
	view := m.View()
	require.Contains(t, view, "PORT")
	require.Contains(t, view, "jq")
	require.Contains(t, view, "1.7 -> 1.8.1")
	require.Contains(t, view, "PR open, 3 checks pending")
	require.NotContains(t, view, "change_jq", "identifiers wait for expansion")
	m.Update(key("enter"))
	view = m.View()
	require.Contains(t, view, "contribution: change_jq")
	require.Contains(t, view, "PR: https://github.com/macports/macports-ports/pull/1")
	require.Contains(t, view, "bump: completed (job_1)")
	m.Update(key("j"))
	view = m.View()
	require.Contains(t, view, "active: verify job_2; Verification running")
}

func TestKeysMapOntoVerbsWithConfirmation(t *testing.T) {
	var ran []string
	m := newModel(Options{
		Poll: func(context.Context) (workflow.Overview, error) { return workflow.Overview{Contributions: rows()}, nil },
		Run: func(_ context.Context, args []string, out io.Writer) error {
			ran = append(ran, strings.Join(args, " "))
			_, _ = io.WriteString(out, "Accepted\n")
			return nil
		},
	})
	m.runner.send = func(tea.Msg) {}
	m.Update(snapshotMsg{overview: workflow.Overview{Contributions: rows()}})
	_, cmd := m.Update(key("v"))
	require.Nil(t, cmd)
	require.Contains(t, m.View(), "Run dockhand verify for jq? y/n")
	m.Update(key("n"))
	require.Contains(t, m.View(), "jq: verify not started")
	require.Empty(t, ran)
	_, cmd = m.Update(key("r"))
	require.NotNil(t, cmd, "refresh runs without confirmation")
	msg := cmd()
	require.Equal(t, doneMsg{port: "jq", verb: "refresh"}, msg)
	require.Equal(t, []string{"refresh --change change_jq"}, ran)
	m.Update(msg)
	require.Contains(t, m.View(), "jq: refresh started")
	_, cmd = m.Update(key("p"))
	require.Nil(t, cmd)
	_, cmd = m.Update(key("y"))
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, "publish --change change_jq --detach", ran[1], "work-starting verbs detach; the table's processing carries them")
}

func TestVerbArgsForStandaloneWork(t *testing.T) {
	standalone := rows()[1]
	args, problem := verbArgs("cancel", standalone)
	require.Empty(t, problem)
	require.Equal(t, []string{"cancel", "--job", "job_2"}, args)
	args, problem = verbArgs("verify", standalone)
	require.Empty(t, problem)
	require.Equal(t, []string{"verify", "deno", "--detach"}, args)
	args, problem = verbArgs("bump", rows()[0])
	require.Empty(t, problem)
	require.Equal(t, []string{"bump", "jq", "--detach"}, args, "a bump continues the port's open contribution or starts afresh")
	_, problem = verbArgs("publish", standalone)
	require.Contains(t, problem, "not a tracked contribution")
	standalone.Active = nil
	_, problem = verbArgs("cancel", standalone)
	require.Equal(t, "nothing is pending", problem)
}

func TestOpenKeysReportMissingTargets(t *testing.T) {
	var opened []string
	m := newModel(Options{Open: func(target string) error { opened = append(opened, target); return nil }})
	m.Update(snapshotMsg{overview: workflow.Overview{Contributions: rows()}})
	m.Update(key("o"))
	require.Equal(t, []string{"https://github.com/macports/macports-ports/pull/1"}, opened)
	m.Update(key("l"))
	require.Contains(t, m.View(), "jq: no log recorded")
	_, cmd := m.Update(key("q"))
	require.NotNil(t, cmd)
}

func TestProcessorRunsWhileTheTableIsOpenAndStopsWithIt(t *testing.T) {
	started := make(chan struct{})
	m := newModel(Options{
		Poll: func(context.Context) (workflow.Overview, error) { return workflow.Overview{}, nil },
		Processor: func(ctx context.Context, say func(scope, text string)) error {
			say("jq", "Verification admitted")
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	var lines []tea.Msg
	m.runner.send = func(msg tea.Msg) { lines = append(lines, msg) }
	cmd := m.startProcessor()
	require.True(t, m.processing)
	require.Contains(t, m.View(), "dockhand status (processing)")
	finished := make(chan tea.Msg, 1)
	go func() { finished <- cmd() }()
	<-started
	m.stopProcessor()
	require.Nil(t, <-finished, "a canceled processor ends quietly")
	require.False(t, m.processing)
	require.Equal(t, []tea.Msg{lineMsg{port: "jq", text: "Verification admitted"}}, lines)
	m.Update(processorMsg{err: io.ErrUnexpectedEOF})
	require.Contains(t, m.View(), "processing stopped: unexpected EOF")
}

func TestRetiredRowsHideUntilHistoryIsAsked(t *testing.T) {
	retired := workflow.Contribution{Port: "xplr", Change: "1.1.1 -> 1.1.2", Phase: "done", State: "merged", Next: "merged; branches cleaned", Retired: true}
	m := newModel(Options{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(snapshotMsg{overview: workflow.Overview{Contributions: append(rows(), retired)}})
	view := m.View()
	require.NotContains(t, view, "xplr")
	require.Contains(t, view, "1 retired hidden (h shows)")
	m.Update(key("j"))
	m.Update(key("j"))
	require.Equal(t, 1, m.cursor, "the cursor stays within the visible rows")
	m.Update(key("h"))
	view = m.View()
	require.Contains(t, view, "xplr")
	require.NotContains(t, view, "retired hidden")
	m.Update(key("j"))
	require.Equal(t, "xplr", m.selected().Port)
	m.Update(key("h"))
	require.Equal(t, "deno", m.selected().Port, "hiding again clamps the cursor to the last visible row")
}
