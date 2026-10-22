package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	var tests, variants, build string
	var plan, head, staged, workingTree, enqueue, baseline, replace, fresh, keep, yes bool
	var include, only, also, on []string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Build and test what you have, committed or not",
		Long: `Captures the branch's files, the tracked ones as they are on disk unless
--staged or --head says otherwise, as a numbered snapshot, and builds every
port the branch changes by MacPorts CI's rule: lint, fetch and checksum,
install, and declared tests (advisory unless --tests required).

It builds where a build profile says: --build names one, and build.default
is the profile otherwise, host, this Mac's macOS, unless set. dockhand's
own are host, minmax (each port on its oldest and newest release dockhand
runs, macOS 12 at the earliest), macports-ci (the releases MacPorts' CI
builds on), and all; yours go under [build.profiles]. --on names explicit
environments instead, tart:15,26, github, or command. Every environment
must pass. --only narrows the
check to some changed ports and adds back the changed ones they need;
--also builds unchanged ports against the branch. --plan shows what would
be built and changes nothing.

Where every port an environment would build reads what an earlier build of
it read, recorded with its result, that result is reused and nothing is
built there; --fresh builds everything.

Without dockhand serve, the check runs here and says so; Ctrl-C stops it
and keeps what finished. With serve running, it is handed to serve and
followed here; Ctrl-C then only stops following. -d queues it and returns.

One check of a branch runs at a time: while one is queued or running,
check refuses, and --replace stops it, keeping what it finished, and checks
the files now, on what --on names (decision 29: switching is explicit).

--baseline builds the ports that failed at install or test in the branch's
latest check, or the --only ones, at the master that check started from,
planned there as a check would be, and reports each beside the branch's
result. It says what happened in each run and nothing more. A failed check
points to it when a baseline can answer something; with check.baseline =
true, it runs that baseline by itself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// A misspelled policy is refused before anything is captured.
			if tests != "" && !model.TestPolicy(tests).Valid() {
				return fmt.Errorf("--tests %q is not declared, required, or skip", tests)
			}
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := where.Resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			// The result is the check's, with a baseline check.baseline runs
			// inside it.
			streams.LinkSteps(&view.CheckJSON{})
			if baseline && plan {
				return previewBaseline(ctx, e, streams, branch, only)
			}
			if baseline {
				return runBaseline(ctx, e, streams, branch, only, enqueue, false, s)
			}
			mode := captureMode(head, staged, workingTree)
			place, err := e.Where(ctx, engine.WhereRequest{On: on, Build: build})
			if err != nil {
				return err
			}
			chosen, each, err := variantsFlag(variants)
			if err != nil {
				return err
			}
			proposal, err := e.ProposeCheck(ctx, engine.CheckRequest{Capture: engine.CaptureRequest{Branch: branch, Mode: mode, Named: where.Explicit(), Include: include, Plan: plan}, Where: place,
				Tests: model.TestPolicy(tests), Only: only, Also: also, Fresh: fresh, Keep: keep, Variants: chosen, EachVariant: each})
			if err != nil {
				return err
			}
			capture, proposed := proposal.Capture, proposal.Plan
			out := streams.Out
			what := "checking " + engine.Describe(capture.Revision)
			if capture.Revision.Kind == model.RevisionSnapshot && !capture.Reused {
				what = "captured working files as " + engine.Describe(capture.Revision)
				if plan {
					what = "would capture the working files as " + engine.Describe(capture.Revision)
				}
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), what)
			// A file the check leaves out in a port it builds is said with
			// a mark, and how to take it in (Codex's gcc15 group update).
			for _, path := range engine.UntrackedInChanged(capture, proposed) {
				fmt.Fprintf(out, "! %s is untracked and excluded from this check. Use --include %s for this check only, or git add %s to track it.\n", path, path, path)
			}
			if len(capture.Untracked) > 0 {
				fmt.Fprintf(out, "Left out, not tracked: %s (--include adds one)\n", strings.Join(capture.Untracked, ", "))
				// A Portfile written by hand in a new port's directory is
				// one Git doesn't track yet, and the worktree's sparse
				// checkout keeps git add from taking it: the commands are
				// said whole (the Vx port's field testing, 2026-10-03).
				for _, directory := range capture.NewPorts {
					fmt.Fprintf(out, "  New port directory %s: run dockhand check --include %s/Portfile to build it. Run git sparse-checkout add %s before staging its files.\n", directory, directory, directory)
				}
			}
			view.WritePlan(out, proposed, e.PlanNotes(ctx, proposed), e.PlanSiblings(ctx, proposed), e.Remedy)
			view.WritePushes(out, proposed)
			revision, planned := view.RevisionView(capture.Revision), view.PlanView(proposed)
			streams.Emit(view.CheckJSON{Branch: branch.ShortName(), Revision: &revision, Plan: &planned, Targets: []view.TargetJSON{}})
			if err := engine.Runnable(proposed); err != nil {
				return fmt.Errorf("nothing was checked: %w", err)
			}
			if plan {
				// Reuse is chosen when the check runs, from what each earlier
				// build read, so a plan can't say which ports reuse; that
				// some may, it says, since a plan after a rebase read as a
				// full build where its check reused one (the rc6 full stage,
				// B5).
				if passed, err := e.Runs(ctx, store.RunFilter{Branch: branch.ID, States: []model.RunState{model.RunPassed}, Limit: 1}); err == nil && len(passed) > 0 && !proposed.Fresh {
					fmt.Fprintln(out, "Reuse       when this check runs, it may reuse passing builds with identical inputs; --fresh rebuilds every port")
				}
				return nil
			}
			if err := confirmVariantBuilds(streams, proposed, yes); err != nil {
				return err
			}
			beside := engine.BesideRefuse
			if replace {
				beside = engine.BesideReplace
			}
			queued, err := e.QueueCheck(ctx, engine.QueueRequest{Branch: branch, Revision: capture.Revision, Plan: proposed, Origin: model.OriginPerson, Beside: beside,
				// A script that asked for a new check replaces a running one;
				// a person is asked.
				Stop: func(run model.Run) (bool, error) {
					ok, _, err := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? stop %s, which is running? [y/N] ", run.Name()), Nobody: term.Answer(true)})
					return ok, err
				}})
			replaced := replacedWords(queued)
			if err != nil {
				for _, words := range replaced {
					fmt.Fprintf(streams.Out, "%s.\n", prose.Sentence(words))
				}
				return besideWords(err, capture.Revision)
			}
			run := queued.Run
			if len(replaced) > 0 {
				// One line names each, where a line said each stopped and
				// another what replaced them (part 5, item 3).
				fmt.Fprintf(streams.Out, "Replaced %s.\n", strings.Join(replaced, "; "))
			}
			err = checkrun.RunQueued(ctx, e, run, streams, enqueue, s.Places(), s.Drain)
			// A check that passed says what moves the branch on now, as
			// status would; one run for submit leaves that to submit.
			if err == nil && !enqueue {
				if nextErr := view.WriteNextFor(ctx, e, streams.Out, branch.ID); nextErr != nil {
					fmt.Fprintf(streams.Err, "next: %v\n", nextErr)
				}
			}
			// check.baseline runs the baseline the failed check pointed to,
			// by itself, when it pointed to one. The report has said why
			// if it couldn't tell.
			// The engine says whether one would answer anything, from the
			// finished run, where this read the check's exit code.
			if s.File.Check.Baseline && !enqueue {
				finished, _ := e.RunNamed(ctx, run.Name())
				if offer, _ := e.BaselineCandidates(ctx, finished); len(offer.Ports) > 0 {
					if baselineErr := runBaseline(ctx, e, streams, branch, nil, false, true, s); baselineErr != nil {
						fmt.Fprintf(streams.Err, "baseline: %v\n", baselineErr)
					}
				}
			}
			return err
		},
	}
	where.Register(cmd, s, "check")
	cmd.Flags().BoolVar(&plan, "plan", false, "show what would be built and change nothing")
	cmd.Flags().BoolVar(&head, "head", false, "check the committed tip")
	cmd.Flags().BoolVar(&staged, "staged", false, "check the index")
	cmd.Flags().BoolVar(&workingTree, "working-tree", false, "check the working files, for a --branch checked out elsewhere")
	cmd.Flags().StringSliceVar(&include, "include", nil, "add an untracked file to the capture without staging it")
	cmd.Flags().StringSliceVar(&only, "only", nil, "check only these changed ports, and the changed ports they need")
	cmd.Flags().StringSliceVar(&also, "also", nil, "also build these unchanged ports against the branch")
	cmd.Flags().StringVar(&build, "build", "", "where to build, by build profile: host, minmax, macports-ci, all, or yours (default build.default)")
	cmd.Flags().StringArrayVar(&on, "on", nil, "where to build, explicit environments, as tart:15,26, github, or command; repeat for several, all of which must pass")
	cmd.Flags().StringVar(&tests, "tests", "", "declared (advisory), required, or skip")
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue the check and return")
	cmd.Flags().BoolVar(&replace, "replace", false, "stop the branch's queued or running check, keeping what it finished, and check this instead")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "build what failed in the latest check, or --only ports, at the master it started from")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "build every port, reusing no earlier build's result")
	cmd.Flags().BoolVar(&keep, "keep", false, "keep each Tart environment's clone, stopped, where its guest finishes, for dockhand shell")
	cmd.Flags().StringVar(&variants, "variants", "", "build the one port checked with these variants, +name -name, or each: its defaults, then each variant it declares")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "with --variants each, make its builds without asking")
	cmd.MarkFlagsMutuallyExclusive("baseline", "also")
	cmd.MarkFlagsMutuallyExclusive("baseline", "keep")
	cmd.MarkFlagsMutuallyExclusive("build", "on")
	cmd.MarkFlagsMutuallyExclusive("head", "staged", "working-tree")
	settings.SupportsJSON(cmd)
	return cmd
}

// captureMode is the files check's flags ask for, or CaptureAuto where
// none does.
func captureMode(head, staged, workingTree bool) engine.CaptureMode {
	switch {
	case head:
		return engine.CaptureHead
	case staged:
		return engine.CaptureStaged
	case workingTree:
		return engine.CaptureWorking
	}
	return engine.CaptureAuto
}

// previewBaseline shows the baseline check --baseline would run, and
// runs nothing.
func previewBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, branch model.Branch, only []string) error {
	baseline, err := e.PreviewBaseline(ctx, branch, only)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline, false)
	view.WritePlan(streams.Out, baseline.Plan, e.PlanNotes(ctx, baseline.Plan), nil, e.Remedy)
	planned := view.PlanView(baseline.Plan)
	streams.Emit(view.CheckJSON{Branch: branch.ShortName(), Plan: &planned, Targets: []view.TargetJSON{}})
	return nil
}

// runBaseline plans a baseline of the branch's latest check and runs it,
// here or through serve.
// runBaseline runs a baseline of a branch's failed check; auto is
// check.baseline's, which the failed check's report pointed to.
func runBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, branch model.Branch, only []string, enqueue, auto bool, s *settings.Settings) error {
	baseline, err := e.PlanBaseline(ctx, branch, only)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline, auto)
	run, err := e.EnqueueBaseline(ctx, branch, baseline, model.OriginPerson)
	if err != nil {
		return err
	}
	return checkrun.RunQueued(ctx, e, run, streams, enqueue, s.Places(), s.Drain)
}

// writeBaselineHeader says what a baseline builds, and what it leaves out.
// Its results name the check they're beside, and check.baseline's follows
// the failed check's pointer to it, which named master's commit (part 5,
// item 3: each was said twice).
func writeBaselineHeader(out io.Writer, branch model.Branch, baseline engine.Baseline, auto bool) {
	var names []string
	for _, target := range baseline.Plan.Targets {
		names = append(names, string(target.ID))
	}
	if auto {
		fmt.Fprintf(out, "%s · baseline, since check.baseline is on: %s\n", branch.ShortName(), strings.Join(names, ", "))
	} else {
		fmt.Fprintf(out, "%s · baseline at master %s: %s\n", branch.ShortName(), baseline.Revision.Source.Commit.Short(), strings.Join(names, ", "))
	}
	if len(baseline.New) > 0 {
		fmt.Fprintf(out, "    left out: %s, which the branch adds, so master has nothing to compare\n", strings.Join(baseline.New, ", "))
	}
	if len(baseline.Unchanged) > 0 {
		fmt.Fprintf(out, "    %s. Use --only to include them.\n", engine.UnchangedWords(baseline.Unchanged, baseline.Of))
	}
	if len(baseline.Skipped) > 0 {
		fmt.Fprintf(out, "    skipped in this baseline: %s failed at lint, fetch, or checksum. Use --only to include them.\n", strings.Join(baseline.Skipped, ", "))
	}
}

// besideWords says how to go on from a check refused beside the branch's
// one queued or running (engine.BesideRefuse): wait resumes one stopped,
// and --replace checks again, keeping what it finished.
func besideWords(err error, revision model.Revision) error {
	active := new(engine.ActiveRunError)
	if !errors.As(err, &active) {
		return err
	}
	switch {
	case active.SameRevision && active.Stopped:
		// Stopped, not running: wait resumes it, where "already running"
		// sent a person to follow a run nothing was running (field
		// testing's rust check, 2026-10-15).
		return fmt.Errorf("%v, or --replace checks again, keeping what it finished", active)
	case active.SameRevision:
		return active
	}
	return fmt.Errorf("%s is %s for %s; --replace stops it, keeping what it finished, and checks %s instead", active.Run.Name(), active.Run.State, engine.Describe(active.Checking), engine.Describe(revision))
}

// replacedWords say each run a new check stopped, and how: "check-1,
// canceled before it started".
func replacedWords(queued engine.Queued) []string {
	var stopped []string
	for i, run := range queued.Replaced {
		switch {
		case run.State != model.RunCanceled:
			stopped = append(stopped, run.Name()+", which the process running it stops at its next step")
		case queued.Was[i].State == model.RunQueued:
			stopped = append(stopped, run.Name()+", canceled before it started")
		default:
			stopped = append(stopped, run.Name()+", stopped, keeping what it finished")
		}
	}
	return stopped
}

// variantBuildsToAsk is how many builds --variants each makes, targets
// times environments, before check asks first: each is a whole build.
const variantBuildsToAsk = 12

// confirmVariantBuilds asks before a --variants each check makes more
// than variantBuildsToAsk builds; without a terminal, --yes goes ahead.
func confirmVariantBuilds(streams term.Streams, plan model.Plan, yes bool) error {
	builds := plan.BuildCount()
	if !plan.EachVariant || builds <= variantBuildsToAsk || yes {
		return nil
	}
	ok, _, err := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? --variants each will run %d full builds. Continue? [y/N] ", builds),
		Undone: fmt.Sprintf("nothing was checked (--variants each makes %d builds here)", builds), ByYes: "--yes builds them"})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("nothing was checked")
	}
	return nil
}
