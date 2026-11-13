package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	var tests, variants, build, of string
	var plan, head, staged, workingTree, enqueue, baseline, replace, fresh, keep, yes bool
	var bound string
	var include, only, also, on []string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Build and test what you have, committed or not",
		Long: `Captures the branch's files as a numbered snapshot, the tracked files as
they are on disk unless --staged or --head says otherwise, and builds
every port the branch changes as MacPorts CI does: lint, fetch and
checksum, install, and declared tests (advisory unless --tests required).

Where it builds comes from a build profile. --build selects one;
otherwise build.default is used. The default is minmax, or command when
that provider is configured. Dockhand's own Tart profiles are host (this
Mac's macOS), minmax (each port's oldest and newest eligible releases with
available images, macOS 12 or later), macports-ci (the releases MacPorts'
CI builds on), and all. Define your own under [build.profiles]. --on
selects explicit environments instead, such as tart:15,26, github, local,
or command. Every environment must pass.

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
check.baseline = true, it runs that baseline automatically.

--for 9m queues the check, as -d does, then follows it for at most nine
minutes, exiting 4 if it is still queued or running, saying what it is
doing; the check continues, with serve or a background worker, never in
this process. dockhand wait check-42 --for 9m follows it further.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// A misspelled policy is refused before anything is captured.
			if tests != "" && !model.TestPolicy(tests).Valid() {
				return fmt.Errorf("--tests %q is not declared, required, or skip", tests)
			}
			var limit checkrun.Bound
			if bound != "" {
				switch {
				case enqueue:
					return errors.New("-d returns at once and --for follows for a while; use one")
				case plan:
					return errors.New("--plan builds nothing and waits for nothing")
				}
				var err error
				if limit, err = checkrun.ParseBound(bound); err != nil {
					return err
				}
			}
			if err := baselineFlags(cmd, baseline, of, nil); err != nil {
				if !cmd.Flags().Changed("variants") {
					return err
				}
				// The variants the baseline takes are the check's, named
				// where it can be found (field testing's curl +sectrust
				// notes, note 2).
				return baselineFlags(cmd, baseline, of, func() (string, string) { return comparedWith(ctx, s, streams, where, of) })
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
			if err := branchflags.MayEvaluate(ctx, e, streams, branch); err != nil {
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
				return runBaseline(ctx, e, streams, request, enqueue, limit, s)
			}
			mode := captureMode(head, staged, workingTree)
			place, err := e.Where(ctx, engine.WhereRequest{On: on, Build: build})
			if err != nil {
				return err
			}
			named, chosen, each, err := variantsFlag(variants)
			if err != nil {
				return err
			}
			proposal, err := e.ProposeCheck(ctx, engine.CheckRequest{Capture: engine.CaptureRequest{Branch: branch, Mode: mode, Named: where.Explicit(), Include: include, Plan: plan}, Where: place,
				Tests: model.TestPolicy(tests), Only: only, Also: also, Fresh: fresh, Keep: keep, Variants: chosen, VariantsPort: named, EachVariant: each})
			if err != nil {
				return err
			}
			capture, proposed := proposal.Capture, proposal.Plan
			out := streams.Prose()
			what := "checking " + checks.Describe(capture.Revision)
			if capture.Revision.Kind == model.RevisionSnapshot && !capture.Reused {
				what = "captured working files as " + checks.Describe(capture.Revision)
				if plan {
					what = "would capture the working files as " + checks.Describe(capture.Revision)
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
			// A tracked file on disk outside the worktree's sparse checkout
			// is built, as git status reports it, but git add declines it:
			// said, with the command that lets it be staged (#228).
			for _, file := range capture.OutsideSparse {
				directory, ok := macports.PortDirectoryOf(file)
				if !ok {
					directory = path.Dir(file)
				}
				fmt.Fprintf(out, "! %s is outside this worktree's sparse checkout: this check builds it as it is on disk, but git add declines it. Run git sparse-checkout add %s before staging it.\n", file, directory)
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
			view.WritePlan(out, proposed, e.PlanNotes(ctx, proposed), e.PlanSiblings(ctx, capture.Revision, proposed), e.Remedy, view.ListShown(streams))
			// The plan's evaluation, with -v: check --plan -v is the dry
			// evaluation under a variant set (field testing's curl
			// +sectrust notes, note 8b).
			if plan && streams.Verbose() > 0 {
				view.WriteEvaluated(out, proposed)
			}
			// The Portfile findings submit would say, before the build
			// they'd otherwise repeat (the openssl4 session's note 1).
			findings, _ := e.BeforeBuilding(ctx, branch, capture, plan)
			view.WriteBeforeBuilding(out, findings)
			// What it will install, by the port index: --plan waits for
			// packages.macports.org's listings; a check says what's kept,
			// and its run says the rest as it starts.
			view.WriteEstimates(out, e.InstallEstimates(ctx, proposed, capture.Revision.Source, plan), proposed.Environments)
			view.WritePushes(out, proposed)
			revision, planned := view.RevisionView(capture.Revision), view.PlanView(proposed)
			streams.Emit(view.CheckJSON{Branch: branch.ShortName(), Revision: &revision, Plan: &planned, Targets: []view.TargetJSON{}}.WithPortfileFindings(findings))
			if err := checks.Runnable(proposed); err != nil {
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
			beside := checks.BesideFollow
			if replace {
				beside = checks.BesideReplace
			}
			queued, err := e.QueueCheck(ctx, checks.QueueRequest{Branch: branch, Revision: capture.Revision, Plan: proposed, Origin: model.OriginPerson, Beside: beside,
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
				return checkrun.See(ctx, e, queued, streams, s.Places(), checkrun.Stay{For: limit}, s.Drain)
			}
			if err != nil {
				for _, words := range replaced {
					fmt.Fprintf(streams.Prose(), "%s.\n", prose.Sentence(words))
				}
				return besideWords(err, capture.Revision)
			}
			run := queued.Run
			if len(replaced) > 0 {
				// One line names each, where a line said each stopped and
				// another what replaced them (part 5, item 3).
				fmt.Fprintf(streams.Prose(), "Replaced %s.\n", strings.Join(replaced, "; "))
			}
			if bound != "" {
				// Queued as -d queues it, then followed for the bound,
				// driving nothing: returning at it stops nothing.
				err = checkrun.See(ctx, e, queued, streams, s.Places(), checkrun.Stay{For: limit}, s.Drain)
				if err == nil {
					if status, nextErr := e.BranchStatusOf(ctx, branch.ID); nextErr == nil {
						view.WriteNext(streams.Prose(), status)
					}
				}
				if s.File.Check.Baseline {
					if queued, _, ok := e.QueuedBaseline(ctx, run.ID); ok {
						fmt.Fprintf(streams.Prose(), "%s · baseline queued, since check.baseline is on: dockhand wait %s --for %s follows it\n", run.Name(), queued.Name(), limit.Said)
					}
				}
				return err
			}
			err = checkrun.See(ctx, e, queued, streams, s.Places(), checkrun.Stay{Detach: enqueue}, s.Drain)
			// A check that passed says what moves the branch on now, as
			// status would; one run for submit leaves that to submit.
			if err == nil && !enqueue {
				if status, nextErr := e.BranchStatusOf(ctx, branch.ID); nextErr != nil {
					fmt.Fprintf(streams.Err, "next: %v\n", nextErr)
				} else {
					view.WriteNext(streams.Prose(), status)
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
					fmt.Fprintf(streams.Prose(), "%s · baseline, since check.baseline is on: %s\n", branch.ShortName(), strings.Join(names, ", "))
					if baselineErr := checkrun.See(ctx, e, checks.Queued{Run: queued}, streams, s.Places(), checkrun.Stay{}, s.Drain); baselineErr != nil {
						fmt.Fprintf(streams.Err, "baseline: %v\n", baselineErr)
					}
				}
			}
			return err
		},
	}
	where.Register(cmd, s, "check")
	cmd.Flags().BoolVar(&plan, "plan", false, "show what would be built and change nothing")
	cmd.Flags().StringVar(&bound, "for", "", "follow for at most this long, then exit 4 if the check is still queued or running; it continues")
	cmd.Flags().BoolVar(&head, "head", false, "check the committed tip")
	cmd.Flags().BoolVar(&staged, "staged", false, "check the index")
	cmd.Flags().BoolVar(&workingTree, "working-tree", false, "check the working files, for a --branch checked out elsewhere")
	cmd.Flags().StringSliceVar(&include, "include", nil, "add an untracked file to the capture without staging it")
	cmd.Flags().StringSliceVar(&only, "only", nil, "check only these changed ports, and the changed ports they need")
	cmd.Flags().StringSliceVar(&also, "also", nil, "also build these unchanged ports against the branch")
	cmd.Flags().StringVar(&build, "build", "", "where to build, by build profile: host (this Mac's macOS alone), minmax, macports-ci, all, or yours (default: build.default)")
	cmd.Flags().StringArrayVar(&on, "on", nil, "where to build, explicit environments such as tart:15,26, github, local, or command; repeat for several, all of which must pass")
	cmd.Flags().StringVar(&tests, "tests", "", "declared (advisory), required, or skip")
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "run the check in the background using serve or a background worker, and return immediately")
	cmd.Flags().BoolVar(&replace, "replace", false, "stop the branch's queued or running check, keeping what it finished, and check this instead")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "build what failed in the latest check, or --only ports, at the master it started from")
	cmd.Flags().StringVar(&of, "of", "", "with --baseline, the branch's finished check to compare with, as check-42, not its latest")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "build every port, reusing no earlier build's result")
	cmd.Flags().BoolVar(&keep, "keep", false, "retain stopped Tart VMs for dockhand shell, or leave the branch's versions active in a local prefix")
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
// its variants and tests, from the files that check captured. compared,
// where given, names that check and its variants, as "check-770" and
// "+gnutls +sectrust"; empty where it can't.
func baselineFlags(cmd *cobra.Command, baseline bool, of string, compared func() (name, variants string)) error {
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
	if len(given) == 0 {
		return nil
	}
	check, variants := "the check it compares with", "its variants"
	if compared != nil {
		name, spec := compared()
		if name != "" {
			check = name
		}
		if spec != "" {
			variants = "its variants (" + spec + ")"
		}
	}
	return fmt.Errorf("--baseline builds at master as %s built, in its environments, with %s and tests, so it takes no %s", check, variants, prose.And(given))
}

// comparedWith is the check a baseline would compare with, --of's or the
// branch's latest, and the variants its plan built with; empty where
// either can't be found.
func comparedWith(ctx context.Context, s *settings.Settings, streams term.Streams, where branchflags.BranchFlags, of string) (string, string) {
	e, err := s.Open(ctx)
	if err != nil {
		return "", ""
	}
	defer e.Close()
	var run model.Run
	if of != "" {
		if run, err = e.RunNamed(ctx, of); err != nil {
			return "", ""
		}
	} else {
		branch, err := where.Resolve(ctx, e, streams)
		if err != nil {
			return "", ""
		}
		latest, ok, err := e.LatestCheck(ctx, branch.ID)
		if err != nil || !ok {
			return "", ""
		}
		run = latest
	}
	plan, err := e.Plan(ctx, run.Plan)
	if err != nil {
		return run.Name(), ""
	}
	return run.Name(), plan.Variants
}

// previewBaseline shows the baseline check --baseline would run, and
// runs nothing.
func previewBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, request engine.BaselineRequest) error {
	branch := request.Branch
	baseline, err := e.PreviewBaseline(ctx, request)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Prose(), branch, baseline)
	view.WritePlan(streams.Prose(), baseline.Plan, e.PlanNotes(ctx, baseline.Plan), engine.Siblings{}, e.Remedy, view.ListShown(streams))
	planned := view.PlanView(baseline.Plan)
	streams.Emit(view.CheckJSON{Branch: branch.ShortName(), Plan: &planned, Targets: []view.TargetJSON{}})
	return nil
}

// runBaseline plans a baseline of the branch's check, its latest or the
// one --of names, and runs it, here or through serve.
// A --for bound follows it as it follows a check, queued as -d queues it:
// it was run in the foreground past the bound (Codex's workflow review of
// 1caa6c3d, finding 4).
func runBaseline(ctx context.Context, e *engine.Engine, streams term.Streams, request engine.BaselineRequest, enqueue bool, limit checkrun.Bound, s *settings.Settings) error {
	branch := request.Branch
	baseline, err := e.PlanBaseline(ctx, request)
	if err != nil {
		return err
	}
	writeBaselineHeader(streams.Prose(), branch, baseline)
	run, err := e.EnqueueBaseline(ctx, branch, baseline, model.OriginPerson)
	if err != nil {
		return err
	}
	return checkrun.See(ctx, e, checks.Queued{Run: run}, streams, s.Places(), checkrun.Stay{Detach: enqueue, For: limit}, s.Drain)
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
// one queued or running (checks.BesideRefuse), or of its files without
// covering this one (checks.BesideFollow): wait resumes one stopped, and
// --replace checks again, keeping what it finished.
func besideWords(err error, revision model.Revision) error {
	active := new(checks.ActiveRunError)
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
		return fmt.Errorf("%s is %s for these files, %s; --replace stops it, keeping what it finished, and checks this one instead", active.Run.Name(), active.Run.State, active.Uncovering())
	case active.SameRevision:
		return active
	}
	return fmt.Errorf("%s is %s for %s; --replace stops it, keeping what it finished, and checks %s instead", active.Run.Name(), active.Run.State, checks.Describe(active.Checking), checks.Describe(revision))
}

// replacedWords say each run a new check stopped, and how: "check-1,
// canceled before it started".
func replacedWords(queued checks.Queued) []string {
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
