package ghactions

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// said is a Build that keeps what's said of it.
type said struct {
	buildenv.Build
	lines []string
}

type testRemoteNamer map[string]string

func (n testRemoteNamer) NameFromRemote(remote string) (string, error) { return n[remote], nil }

func (s *said) Progress(message string) { s.lines = append(s.lines, message) }
func (s *said) Waiting(message string)  { s.Progress(message) }
func (s *said) Holding(string) string   { return "" }

// The check's branch goes from your fork once the check is done with it,
// only while it holds the commit checked: one that moved since is left,
// and said, and neither fails the check
// (the test plan's step 2, item 20).
func TestTheChecksBranchGoesOnlyWhileItHoldsTheCommit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	checked := testsupport.Git(t, clone, "rev-parse", "HEAD")
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "later")
	later := testsupport.Git(t, clone, "rev-parse", "HEAD")
	fork := filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	repo, err := git.Open(t.Context(), clone, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	p := &Provider{Repo: repo, Identity: accountIdentity("ada"), RemoteNamer: testRemoteNamer{fork: "ada/macports-ports"}}
	target := buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}
	branch := BranchPrefix + checked[:12]

	testsupport.Git(t, clone, "push", "-q", fork, later+":refs/heads/"+branch)
	build := &said{}
	writeCtx, _, err := p.accountContext(t.Context())
	require.NoError(t, err)
	p.removeBranch(writeCtx, build, target, branch, checked)
	require.Equal(t, []string{"left " + branch + " on ada/macports-ports, since it has moved since the check"}, build.lines)
	require.Equal(t, later, testsupport.Git(t, fork, "rev-parse", branch))

	testsupport.Git(t, clone, "push", "-q", "-f", fork, checked+":refs/heads/"+branch)
	build = &said{}
	p.removeBranch(writeCtx, build, target, branch, checked)
	require.Equal(t, []string{"removed " + branch + " from ada/macports-ports"}, build.lines)
	require.Empty(t, testsupport.Git(t, fork, "branch", "--list", branch))

	build = &said{}
	p.removeBranch(writeCtx, build, target, branch, checked)
	require.Equal(t, []string{"removed " + branch + " from ada/macports-ports"}, build.lines, "one already gone is gone either way, and the check goes on")
}

// pushingAPI is a fork whose workflow runs and that lists no runs.
type pushingAPI struct{ API }

func (pushingAPI) WorkflowState(context.Context, string) (string, error) { return "active", nil }
func (pushingAPI) Runs(context.Context, string, string, string) ([]Run, error) {
	return nil, nil
}

// noting is a Build that keeps the notes its launches saved, the first and
// the one the start handed back.
type noting struct {
	said
	notes []string
}

func (n *noting) Launch(note string, start func(context.Context) (string, error)) error {
	n.notes = append(n.notes, note)
	after, err := start(context.Background())
	n.notes = append(n.notes, after)
	return err
}

func (*noting) Refer(string) error { return nil }
func (*noting) Canceled() bool     { return false }

// The check's branch is its own, the commit's and the check's ID's: one
// already at the commit that no attempt of the check noted pushing is
// refused by name, and a push the remote rejects is written down as
// having started nothing, which a stop then needs no run for (the
// lifecycle design's "GitHub").
func TestTheChecksOwnBranchAndAPushThatStartedNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	commit := testsupport.Git(t, clone, "rev-parse", "HEAD")
	repo, err := git.Open(t.Context(), clone, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	run := model.Run{ID: "run_abcdefghijklmnop", Number: 4}
	branch := buildenv.CheckBranch(commit, run.ID)
	require.Equal(t, "dockhand-check/"+commit[:12]+"-abcdefghijklmnop", branch)
	job := buildenv.Job{Run: run, Commit: commit, Directory: t.TempDir(), Execution: model.GuestExecution{Attempt: 1}}

	fork := filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	testsupport.Git(t, clone, "push", "-q", fork, commit+":refs/heads/"+branch)
	p := &Provider{Repo: repo, API: pushingAPI{}, Identity: accountIdentity("ada"), RemoteNamer: testRemoteNamer{fork: "ada/macports-ports"}, Fork: func(context.Context) (buildenv.Fork, error) {
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
	}}
	err = p.Execute(t.Context(), job, &noting{})
	require.ErrorContains(t, err, branch+" on ada/macports-ports is already at commit "+commit[:12]+", and no attempt of check-4 noted pushing it")

	// A remote that refuses the push: the note it saved first says only
	// that something may have started, and the one it hands back that
	// nothing did.
	refusing := filepath.Join(root, "refusing")
	testsupport.Git(t, root, "init", "-q", "-b", branch, refusing)
	p.Fork = func(context.Context) (buildenv.Fork, error) {
		return buildenv.Fork{Repository: "ada/macports-ports", PushURL: refusing}, nil
	}
	p.RemoteNamer = testRemoteNamer{refusing: "ada/macports-ports"}
	build := &noting{}
	require.Error(t, p.Execute(t.Context(), job, build))
	require.Len(t, build.notes, 2)
	require.NotContains(t, build.notes[0], "nothing")
	require.Contains(t, build.notes[0], `"action":"push"`)
	require.Contains(t, build.notes[1], `"nothing":"git push reported the branch rejected: `)
	require.Contains(t, build.notes[1], "[remote rejected]")
}

// forked is a clone holding one commit, and a bare fork beside it with
// nothing pushed: the clone's directory, the fork's, and the commit.
func forked(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	clone, fork := filepath.Join(root, "clone"), filepath.Join(root, "fork.git")
	testsupport.Git(t, root, "init", "-q", clone)
	testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "checked")
	testsupport.Git(t, root, "init", "-q", "--bare", fork)
	return clone, fork, testsupport.Git(t, clone, "rev-parse", "HEAD")
}

// movingAPI is a fork whose workflow runs and lists no runs, and whose
// first listing, the one a push reads its newest earlier run from, moves
// the branch first, as another push landing between the provider's read
// of the branch and the push's own.
type movingAPI struct {
	pushingAPI
	move  func()
	moved bool
}

func (a *movingAPI) Runs(context.Context, string, string, string) ([]Run, error) {
	if !a.moved && a.move != nil {
		a.moved = true
		a.move()
	}
	return nil, nil
}

// A push that sends nothing is written down as having started nothing,
// and a stop then needs no run for it: one dockhand refuses before it
// runs git push, here a push to MacPorts' own repository, and one whose
// own read of the branch finds it already at the commit, or elsewhere
// than the provider read it. The one already at the commit says nothing
// of an earlier push, whose run is still needed (the lifecycle design's
// "A launch known to have started nothing").
func TestAPushThatSendsNothingIsWrittenDownAsThat(t *testing.T) {
	t.Parallel()
	run := model.Run{ID: "run_abcdefghijklmnop", Number: 4}
	setup := func(t *testing.T, repository string, api API) (*Provider, buildenv.Job, string, string) {
		clone, fork, commit := forked(t)
		repo, err := git.Open(t.Context(), clone, git.Program{Env: testsupport.GitEnv(t)})
		require.NoError(t, err)
		p := &Provider{Repo: repo, API: api, Identity: accountIdentity("ada"), RemoteNamer: testRemoteNamer{fork: repository}, Sleep: func(context.Context, time.Duration) error { return nil },
			Fork: func(context.Context) (buildenv.Fork, error) {
				return buildenv.Fork{Repository: repository, PushURL: fork}, nil
			}}
		job := buildenv.Job{Run: run, Commit: commit, Directory: t.TempDir(), Execution: model.GuestExecution{Attempt: 1}}
		return p, job, fork, clone
	}
	nothing := func(t *testing.T, raw string) launchNote {
		t.Helper()
		var note launchNote
		require.NoError(t, json.Unmarshal([]byte(raw), &note))
		return note
	}

	t.Run("refused before sending", func(t *testing.T) {
		t.Parallel()
		p, job, fork, _ := setup(t, "macports/macports-ports", pushingAPI{})
		build := &noting{}
		require.ErrorContains(t, p.Execute(t.Context(), job, build), "dockhand doesn't push a branch in macports/macports-ports")
		require.Len(t, build.notes, 2)
		require.Empty(t, nothing(t, build.notes[0]).Nothing, "saved first as a launch that may start something")
		require.Contains(t, nothing(t, build.notes[1]).Nothing, "nothing was sent: dockhand doesn't push a branch in macports/macports-ports")
		require.Empty(t, testsupport.Git(t, fork, "branch", "--list"), "nothing reached the fork")
		left, err := p.Stop(t.Context(), buildenv.StopRequest{Attempts: attempts(nothing(t, build.notes[1])), Lock: freeLock{}})
		require.NoError(t, err, "ended: no run is needed")
		require.Empty(t, left)
	})

	t.Run("already at the commit", func(t *testing.T) {
		t.Parallel()
		api := &movingAPI{}
		p, job, fork, clone := setup(t, "ada/macports-ports", api)
		branch := buildenv.CheckBranch(job.Commit, run.ID)
		api.move = func() { testsupport.Git(t, clone, "push", "-q", fork, job.Commit+":refs/heads/"+branch) }
		build := &noting{}
		require.ErrorContains(t, p.Execute(t.Context(), job, build), "GitHub did not start "+Workflow)
		require.Len(t, build.notes, 2)
		require.Equal(t, "the branch was already at the commit, so nothing was sent", nothing(t, build.notes[1]).Nothing)
		sent := nothing(t, build.notes[1])
		left, err := p.Stop(t.Context(), buildenv.StopRequest{Attempts: attempts(sent), Lock: freeLock{}})
		require.NoError(t, err, "this launch needs no run")
		require.Empty(t, left)
		// An earlier attempt's push, whose run isn't listed, still needs
		// one: the branch at the commit says nothing of it.
		earlier := newLaunch("ada/macports-ports", branch, job, launchPush)
		_, err = p.Stop(t.Context(), buildenv.StopRequest{Attempts: attempts(earlier, sent), Lock: freeLock{}})
		require.ErrorContains(t, err, "GitHub lists no run yet for 1 push of "+branch+" it accepted")
	})

	t.Run("elsewhere than read", func(t *testing.T) {
		t.Parallel()
		api := &movingAPI{}
		p, job, fork, clone := setup(t, "ada/macports-ports", api)
		branch := buildenv.CheckBranch(job.Commit, run.ID)
		testsupport.Git(t, clone, "commit", "-q", "--allow-empty", "-m", "another")
		other := testsupport.Git(t, clone, "rev-parse", "HEAD")
		api.move = func() { testsupport.Git(t, clone, "push", "-q", fork, other+":refs/heads/"+branch) }
		build := &noting{}
		require.Error(t, p.Execute(t.Context(), job, build))
		require.Len(t, build.notes, 2)
		require.Contains(t, nothing(t, build.notes[1]).Nothing, "nothing was sent: ")
		require.Equal(t, other, testsupport.Git(t, fork, "rev-parse", branch), "the branch is as the other push left it")
		left, err := p.Stop(t.Context(), buildenv.StopRequest{Attempts: attempts(nothing(t, build.notes[1])), Lock: freeLock{}})
		require.NoError(t, err)
		require.Empty(t, left)
	})
}

// A push whose answer is lost, git push failing with no line for the
// branch though the push landed, leaves its note as first saved: a stop
// is not known for as long as no run is listed, however often it is
// asked, and the run that appears is canceled and ends it. The same holds
// for a run that appears only after the driver's ten minutes, and after
// the branch is gone (the lifecycle design's "GitHub", "none listed").
func TestAPushWhoseRunIsntSeenIsNotKnownUntilItIs(t *testing.T) {
	t.Parallel()
	run := model.Run{ID: "run_abcdefghijklmnop", Number: 4}
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		// lost makes git push land and then fail with no line for the
		// branch, as one whose answer was lost.
		lost bool
		said string
	}{
		{name: "answer lost", lost: true, said: "pushing to ada/macports-ports"},
		{name: "run after the wait and the branch", said: "GitHub did not start " + Workflow + " on branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clone, fork, commit := forked(t)
			program := git.Program{Env: testsupport.GitEnv(t)}
			if tc.lost {
				program.Executable = filepath.Join(t.TempDir(), "git")
				testsupport.WriteExecutable(t, program.Executable, "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = push ]; then\n    "+real+" \"$@\" >/dev/null 2>&1\n    echo 'fatal: the remote end hung up unexpectedly' >&2\n    exit 128\n  fi\ndone\nexec "+real+" \"$@\"\n")
			}
			repo, err := git.Open(t.Context(), clone, program)
			require.NoError(t, err)
			p := &Provider{Repo: repo, API: pushingAPI{}, Identity: accountIdentity("ada"), RemoteNamer: testRemoteNamer{fork: "ada/macports-ports"}, Sleep: func(context.Context, time.Duration) error { return nil },
				Fork: func(context.Context) (buildenv.Fork, error) {
					return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
				}}
			job := buildenv.Job{Run: run, Commit: commit, Directory: t.TempDir(), Execution: model.GuestExecution{Attempt: 1}}
			branch := buildenv.CheckBranch(commit, run.ID)
			build := &noting{}
			require.ErrorContains(t, p.Execute(t.Context(), job, build), tc.said)
			require.Len(t, build.notes, 2)
			require.Equal(t, build.notes[0], build.notes[1], "the note stays as first saved")
			require.Equal(t, commit, testsupport.Git(t, fork, "rev-parse", branch), "the push landed")
			if !tc.lost {
				testsupport.Git(t, fork, "branch", "-D", branch)
			}
			var note launchNote
			require.NoError(t, json.Unmarshal([]byte(build.notes[1]), &note))
			api := &stopAPI{}
			for range 3 {
				require.ErrorContains(t, stop(t, api, attempts(note)), "GitHub lists no run yet for 1 push of "+branch+" it accepted, and one can still start")
			}
			api.add(7, "in_progress")
			require.NoError(t, stop(t, api, attempts(note)))
			require.Equal(t, []int64{7}, api.canceled)
		})
	}
}

// branchAPI is a fork listing each check's runs on its own branch, which
// a cancel completes at once.
type branchAPI struct {
	pushingAPI
	runs     map[string][]Run
	canceled []int64
}

func (a *branchAPI) Runs(_ context.Context, _, branch, _ string) ([]Run, error) {
	return slices.Clone(a.runs[branch]), nil
}

func (a *branchAPI) Cancel(_ context.Context, _ string, id int64) error {
	a.canceled = append(a.canceled, id)
	for branch, runs := range a.runs {
		for i := range runs {
			if runs[i].ID == id {
				a.runs[branch][i].Status = "completed"
			}
		}
	}
	return nil
}

// Another check of the same commit pushes to a branch of its own, so it
// is never refused for finding the first check's branch at the commit,
// and a stop of one check cancels only the runs on its own branch (the
// lifecycle design's "GitHub", the check's own branch).
func TestAnotherCheckOfTheSameCommitIsOnItsOwnBranch(t *testing.T) {
	t.Parallel()
	clone, fork, commit := forked(t)
	repo, err := git.Open(t.Context(), clone, git.Program{Env: testsupport.GitEnv(t)})
	require.NoError(t, err)
	api := &branchAPI{runs: map[string][]Run{}}
	p := &Provider{Repo: repo, API: api, Identity: accountIdentity("ada"), RemoteNamer: testRemoteNamer{fork: "ada/macports-ports"}, Sleep: func(context.Context, time.Duration) error { return nil },
		Fork: func(context.Context) (buildenv.Fork, error) {
			return buildenv.Fork{Repository: "ada/macports-ports", PushURL: fork}, nil
		}}
	var notes []launchNote
	var branches []string
	for _, run := range []model.Run{{ID: "run_abcdefghijklmnop", Number: 4}, {ID: "run_qrstuvwxyzabcdef", Number: 5}} {
		build := &noting{}
		err := p.Execute(t.Context(), buildenv.Job{Run: run, Commit: commit, Directory: t.TempDir(), Execution: model.GuestExecution{Attempt: 1}}, build)
		require.ErrorContains(t, err, "GitHub did not start", "pushed, and no run listed: never refused for the other check's branch")
		require.Len(t, build.notes, 2)
		var note launchNote
		require.NoError(t, json.Unmarshal([]byte(build.notes[1]), &note))
		require.Empty(t, note.Nothing)
		notes = append(notes, note)
		branches = append(branches, buildenv.CheckBranch(commit, run.ID))
		require.Equal(t, commit, testsupport.Git(t, fork, "rev-parse", branches[len(branches)-1]))
	}
	require.NotEqual(t, branches[0], branches[1])
	api.runs[branches[0]] = []Run{{ID: 7, Attempt: 1, Status: "in_progress", URL: "https://github.com/ada/macports-ports/actions/runs/7"}}
	api.runs[branches[1]] = []Run{{ID: 8, Attempt: 1, Status: "in_progress", URL: "https://github.com/ada/macports-ports/actions/runs/8"}}
	left, err := p.Stop(t.Context(), buildenv.StopRequest{Attempts: attempts(notes[0]), Lock: freeLock{}})
	require.NoError(t, err)
	require.Empty(t, left)
	require.Equal(t, []int64{7}, api.canceled, "the other check's run is its own")
	require.Equal(t, "in_progress", api.runs[branches[1]][0].Status)
}
