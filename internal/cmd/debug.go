package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// debugEnv is one reachable verification environment: a build in
// flight, or a failure kept as the debug handle.
type debugEnv struct {
	Job   verify.Job
	State string
	Port  string
	Plat  string
}

// debugTarget resolves a branch, port, or worker name to one
// verification environment. With a verdict set per commit there can be
// several; --on picks a platform, one candidate needs no picking, and
// anything else is refused with the choices named. A released
// environment is gone and says so; these verbs never boot anything.
func debugTarget(ctx context.Context, rs *runstate.Context, target, on string) (debugEnv, error) {
	// A worker name addresses its environment directly, no branch
	// involved: the environment a pre-mint --verify failure keeps has
	// no branch, and the printed handle is all a user holds.
	if strings.HasPrefix(target, tart.WorkerPrefix) {
		ok, err := tart.HasVM(ctx, target)
		if err != nil {
			return debugEnv{}, err
		}
		if !ok {
			return debugEnv{}, fmt.Errorf("environment %s no longer exists", target)
		}
		return debugEnv{Job: verify.Job{Provider: "tart", ID: target}, State: "kept"}, nil
	}
	repo, err := rs.Repo(ctx)
	if err != nil {
		return debugEnv{}, err
	}
	branch, err := resolveDockhandBranch(ctx, repo, target)
	if err != nil {
		return debugEnv{}, err
	}
	n, err := latestNote(ctx, repo, branch)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return debugEnv{}, fmt.Errorf("%s has no verification record; `dockhand verify %s` starts one", branch, branch)
		}
		return debugEnv{}, err
	}

	reachable := map[string]verifyRun{}
	for plat, r := range n.Runs {
		if r.State == "running" || r.Handle != "" {
			reachable[plat] = r
		}
	}
	var plat string
	switch {
	case on != "":
		r, err := platform.Parse(on)
		if err != nil {
			return debugEnv{}, &UsageError{Err: err}
		}
		if _, ok := reachable[r.Name]; !ok {
			return debugEnv{}, fmt.Errorf("%s has no reachable environment on %s (%s)", branch, r.Name, summarizeNote(n))
		}
		plat = r.Name
	case len(reachable) == 1:
		for p := range reachable {
			plat = p
		}
	case len(reachable) == 0:
		return debugEnv{}, fmt.Errorf("%s: no environment to reach (%s); `dockhand verify %s` starts one", branch, summarizeNote(n), branch)
	default:
		var plats []string
		for p := range reachable {
			plats = append(plats, p)
		}
		return debugEnv{}, usagef("%s has environments on %s; pick one with --on", branch, strings.Join(plats, ", "))
	}
	run := reachable[plat]
	env := debugEnv{Job: run.Job, State: run.State, Port: n.Port, Plat: plat}
	prov, err := vmProvider(ctx)
	if err != nil {
		return debugEnv{}, err
	}
	if _, err := prov.Poll(ctx, run.Job); errors.Is(err, verify.ErrUnknownJob) {
		return debugEnv{}, fmt.Errorf("%s: environment %s no longer exists", branch, run.Job.ID)
	}
	return env, nil
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
	on     string
	trace  bool
}

var _ Action = logAction{}

func (a logAction) Execute(ctx context.Context, rs *runstate.Context) error {
	env, err := debugTarget(ctx, rs, a.target, a.on)
	if err != nil {
		return err
	}
	prov, err := vmProvider(ctx)
	if err != nil {
		return err
	}
	if a.trace {
		return traceLog(ctx, rs, prov, env)
	}
	log, err := prov.Log(ctx, env.Job)
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

// traceLog streams the environment's log as it grows, until the build
// reaches a terminal state. Read-only on purpose: verdict-keeping
// stays with status, which this hands off to.
func traceLog(ctx context.Context, rs *runstate.Context, prov verify.Verifier, env debugEnv) error {
	printed := 0
	for {
		st, err := prov.Poll(ctx, env.Job)
		if err != nil {
			return err
		}
		if log, lerr := prov.Log(ctx, env.Job); lerr == nil && len(log) > printed {
			fmt.Fprint(rs.Out, log[printed:])
			printed = len(log)
		}
		if st.State.Terminal() {
			fmt.Fprintf(rs.Err, "build finished: %s; `dockhand status` records it\n", st.State)
			return nil
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(rs.Err, "detached; the build continues")
			return nil
		case <-time.After(4 * time.Second):
		}
	}
}

// shellAction opens an interactive shell inside the target's
// verification environment, which is what a kept failure is for:
// the build's remains exactly as the guest left them.
type shellAction struct {
	target string
	on     string
}

var _ Action = shellAction{}

func (a shellAction) Execute(ctx context.Context, rs *runstate.Context) error {
	env, err := debugTarget(ctx, rs, a.target, a.on)
	if err != nil {
		return err
	}
	what := env.State
	if env.Port != "" {
		what += " verification of " + env.Port
	} else {
		what += " environment"
	}
	if env.Plat != "" {
		what += " on " + env.Plat
	}
	fmt.Fprintf(rs.Err, "connecting to %s (%s)\n", env.Job.ID, what)
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
	args = append(args, env.Job.ID, "/bin/zsh", "-l")
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
	var on string
	var trace bool
	c := &cobra.Command{
		Use:   "log <branch|port|worker>",
		Short: "Print the build log from a verification environment",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return logAction{target: args[0], on: on, trace: trace}, nil
		}),
	}
	c.Flags().StringVar(&on, "on", "", "which platform's environment, when several are reachable")
	c.Flags().BoolVar(&trace, "trace", false, "stream the log as it is written, until the build finishes")
	return c
}

// Shell builds the shell subcommand.
func Shell() *cobra.Command {
	var on string
	c := &cobra.Command{
		Use:   "shell <branch|port|worker>",
		Short: "Open a shell inside a verification environment",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return shellAction{target: args[0], on: on}, nil
		}),
	}
	c.Flags().StringVar(&on, "on", "", "which platform's environment, when several are reachable")
	return c
}
