package sqlite_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func helperCommand(t *testing.T, s *sqlite.Store, r record.Repository, mode string, extra ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteProcessHelper$")
	cmd.Env = append(os.Environ(), "DOCKHAND_TEST_PROCESS="+mode, "DOCKHAND_TEST_DB="+s.Path(), "DOCKHAND_TEST_REPOSITORY="+string(r.ID))
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}
func TestSQLiteProcessHelper(t *testing.T) {
	t.Parallel()
	mode := os.Getenv("DOCKHAND_TEST_PROCESS")
	if mode == "" {
		return
	}
	s, err := sqlite.Open(t.Context(), os.Getenv("DOCKHAND_TEST_DB"), sqlite.Options{})
	require.NoError(t, err)
	defer s.Close()
	repository := record.RepositoryID(os.Getenv("DOCKHAND_TEST_REPOSITORY"))
	switch mode {
	case "write":
		for range 12 {
			require.NoError(t, s.Update(t.Context(), repository, func(ctx context.Context, tx state.Tx) error {
				c, err := tx.Change(ctx, "counter")
				if err != nil {
					return err
				}
				n, err := strconv.Atoi(c.Branch)
				if err != nil {
					return err
				}
				c.Branch = strconv.Itoa(n + 1)
				return tx.PutChange(ctx, c)
			}))
		}
	case "hold":
		require.NoError(t, s.Update(t.Context(), repository, func(ctx context.Context, tx state.Tx) error {
			c, err := tx.Change(ctx, "counter")
			if err != nil {
				return err
			}
			c.Branch = "uncommitted"
			if err = tx.PutChange(ctx, c); err != nil {
				return err
			}
			fmt.Println("locked")
			<-ctx.Done()
			return ctx.Err()
		}))
	case "cycle":
		ms, err := strconv.ParseInt(os.Getenv("DOCKHAND_TEST_NOW"), 10, 64)
		require.NoError(t, err)
		now := time.UnixMilli(ms).UTC()
		e := workflow.Engine{State: state.Bind(s, record.Repository{ID: repository}), Provider: processProvider{log: os.Getenv("DOCKHAND_TEST_LOG")}, Now: func() time.Time { return now }}
		_, err = e.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{record.JobID(os.Getenv("DOCKHAND_TEST_JOB"))}})
		require.NoError(t, err)
	default:
		t.Fatal("unknown helper mode")
	}
}
func TestConcurrentProcessesAndAbandonedWriter(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	r := repository(t, s, "repo")
	require.NoError(t, s.Update(t.Context(), r.ID, func(ctx context.Context, tx state.Tx) error {
		return tx.PutChange(ctx, record.Change{ID: "counter", Branch: "0", Disposition: record.ChangeOpen})
	}))
	cmds := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for range 3 {
		cmd := helperCommand(t, s, r, "write")
		output := &bytes.Buffer{}
		cmd.Stdout = output
		cmd.Stderr = output
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd)
		outputs = append(outputs, output)
	}
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), outputs[i].String())
	}
	require.NoError(t, s.View(t.Context(), r.ID, func(ctx context.Context, reader state.Reader) error {
		c, err := reader.Change(ctx, "counter")
		require.Equal(t, "36", c.Branch)
		return err
	}))
	holder := helperCommand(t, s, r, "hold")
	out, err := holder.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	holder.Stderr = &stderr
	require.NoError(t, holder.Start())
	t.Cleanup(func() { holder.Process.Kill() })
	scanner := bufio.NewScanner(out)
	require.True(t, scanner.Scan())
	require.Equal(t, "locked", scanner.Text())
	require.NoError(t, holder.Process.Kill())
	require.Error(t, holder.Wait())
	require.NoError(t, s.Update(t.Context(), r.ID, func(ctx context.Context, tx state.Tx) error {
		c, err := tx.Change(ctx, "counter")
		if err != nil {
			return err
		}
		require.Equal(t, "36", c.Branch)
		c.Branch = "37"
		return tx.PutChange(ctx, c)
	}))
}

type processProvider struct {
	verify.Provider
	log string
}

func (p processProvider) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: "test", Platforms: []record.Platform{{OS: "darwin", Version: "25", Architecture: "arm64"}}, Capacity: 1, Isolated: true}, nil
}
func (p processProvider) Submit(ctx context.Context, r verify.Request) (verify.Submission, error) {
	file, err := os.OpenFile(p.log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return verify.Submission{}, err
	}
	_, err = fmt.Fprintln(file, r.ID)
	closeErr := file.Close()
	if err != nil {
		return verify.Submission{}, err
	}
	if closeErr != nil {
		return verify.Submission{}, closeErr
	}
	select {
	case <-time.After(75 * time.Millisecond):
	case <-ctx.Done():
		return verify.Submission{}, ctx.Err()
	}
	return verify.Submission{State: verify.Admitted, Run: record.ProviderRun{Provider: "test", RequestID: r.ID, RunID: "run_" + string(r.ID)}}, nil
}
func TestDriverProcessesClaimOneSubmission(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	r := repository(t, s, "repo")
	receipt := seed(t, s, r, "change")
	log := filepath.Join(t.TempDir(), "submissions")
	now := time.Now().UnixMilli()
	cmds := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for range 3 {
		cmd := helperCommand(t, s, r, "cycle", "DOCKHAND_TEST_JOB="+string(receipt.JobID), "DOCKHAND_TEST_LOG="+log, "DOCKHAND_TEST_NOW="+strconv.FormatInt(now, 10))
		output := &bytes.Buffer{}
		cmd.Stdout = output
		cmd.Stderr = output
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd)
		outputs = append(outputs, output)
	}
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), outputs[i].String())
	}
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Len(t, strings.Fields(string(data)), 1)
	require.NoError(t, s.View(t.Context(), r.ID, func(ctx context.Context, reader state.Reader) error {
		attempts, err := reader.AttemptsForJob(ctx, receipt.JobID)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, record.AttemptRunning, attempts[0].State)
		return nil
	}))
}
