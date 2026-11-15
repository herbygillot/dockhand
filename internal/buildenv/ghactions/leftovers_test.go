package ghactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// leftoverFork keeps several checks' recorded branches in a local fork,
// without asking GitHub (#241).
func leftoverFork(t *testing.T) (*Provider, []model.GuestExecution) {
	t.Helper()
	root := t.TempDir()
	testsupport.Git(t, root, "init", "-q")
	testsupport.Git(t, root, "commit", "-q", "--allow-empty", "-m", "checked")
	commit := testsupport.Git(t, root, "rev-parse", "HEAD")
	fork := filepath.Join(t.TempDir(), "fork.git")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	repo, err := git.Open(t.Context(), root, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	p := &Provider{Repo: repo, Fork: func(context.Context) (buildenv.Fork, error) {
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
	}}
	var attempts []model.GuestExecution
	for i := range 3 {
		run := model.Run{ID: model.RunID(fmt.Sprintf("run_check%d", i))}
		branch := buildenv.CheckBranch(commit, run.ID)
		note := newLaunch("ada/macports-ports", branch, buildenv.Job{Run: run, Commit: commit}, launchPush)
		attempts = append(attempts, model.GuestExecution{ID: model.ExecutionID(fmt.Sprintf("github_%d", i)), Run: run.ID,
			Environment: model.Environment{Provider: buildenv.GitHub}, Attempt: 1, Launch: note.String()})
		testsupport.Git(t, root, "push", "-q", fork, commit+":refs/heads/"+branch)
	}
	return p, attempts
}

func TestAttemptLeftoversSkipsNotesFromAnotherFork(t *testing.T) {
	t.Parallel()
	p, attempts := leftoverFork(t)
	var stale launchNote
	require.NoError(t, json.Unmarshal([]byte(attempts[0].Launch), &stale))
	stale.Repository = "earlier/macports-ports"
	attempts[0].Launch = stale.String()
	found, err := p.AttemptLeftovers(t.Context(), attempts)
	require.NoError(t, err)
	require.Len(t, found, 2, "the current fork's checks are still listed")
}

func TestAttemptLeftoversWithNoConfiguredForkOrLoginIsQuiet(t *testing.T) {
	t.Parallel()
	for _, login := range []bool{false, true} {
		t.Run(fmt.Sprint("login missing ", login), func(t *testing.T) {
			t.Parallel()
			p, attempts := leftoverFork(t)
			p.Fork = func(context.Context) (buildenv.Fork, error) {
				if login {
					return buildenv.Fork{}, github.ErrNoCredentials
				}
				return buildenv.Fork{}, nil
			}
			found, err := p.AttemptLeftovers(t.Context(), attempts)
			require.NoError(t, err)
			require.Empty(t, found)
		})
	}
}

func TestAttemptLeftoversReadsTheForkOnce(t *testing.T) {
	t.Parallel()
	p, attempts := leftoverFork(t)
	reads := 0
	p.Repo.Hook = func(_ context.Context, args []string, run func() error) error {
		if args[0] == "ls-remote" {
			reads++
		}
		return run()
	}
	found, err := p.AttemptLeftovers(t.Context(), attempts)
	require.NoError(t, err)
	require.Len(t, found, 3)
	require.Equal(t, 1, reads)
}

// A remote that never answers is bounded even when the caller has no
// deadline, as an interrupted check's WithoutCancel has none (#241).
func TestSettleBoundsTheWholeRemoteOperation(t *testing.T) {
	t.Parallel()
	for _, delete := range []bool{false, true} {
		t.Run(fmt.Sprint("delete ", delete), func(t *testing.T) {
			t.Parallel()
			p, attempts := leftoverFork(t)
			realGit, err := exec.LookPath("git")
			require.NoError(t, err)
			blocked := "ls-remote"
			if delete {
				blocked = "push"
			}
			program := filepath.Join(t.TempDir(), "git")
			testsupport.WriteExecutable(t, program, "#!/bin/sh\ncase \" $* \" in *\" "+blocked+" \"*) exec /bin/sleep 120;; esac\nexec '"+strings.ReplaceAll(realGit, "'", "'\"'\"'")+"' \"$@\"\n")
			p.Repo.Executable = program
			bounded := false
			p.Repo.Hook = func(ctx context.Context, args []string, run func() error) error {
				if (!delete && args[0] == "ls-remote") || (delete && strings.Contains(strings.Join(args, " "), " push ")) {
					deadline, ok := ctx.Deadline()
					if !ok {
						return errors.New("the remote has no settlement deadline")
					}
					bounded = true
					require.LessOrEqual(t, time.Until(deadline), 30*time.Second)
					return run()
				}
				return run()
			}
			started := time.Now()
			_, err = p.Settle(t.Context(), buildenv.SettleRequest{Execution: attempts[0], Attempts: attempts[:1], Lock: freeLock{}})
			require.True(t, bounded, "Settle supplied the remote's deadline")
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Less(t, time.Since(started), 35*time.Second)
		})
	}
}
