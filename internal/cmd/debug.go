package cmd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verdict"
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
	branch, err := lifecycle.ResolveBranch(ctx, repo, target)
	if err != nil {
		return debugEnv{}, err
	}
	n, err := lifecycle.LatestNote(ctx, repo, branch)
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
			return debugEnv{}, fmt.Errorf("%s has no reachable environment on %s (%s)", branch, r.Name, verdict.Summarize(n))
		}
		plat = r.Name
	case len(reachable) == 1:
		for p := range reachable {
			plat = p
		}
	case len(reachable) == 0:
		return debugEnv{}, fmt.Errorf("%s: no environment to reach (%s); `dockhand verify %s` starts one", branch, verdict.Summarize(n), branch)
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
		return traceLog(ctx, rs, prov, env)
	}
	if a.errors_ {
		return errorLog(ctx, rs, prov, env)
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

// mainLogRE finds the guest-side main.log path MacPorts names in its
// own failure output.
var mainLogRE = regexp.MustCompile(`See (/\S+/main\.log)`)

// errorLog digs the actual failure out of the environment. The console
// log ends with "Error: See .../main.log for details" — the error
// itself lives in that file, inside the guest, and the field pattern
// was a human sshing in to grep it. This does the dig: the last lines
// of context before the first :error: line, then the :error: lines
// themselves.
//
// Guest-side extraction is provider-specific by nature — it execs into
// the environment — which is the same standing shell already has.
func errorLog(ctx context.Context, rs *runstate.Context, prov verify.Verifier, env debugEnv) error {
	console, err := prov.Log(ctx, env.Job)
	if err != nil {
		return err
	}
	m := mainLogRE.FindStringSubmatch(console)
	if m == nil {
		fmt.Fprintln(rs.Err, "the console log names no main.log; showing its tail instead")
		tail := console
		if len(tail) > 4000 {
			tail = tail[len(tail)-4000:]
		}
		fmt.Fprint(rs.Out, tail)
		return nil
	}
	ex, ok := prov.(verify.Executor)
	if !ok {
		fmt.Fprintln(rs.Err, "this provider's environments cannot be reached from here; showing the console tail instead")
		tail := console
		if len(tail) > 4000 {
			tail = tail[len(tail)-4000:]
		}
		fmt.Fprint(rs.Out, tail)
		return nil
	}
	fmt.Fprintf(rs.Err, "errors from %s in %s:\n", m[1], env.Job.ID)
	script := `log="$1"
first=$(grep -n -m1 ':error:' "$log" | cut -d: -f1)
if [ -z "$first" ]; then
  echo "no :error: lines in $log"
  exit 0
fi
start=$((first > 25 ? first - 25 : 1))
sed -n "${start},$((first - 1))p" "$log"
grep ':error:' "$log" | head -40`
	out, err := ex.Exec(ctx, env.Job, "/bin/sh", "-c", script, "sh", m[1])
	if err != nil {
		return fmt.Errorf("reading %s from %s: %w", m[1], env.Job.ID, err)
	}
	fmt.Fprint(rs.Out, out)
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
