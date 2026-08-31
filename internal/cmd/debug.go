package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// debugTarget resolves a branch or port to its verification
// environment: the worker VM still holding the target's most recent
// verification — a build in flight, or a failure kept as the debug
// handle. A released environment is gone and says so; these verbs
// never boot anything.
func debugTarget(ctx context.Context, rs *runstate.Context, target string) (*git.Repo, verifyNote, error) {
	// A worker name addresses its environment directly, no branch
	// involved: the environment a pre-mint --verify failure keeps has
	// no branch, and the printed handle is all a user holds.
	if strings.HasPrefix(target, tart.WorkerPrefix) {
		ok, err := tart.HasVM(ctx, target)
		if err != nil {
			return nil, verifyNote{}, err
		}
		if !ok {
			return nil, verifyNote{}, fmt.Errorf("environment %s no longer exists", target)
		}
		return nil, verifyNote{
			Schema: noteSchema, State: "kept",
			Job: verify.Job{Provider: "tart", ID: target},
		}, nil
	}
	repo, err := rs.Repo(ctx)
	if err != nil {
		return nil, verifyNote{}, err
	}
	branch, err := resolveDockhandBranch(ctx, repo, target)
	if err != nil {
		return nil, verifyNote{}, err
	}
	n, err := latestNote(ctx, repo, branch)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return nil, verifyNote{}, fmt.Errorf("%s has no verification record; `dockhand verify %s` starts one", branch, branch)
		}
		return nil, verifyNote{}, err
	}
	name := n.Job.ID
	if n.State != "running" && n.Handle == "" {
		return nil, verifyNote{}, fmt.Errorf("%s: verification %s and its environment was released; `dockhand verify %s` starts a fresh one", branch, n.State, branch)
	}
	prov, err := vmProvider(ctx)
	if err != nil {
		return nil, verifyNote{}, err
	}
	if _, err := prov.Poll(ctx, n.Job); errors.Is(err, verify.ErrUnknownJob) {
		return nil, verifyNote{}, fmt.Errorf("%s: environment %s no longer exists", branch, name)
	}
	return repo, n, nil
}

// latestNote is the branch's most recent verification record: the
// tip's note, or the nearest one behind it.
func latestNote(ctx context.Context, repo *git.Repo, branch string) (verifyNote, error) {
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return verifyNote{}, err
	}
	for _, sha := range shas {
		if n, err := readNote(ctx, repo, sha); err == nil {
			return n, nil
		}
	}
	return verifyNote{}, git.ErrNoNote
}

// logAction prints the build log out of the target's verification
// environment, as it stands right now — mid-build for a running job,
// complete for a kept failure.
type logAction struct {
	target string
}

var _ Action = logAction{}

func (a logAction) Execute(ctx context.Context, rs *runstate.Context) error {
	_, n, err := debugTarget(ctx, rs, a.target)
	if err != nil {
		return err
	}
	prov, err := vmProvider(ctx)
	if err != nil {
		return err
	}
	log, err := prov.Log(ctx, n.Job)
	if err != nil {
		return err
	}
	if log == "" {
		fmt.Fprintln(rs.Err, "no log output yet")
		return nil
	}
	fmt.Fprint(rs.Out, log)
	return nil
}

// shellAction opens an interactive shell inside the target's
// verification environment, which is what a kept failure is for:
// the build's remains exactly as the guest left them.
type shellAction struct {
	target string
}

var _ Action = shellAction{}

func (a shellAction) Execute(ctx context.Context, rs *runstate.Context) error {
	_, n, err := debugTarget(ctx, rs, a.target)
	if err != nil {
		return err
	}
	what := n.State
	if n.Port != "" {
		what += " verification of " + n.Port
	} else {
		what += " environment"
	}
	fmt.Fprintf(rs.Err, "connecting to %s (%s)\n", n.Job.ID, what)
	// An interactive session wants the process's real terminal, not the
	// run's buffered streams: tart exec attaches through the guest
	// agent — the same channel verification itself drives the guest by.
	// The TTY is requested only when there is one: -t on a piped stdin
	// dies on the terminal-size ioctl, and a pipe of commands is a
	// legitimate way to use a shell.
	args := []string{"exec", "-i"}
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		args = append(args, "-t")
	}
	args = append(args, n.Job.ID, "/bin/zsh", "-l")
	cmd := exec.CommandContext(ctx, "tart", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		// The shell's own exit status is the user's business.
		return nil
	}
	return err
}

// Log builds the log subcommand.
func Log() *cobra.Command {
	return &cobra.Command{
		Use:   "log <branch|port|worker>",
		Short: "Print the build log from a verification environment",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return logAction{target: args[0]}, nil
		}),
	}
}

// Shell builds the shell subcommand.
func Shell() *cobra.Command {
	return &cobra.Command{
		Use:   "shell <branch|port|worker>",
		Short: "Open a shell inside a verification environment",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return shellAction{target: args[0]}, nil
		}),
	}
}
