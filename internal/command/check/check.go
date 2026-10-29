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
	var tests, variants, build, of string
	var plan, head, staged, workingTree, enqueue, baseline, replace, fresh, keep, yes bool
	var include, only, also, on []string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Build and test what you have, committed or not",
		Long: `Captures the branch's files as a numbered snapshot, the tracked files as
they are on disk unless --staged or --head says otherwise, and builds
every port the branch changes as MacPorts CI does: lint, fetch and
checksum, install, and declared tests (advisory unless --tests required).

Where it builds comes from a build profile. --build selects one;
otherwise build.default is used, which is host, this Mac's macOS, unless
set. Dockhand's own profiles are host, minmax (each port on the oldest
and newest releases Dockhand runs, macOS 12 at the earliest), macports-ci
(the releases MacPorts' CI builds on), and all; your own go under
[build.profiles]. --on selects explicit environments instead, such as
tart:15,26, github, local, or command. Every environment must pass.

--only narrows the check to some changed ports and adds back the changed
ports they need; --also builds unchanged ports against the branch. --plan
shows what would be built and changes nothing.

If every port an environment would build has inputs identical to an
earlier recorded build, that result is reused and nothing is built there;
--fresh builds everything.

Without dockhand serve, the check runs here and says so; Ctrl-C stops it
and keeps completed results. With serve running, the check is handed to
serve and followed here; Ctrl-C then only stops following. -d queues the
check and returns: serve runs it, or, with no serve running, Dockhand
starts a background worker for it, so -d runs a check in the background
either way. dockhand wait follows it.

A branch runs one check at a time. While one is queued or running for the
same files, check follows it to its result, as dockhand wait does; with
-d, it says so and leaves it running. While one is checking other files,
or stopped, check refuses; --replace stops it, keeping completed results,
and checks the files as they are now, where --on says (switching is
always explicit).

--baseline builds the ports that failed at install or test in the
branch's latest check, or the check --of names, or the --only ones, at
the master that check started from, in its environments, with its
variants and tests, and reports each beside the branch's result. It reports what happened in each run and nothing more. A
failed check suggests a baseline when one could tell more; with
check.baseline = true, it runs that baseline automatically.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// A misspelled policy is refused before anything is captured.
			if tests != "" && !model.TestPolicy(tests).Valid() {
				return fmt.Errorf("--tests %q is not declared, required, or skip", tests)
			}
			if err := baselineFlags(cmd, baseline, of); err != nil {
				return err
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
			request := engine.BaselineRequest{Branch: branch, Of: of, Ports: only}
			if baseline && plan {
				return previewBaseline(ctx, e, streams, request)
			}
			if baseline {
				return runBaseline(ctx, e, streams, request, enqueue, s)
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
			// The lint fixes tidy would commit are made first, so this
			// check builds what tidy commits.
			for _, fixed := range capture.Fixed {
				label := "Fixed"
				if plan {
					label = "Fixes"
				}
				fmt.Fprintf(out, "%s: %s\n", label, fixed)
			}
			// A file the check leaves out in a port it builds is said with
			// a mark, and how to take it in (Codex's gcc15 group update).
			for _, path := range engine.UntrackedInChanged(capture, proposed) {
				fmt.Fprintf(out, "! %s is untracked and excluded from this check. Use --include %s for this check only, or git add %s to track it.\n", path, path, path)
			}
			if len(capture.Untracked) > 0 {
				fmt.Fprintf(out, "Untracked files excluded: %s. Use --include to add a file to this check.\n", strings.Join(capture.Untracked, ", "))
				// A Portfile written by hand in a new port's directory is
				// one Git doesn't track yet, and the worktree's sparse
				// checkout keeps git add from taking it: the commands are
				// said whole (the Vx port's field testing, 2026-10-03).
				for _, directory := range capture.NewPorts {
					fmt.Fprintf(out, "  New port directory %s: run dockhand check --include %s/Portfile to build it. Run git sparse-checkout add %s before staging its files.\n", directory, directory, directory)
				}
			}
			view.WritePlan(out, proposed, e.PlanNotes(ctx, proposed), e.PlanSiblings(ctx, proposed), e.Remedy, view.ListShown(streams))
			// What it will install, by the port index: --plan waits for
			// packages.macports.org's listings; a check says what's kept,
			// and its run says the rest as it starts.
			view.WriteEstimates(out, e.InstallEstimates(ctx, proposed, capture.Revision.Source, plan), proposed.Environments)
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
			beside := engine.BesideFollow
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
			// A check already queued or running that covers this one, by the
			// engine's rule (PlanCovers), is followed to its result, as wait
			// follows it, where check refused for the person to type the
			// wait it printed (the docs dig's I10). -d leaves it running.
			if err == nil && queued.Followed {
				if enqueue {
					return fmt.Errorf("%s is already %s for these files; dockhand wait %s follows it", queued.Run.Name(), queued.Run.State, queued.Run.Name())
				}
				fmt.Fprintf(streams.Err, "%s is already %s and covers this check; following it.\n", queued.Run.Name(), queued.Run.State)
				return checkrun.Wait(ctx, e, queued.Run, streams, s.Places())
			}
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
				if status, nextErr := e.BranchStatusOf(ctx, branch.ID); nextErr != nil {
					fmt.Fprintf(streams.Err, "next: %v\n", nextErr)
				} else {
					view.WriteNext(streams.Out, status)
				}
			}
			// check.baseline runs the baseline the failed check pointed to,
			// by itself, when it pointed to one. The report has said why
			// if it couldn't tell.
			// The engine says whether one would answer anything, from the
			// finished run, where this read the check's exit code.
			// The engine queued it as the check's continuation, which a
			// drain runs for check -d; here it runs next, in the foreground.
			if s.File.Check.Baseline && !enqueue {
				if queued, plan, ok := e.QueuedBaseline(ctx, run.ID); ok {
					var names []string
					for _, target := range plan.Targets {
						names = append(names, string(target.ID))
					}
					fmt.Fprintf(streams.Out, "%s · baseline, since check.baseline is on: %s\n", branch.ShortName(), strings.Join(names, ", "))
					if baselineErr := checkrun.RunQueued(ctx, e, queued, streams, false, s.Places(), s.Drain); baselineErr != nil {
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
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue the check and return; serve, or a background drain where none runs, runs it")
	cmd.Flags().BoolVar(&replace, "replace", false, "stop the branch's queued or running check, keeping what it finished, and check this instead")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "build what failed in the latest check, or --only ports, at the master it started from")
	cmd.Flags().StringVar(&of, "of", "", "with --baseline, the branch's finished check to compare with, as check-42, not its latest")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "build every port, reusing no earlier build's result")
	cmd.Flags().BoolVar(&keep, "keep", false, "keep what the check built: each Tart clone, stopped, where its guest finishes, for dockhand shell, and in a local prefix the branch's versions, left active")
	cmd.Flags().StringVar(&variants, "variants", "", "build the one port checked with these variants, +name -name, or each: its defaults, then each variant it declares")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "with --variants each, make its builds without asking")
	cmd.MarkFlagsMutuallyExclusive("plan", "yes")
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

// baselineFlags refuses the flags --baseline doesn't take, where it
// returned before reading them, and --of without it: a baseline is built
// at master as the check it compares with was, in its environments, with
// its variants and tests, from the files that check captured.
func baselineFlags(cmd *cobra.Command, baseline bool, of string) error {
	if !baseline {
		if of != "" {
			return fmt.Errorf("--of names the check a baseline compares with; add --baseline")
		}
		return nil
	}
	var given []string
	for _, name := range []string{"on", "build", "variants", "tests", "fresh", "head", "staged", "working-tree", "include", "replace", "yes"} {
		if cmd.Flags().Changed(name) {
			given = append(given, "--"+name)
		}
	}
	if len(given) > 0 {
		return fmt.Errorf("--baseline builds at master as the check it compares with built, in its environments, with its variants and tests, so it takes no %s", prose.And(given))
	}
	return nil
}

// previewBaseline shows the baseline check --baseline would run, and
// runs nothing.
func previewBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, request engine.BaselineRequest) error {
	branch := request.Branch
	baseline, err := e.PreviewBaseline(ctx, request)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline)
	view.WritePlan(streams.Out, baseline.Plan, e.PlanNotes(ctx, baseline.Plan), nil, e.Remedy, view.ListShown(streams))
	planned := view.PlanView(baseline.Plan)
	streams.Emit(view.CheckJSON{Branch: branch.ShortName(), Plan: &planned, Targets: []view.TargetJSON{}})
	return nil
}

// runBaseline plans a baseline of the branch's check, its latest or the
// one --of names, and runs it, here or through serve.
func runBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, request engine.BaselineRequest, enqueue bool, s *settings.Settings) error {
	branch := request.Branch
	baseline, err := e.PlanBaseline(ctx, request)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Out, branch, baseline)
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
func writeBaselineHeader(out io.Writer, branch model.Branch, baseline engine.Baseline) {
	var names []string
	for _, target := range baseline.Plan.Targets {
		names = append(names, string(target.ID))
	}
	fmt.Fprintf(out, "%s · baseline at master %s: %s\n", branch.ShortName(), baseline.Revision.Source.Commit.Short(), strings.Join(names, ", "))
	if len(baseline.New) > 0 {
		fmt.Fprintf(out, "    Excluded from the baseline: %s. These ports are new on the branch and do not exist on master.\n", strings.Join(baseline.New, ", "))
	}
	if len(baseline.Unchanged) > 0 {
		fmt.Fprintf(out, "    %s. Use --only to include them.\n", engine.UnchangedWords(baseline.Unchanged, baseline.Of))
	}
	if len(baseline.Skipped) > 0 {
		fmt.Fprintf(out, "    skipped in this baseline: %s failed at lint, fetch, or checksum. Use --only to include them.\n", strings.Join(baseline.Skipped, ", "))
	}
}

// besideWords says how to go on from a check refused beside the branch's
// one queued or running (engine.BesideRefuse), or of its files without
// covering this one (engine.BesideFollow): wait resumes one stopped, and
// --replace checks again, keeping what it finished.
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
	case active.Uncovered:
		return fmt.Errorf("%s is %s for these files, but doesn't cover what this check asks, the builds it makes in each environment, its variants, tests, or freshness, or a kept guest; --replace stops it, keeping what it finished, and checks this one instead", active.Run.Name(), active.Run.State)
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
