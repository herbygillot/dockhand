package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
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
		ok, err := tart.HasVM(ctx, rs.Tools, target)
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
	eng := rs.Deps()
	branch, err := eng.Resolve(ctx, repo, target)
	if err != nil {
		return debugEnv{}, err
	}
	n, err := eng.LatestNote(ctx, repo, branch)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return debugEnv{}, fmt.Errorf("%s has no verification record; `dockhand verify %s` starts one", branch, branch)
		}
		return debugEnv{}, err
	}

	reachable := map[string]record.Run{}
	for plat, r := range n.Runs {
		if r.State == record.Running || r.Handle != "" {
			reachable[plat] = r
		}
	}
	var plat string
	switch {
	case on != "":
		r, err := parseRelease(on)
		if err != nil {
			return debugEnv{}, err
		}
		if _, ok := reachable[r.Name]; !ok {
			return debugEnv{}, fmt.Errorf("%s has no reachable environment on %s (%s)", branch, r.Name, render.SummarizeRecord(n))
		}
		plat = r.Name
	case len(reachable) == 1:
		for p := range reachable {
			plat = p
		}
	case len(reachable) == 0:
		return debugEnv{}, fmt.Errorf("%s: no environment to reach (%s); `dockhand verify %s` starts one", branch, render.SummarizeRecord(n), branch)
	default:
		var plats []string
		for p := range reachable {
			plats = append(plats, p)
		}
		return debugEnv{}, usagef("%s has environments on %s; pick one with --on", branch, strings.Join(plats, ", "))
	}
	run := reachable[plat]
	// The state is prose from here on — "kept" is one of the words this
	// field holds, and no note ever carried that one.
	env := debugEnv{Job: run.Job, State: string(run.State), Port: n.Port, Plat: plat}
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return debugEnv{}, err
	}
	if _, err := prov.Poll(ctx, run.Job); errors.Is(err, verify.ErrUnknownJob) {
		return debugEnv{}, fmt.Errorf("%s: environment %s no longer exists", branch, run.Job.ID)
	}
	return env, nil
}

// logAction prints the build log out of the target's verification
// environment, as it stands right now — mid-build for a running job,
// complete for a kept failure.
type logAction struct {
	target  string
	on      string
	trace   bool
	errors_ bool
}

var _ Action = logAction{}

func (a logAction) Execute(ctx context.Context, rs *runstate.Context) error {
	env, err := debugTarget(ctx, rs, a.target, a.on)
	if err != nil {
		return err
	}
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return err
	}
	if a.trace {
		return rs.Deps().Trace(ctx, prov, env.Job)
	}
	if a.errors_ {
		return rs.Deps().ErrorLog(ctx, prov, env.Job)
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
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return err
	}
	sh, ok := prov.(verify.InteractiveShell)
	if !ok {
		return fmt.Errorf("this provider's environments do not take an interactive shell; `dockhand log` still reads their output")
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
	return sh.Shell(ctx, env.Job)
}

// Log builds the log subcommand.
func Log() *cobra.Command {
	var on string
	var trace, errs bool
	c := &cobra.Command{
		Use:   "log <branch|port|worker>",
		Short: "Print the build log from a verification environment",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return logAction{target: args[0], on: on, trace: trace, errors_: errs}, nil
		}),
	}
	c.Flags().StringVar(&on, "on", "", "which platform's environment, when several are reachable")
	c.Flags().BoolVar(&trace, "trace", false, "stream the log as it is written, until the build finishes")
	c.Flags().BoolVar(&errs, "errors", false, "dig the :error: lines and their context out of the guest's main.log")
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
