package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/verify"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	errJobFailed      = errors.New("dockhand: requested work failed")
	errNeedsAttention = errors.New("dockhand: requested work needs attention")
	errJobCanceled    = errors.New("dockhand: requested work was canceled")
)

func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled), errors.Is(err, errJobCanceled):
		return 130
	case errors.Is(err, errNeedsAttention), errors.Is(err, workflow.ErrContinuation):
		return 3
	case errors.Is(err, errJobFailed):
		return 2
	default:
		return 1
	}
}

type ActionResult struct {
	JobID       record.JobID   `json:",omitempty"`
	JobIDs      []record.JobID `json:",omitempty"`
	Branch      string         `json:",omitempty"`
	Receipt     *workflow.Receipt
	Status      workflow.Status
	Interrupted bool
}

func (r *runtime) verifyCommand() *cobra.Command {
	var branch, adopt, change string
	var workingTree bool
	var build buildOptions
	var variants []string
	var detach, trace, fresh, allSubports bool
	var releases []string
	command := &cobra.Command{
		Use: "verify [port]", Short: "Verify a prepared contribution or explicit source",
		Long: "Verify one named port or subport, or a snapshot-relative Portfile. By default, continue the unique open contribution for the target using its committed branch and recorded verification settings. Use --working-tree to capture tracked working-tree contents, including staged additions and deletions. Stage new files with git add to include them. --branch selects a tracked contribution branch; --adopt selects the committed contents of a branch dockhand did not make. Omit the port to use a tracked contribution's single target, including its subport and variant choices. Explicit variants override those choices. Inference requires changes confined to that port relative to its recorded base. The captured snapshot stays fixed while you continue editing. Matching passing evidence is reused unless --fresh is supplied. The build runs on this Mac's macOS release; --os names others to build on instead, each in its own prepared Tart image, and every one must pass. The command stays through completion; --detach returns once the provider admits the build. Ctrl-C detaches without canceling accepted work.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if len(args) == 1 && args[0] == "" {
				return fmt.Errorf("port must not be empty")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("branch") && !git.ValidBranchName(branch) {
				return fmt.Errorf("branch must name a literal local branch")
			}
			choices, err := parseVariants(variants)
			if err != nil {
				return err
			}
			if len(releases) > 0 {
				if cmd.Flags().Changed("image") {
					return fmt.Errorf("--os builds each release on its prepared image; --image names one image, on one release")
				}
				if build.dependents {
					return fmt.Errorf("--dependents plans dependent coverage on one platform; --os cannot be combined with it yet")
				}
			}
			config, err := build.config(cmd, r.config)
			if err != nil {
				return err
			}
			if len(releases) > 0 && config.VerificationProvider == verify.ProviderGitHub {
				return fmt.Errorf("--os selects Tart images; GitHub verification builds on the fork workflow's runner matrix, which dockhand does not choose")
			}
			if config.VerificationProvider == verify.ProviderGitHub && (workingTree || fresh) {
				return fmt.Errorf("GitHub verification requires committed source; --fresh is unsupported, rerun the workflow on GitHub and verify again")
			}
			services, err := r.build(cmd.Context(), config)
			if err != nil {
				return err
			}
			defer services.Close()
			if workingTree {
				progress.VerboseReport(cmd.Context(), "Capturing working-tree source and checking verification settings")
			} else if adopt != "" {
				progress.VerboseReport(cmd.Context(), "Binding committed source from %s and checking verification settings", adopt)
			}
			var selector string
			if len(args) == 1 {
				if selector, err = portName(args[0]); err != nil {
					return err
				}
			}
			source := branch
			if adopt != "" {
				source = adopt
			}
			bound, err := services.BindVerification(cmd.Context(), app.Verification{KeepFailed: build.keepFailed, AllSubports: allSubports, WorkingTree: workingTree, ChangeID: record.ChangeID(change), UseRecordedBuild: !verificationSettingsChanged(cmd), IncludeDependents: build.dependents, ID: record.RequestID("request_" + rand.Text()), Branch: source, Adopt: adopt != "", Selection: macports.Selection{Selector: selector, Variants: choices}, Tests: record.TestPolicy(build.tests), FromSource: build.fromSource, Fresh: fresh, OS: releases})
			if err != nil {
				return err
			}
			if err := renderVerificationSource(cmd.ErrOrStderr(), bound); err != nil {
				return err
			}
			receipt, err := services.Workflow.Submit(cmd.Context(), bound.Request)
			if err != nil {
				return fmt.Errorf("accepting request %s: %w", bound.Request.ID, err)
			}
			progress.VerboseReport(cmd.Context(), "Accepted job %s; source tree %s", receipt.JobID, bound.Request.Spec.Source.Tree)
			milestone := workflow.Completion
			if detach {
				milestone = workflow.Admission
			}
			return r.attach(cmd, services, receipt.JobID, milestone, trace, false, &receipt)
		},
	}
	command.Flags().StringVar(&branch, "branch", "", "Select a tracked contribution branch (default: the current branch, or the target's)")
	command.Flags().StringVar(&adopt, "adopt", "", "Verify the committed contents of a branch dockhand did not make, tracking it as a contribution once published; dockhand adopt <branch> tracks it first")
	command.Flags().StringVar(&change, "change", "", "Select one tracked contribution when the target is ambiguous")
	command.Flags().BoolVar(&workingTree, "working-tree", false, "Explicitly capture tracked checkout edits and staged new files")
	command.MarkFlagsMutuallyExclusive("working-tree", "branch")
	command.MarkFlagsMutuallyExclusive("working-tree", "change")
	command.MarkFlagsMutuallyExclusive("adopt", "branch")
	command.MarkFlagsMutuallyExclusive("adopt", "change")
	command.MarkFlagsMutuallyExclusive("adopt", "working-tree")
	command.Flags().StringArrayVar(&variants, "variant", nil, "Explicit variant choice, such as +ssl or -x11 (repeatable)")
	command.Flags().String("remote", "", "Git remote receiving the branch for GitHub verification (default: the remote pushing to your fork)")
	build.flags(command, r.config)
	command.Flags().BoolVar(&fresh, "fresh", false, "Run a new build even when previous passing evidence applies")
	command.Flags().BoolVar(&allSubports, "all-subports", false, "Verify every subport of a shared release locally, not only the initiating one")
	command.Flags().StringArrayVar(&releases, "os", nil, "Build on this macOS release instead of this Mac's, as setup names it (sonoma or 14); repeatable, and "+app.AvailablePlatforms+" names every release with a prepared image. Tart only")
	command.Flags().BoolVar(&detach, "detach", false, "Return once the build is admitted; wait or serve finishes it")
	command.Flags().BoolVar(&trace, "trace", false, "Follow build logs on stderr through completion; implies --debug")
	command.MarkFlagsMutuallyExclusive("detach", "trace")
	section(command.Flags(), sectionSelection, "branch", "adopt", "change", "working-tree")
	section(command.Flags(), sectionBuild, "variant", "fresh", "all-subports", "os")
	section(command.Flags(), sectionGitHub, "remote")
	section(command.Flags(), sectionRun, "detach", "trace")
	return command
}
func parseVariants(values []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, value := range values {
		if len(value) < 2 || value[0] != '+' && value[0] != '-' {
			return nil, fmt.Errorf("variant %q must start with + or -", value)
		}
		name := value[1:]
		for _, c := range name {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.') {
				return nil, fmt.Errorf("invalid variant %q", value)
			}
		}
		enabled := value[0] == '+'
		if previous, ok := result[name]; ok && previous != enabled {
			return nil, fmt.Errorf("conflicting choices for variant %s", name)
		}
		result[name] = enabled
	}
	return result, nil
}

type workSelector struct{ branch, change, job string }

func (s *workSelector) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.branch, "branch", "", "Select a tracked contribution branch (defaults to the current branch)")
	cmd.Flags().StringVar(&s.change, "change", "", "Select a tracked contribution by ID")
	cmd.Flags().StringVar(&s.job, "job", "", "Select one recorded job")
	cmd.MarkFlagsMutuallyExclusive("job", "branch", "change")
}
func (s workSelector) validate(cmd *cobra.Command, args []string) error {
	for _, name := range []string{"job", "branch", "change"} {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed && flag.Value.String() == "" {
			return fmt.Errorf("--%s requires a nonempty selector", name)
		}
	}
	if len(args) == 1 && (args[0] == "" || !pathToken(args[0]) && !macports.ValidName(args[0])) {
		return fmt.Errorf("target must name a port or subport, a port directory, or a Portfile")
	}
	if len(args) > 0 && s.job != "" {
		return fmt.Errorf("select a target or --job, not both")
	}
	if s.branch != "" && !git.ValidBranchName(s.branch) {
		return fmt.Errorf("branch must name a literal recorded contribution branch")
	}
	return nil
}
func (s workSelector) contribution(ctx context.Context, services *app.Services, args []string) (workflow.ContributionSelector, error) {
	selected := workflow.ContributionSelector{Branch: s.branch, ChangeID: record.ChangeID(s.change)}
	if len(args) == 1 {
		target, err := portName(args[0])
		if err != nil {
			return selected, err
		}
		selected.Target = target
	}
	if selected == (workflow.ContributionSelector{}) {
		branch, err := services.Workflow.Repo.CurrentBranch(ctx)
		if err != nil {
			return selected, err
		}
		selected.Branch = branch
	}
	return selected, selected.Validate()
}
func (r *runtime) waitCommand() *cobra.Command {
	var selected workSelector
	var trace bool
	command := &cobra.Command{Use: "wait [target]", Short: "Resume existing work through completion", Long: "Attach to the selected contribution's pending jobs, or to one job with --job, process them in this invocation, and return when they settle. Jobs frozen at selection are the ones waited for; later submissions do not join. Ctrl-C detaches without canceling accepted work, and the next wait or serve resumes it.", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("branch") && !git.ValidBranchName(selected.branch) {
			return fmt.Errorf("branch must name a literal recorded contribution branch")
		}
		if err := selected.validate(cmd, args); err != nil {
			return err
		}
		services, err := r.build(cmd.Context(), r.config)
		if err != nil {
			return err
		}
		defer services.Close()
		if selected.job != "" {
			return r.attach(cmd, services, record.JobID(selected.job), workflow.Completion, trace, false, nil)
		}
		selector, err := selected.contribution(cmd.Context(), services, args)
		if err != nil {
			return err
		}
		scope, err := services.Workflow.ContributionScope(cmd.Context(), selector)
		if err != nil {
			return err
		}
		progress.VerboseReport(cmd.Context(), "Selected %d pending job(s): %s", len(scope.Jobs), joinJobIDs(scope.Jobs))
		return r.attachScope(cmd, services, scope, workflow.Completion, trace, false, nil, ActionResult{JobIDs: slices.Clone(scope.Jobs), Branch: selector.Branch})
	}}
	selected.flags(command)
	command.Flags().BoolVar(&trace, "trace", false, "Follow build logs on stderr through completion; implies --debug")
	return command
}
func (r *runtime) cancelCommand() *cobra.Command {
	var selected workSelector
	var wait bool
	var reason string
	command := &cobra.Command{Use: "cancel [target]", Short: "Request cancellation while preserving branches and evidence", Long: "Record a cancellation request for the selected contribution's pending jobs, or for one job with --job; the driver applies it at the next safe point, releasing environments and leaving branches, evidence, and any PR in place. The contribution stays open for a retry; abandon ends it. --wait stays attached until the selected jobs settle.", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("branch") && !git.ValidBranchName(selected.branch) {
			return fmt.Errorf("branch must name a literal recorded contribution branch")
		}
		if err := selected.validate(cmd, args); err != nil {
			return err
		}
		services, err := r.build(cmd.Context(), r.config)
		if err != nil {
			return err
		}
		defer services.Close()
		request := record.ControlRequest{ID: record.RequestID("control_" + rand.Text()), Kind: record.Cancel, Reason: reason}
		var scope workflow.Scope
		result := ActionResult{}
		if selected.job != "" {
			id := record.JobID(selected.job)
			request.Jobs = []record.JobID{id}
			if err = services.Workflow.Control(cmd.Context(), request); err != nil {
				return err
			}
			scope = workflow.Scope{Jobs: []record.JobID{id}}
			result.JobID = id
		} else {
			selector, err := selected.contribution(cmd.Context(), services, args)
			if err != nil {
				return err
			}
			scope, err = services.Workflow.ControlContribution(cmd.Context(), request, selector)
			if err != nil {
				return err
			}
			result.JobIDs, result.Branch = slices.Clone(scope.Jobs), selector.Branch
		}
		progress.Report(cmd.Context(), "Cancellation requested")
		progress.VerboseReport(cmd.Context(), "Cancellation requested for %s", joinJobIDs(scope.Jobs))
		if wait {
			return r.attachScope(cmd, services, scope, workflow.Completion, false, true, nil, result)
		}
		_, cycleErr := services.Workflow.Cycle(cmd.Context(), scope)
		status, statusErr := services.Workflow.Status(cmd.Context(), scope)
		result.Status, result.Interrupted = status, cmd.Context().Err() != nil
		return errors.Join(cycleErr, statusErr, r.result(cmd.OutOrStdout(), r.level(cmd), result))
	}}
	selected.flags(command)
	command.Flags().BoolVar(&wait, "wait", false, "Remain attached until the selected jobs settle")
	command.Flags().StringVar(&reason, "reason", "", "Record a cancellation reason")
	return command
}

func joinJobIDs(ids []record.JobID) string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = string(id)
	}
	return strings.Join(values, ", ")
}

func (r *runtime) serveCommand() *cobra.Command {
	var all, install bool
	command := &cobra.Command{Use: "serve", Short: "Run the driver as a service until stopped", Args: cobra.NoArgs,
		Long: "Run driver cycles until stopped: queued jobs are claimed and advanced, open contributions' PRs are synced on their recorded schedule, owed branch cleanup is settled, and released environments are pruned. It serves the selected repository, or with --all-repositories every repository registered in the state database whose checkout still exists, which is what a launchd agent wants, since it has no checkout of its own. One serve per database: a second refuses to start. Ctrl-C and SIGTERM stop it cleanly, and accepted work stays recorded for the next serve. --install-launchd writes a launchd agent that runs serve --all-repositories against this database with your PATH, and prints how to load it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if install {
				return r.installLaunchd(cmd)
			}
			held, err := app.HoldService(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer held.Close()
			configs := []app.Config{r.config}
			if all {
				checkouts, err := app.RegisteredCheckouts(cmd.Context(), r.config)
				if err != nil {
					return err
				}
				if len(checkouts) == 0 {
					progress.Report(cmd.Context(), "No registered repository has a checkout; nothing to serve.")
					return nil
				}
				configs = nil
				for _, checkout := range checkouts {
					config := r.config
					config.Repository = checkout
					configs = append(configs, config)
				}
			}
			group, ctx := errgroup.WithContext(cmd.Context())
			for _, config := range configs {
				services, err := r.build(ctx, config)
				if err != nil {
					return fmt.Errorf("%s: %w", config.Repository, err)
				}
				defer services.Close()
				progress.Report(ctx, "Serving %s.", config.Repository)
				reporter := newReporter(cmd.ErrOrStderr(), services.Workflow.Provider, false, r.level(cmd), r.json, r.timestamps)
				services.Processes.OnCycle = reporter.cycle
				group.Go(func() error { return services.Processes.Run(ctx, services.Workflow, workflow.Scope{All: true}) })
			}
			err = group.Wait()
			stopped := cmd.Context().Err() != nil
			if stopped {
				// Being stopped is what a service expects: launchd reads a
				// clean exit, and the work is recorded for the next serve.
				err = nil
			}
			if r.json {
				err = errors.Join(err, r.emit(struct {
					Stopped     bool
					Interrupted bool
				}{true, stopped}))
			}
			return err
		}}
	command.Flags().BoolVar(&all, "all-repositories", false, "Serve every repository registered in the state database whose checkout exists")
	command.Flags().BoolVar(&install, "install-launchd", false, "Write a launchd agent that runs serve --all-repositories against this database, and print how to load it")
	return command
}

// serviceLabel names the launchd agent in reverse-DNS form, from the module path.
const serviceLabel = "com.github.herbygillot.dockhand.serve"

// installLaunchd writes the agent's plist under the user's LaunchAgents and
// prints the launchctl commands rather than running them, so loading a
// service into the session is the person's own step, and visible.
func (r *runtime) installLaunchd(cmd *cobra.Command) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	directory := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	plist := filepath.Join(directory, serviceLabel+".plist")
	log := filepath.Join(filepath.Dir(r.config.DBPath), "serve.log")
	// A launchd agent's PATH is the system's, without MacPorts or Homebrew;
	// the agent inherits the PATH this was installed from, and the tool
	// variables set now, so serve finds what the shell finds.
	env := map[string]string{"PATH": os.Getenv("PATH")}
	for _, name := range []string{"MACPORTS_PREFIX", "TART_BIN", "GIT_BIN", "GO2PORT_BIN", "CARGO2PORT_BIN"} {
		if value := os.Getenv(name); value != "" {
			env[name] = value
		}
	}
	args := []string{executable, "--db", r.config.DBPath, "serve", "--all-repositories"}
	if err := os.WriteFile(plist, macos.LaunchdServicePlist(serviceLabel, args, log, env), 0644); err != nil {
		return err
	}
	uid := os.Getuid()
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\nLoad:   launchctl bootstrap gui/%d %s\nUnload: launchctl bootout gui/%d/%s\nLog:    %s\n", plist, uid, plist, uid, serviceLabel, log)
	return err
}
func (r *runtime) attach(cmd *cobra.Command, services *app.Services, id record.JobID, milestone workflow.Milestone, trace, canceling bool, receipt *workflow.Receipt) error {
	return r.attachScope(cmd, services, workflow.Scope{Jobs: []record.JobID{id}}, milestone, trace, canceling, receipt, ActionResult{JobID: id})
}
func (r *runtime) attachScope(cmd *cobra.Command, services *app.Services, scope workflow.Scope, milestone workflow.Milestone, trace, canceling bool, receipt *workflow.Receipt, result ActionResult) error {
	reporter := newReporter(cmd.ErrOrStderr(), services.Workflow.Provider, trace, r.level(cmd), r.json, r.timestamps)
	reporter.providers = services.Workflow.Providers
	services.Processes.OnCycle = reporter.cycle
	// The command drives only to see its job through; the driver's own
	// reports are the work behind the scenes and print at -v.
	services.Processes.Drive = progress.Quiet
	status, err := services.Processes.Attach(cmd.Context(), services.Workflow, scope, milestone, func(status workflow.Status) error { return reporter.status(cmd.Context(), status) })
	if len(status.Jobs) == 0 {
		if receipt == nil {
			return err
		}
		status = workflow.EmptyStatus(time.Time{})
	}
	result.Receipt, result.Status, result.Interrupted = receipt, status, cmd.Context().Err() != nil
	outputErr := r.result(cmd.OutOrStdout(), r.level(cmd), result)
	if err != nil {
		return errors.Join(err, outputErr)
	}
	return errors.Join(outcome(status, canceling), outputErr)
}
func outcome(status workflow.Status, canceling bool) error {
	var result error
	for _, entry := range status.Jobs {
		switch entry.Job.State {
		case record.JobFailed:
			result = errors.Join(result, errJobFailed)
		case record.JobNeedsAttention, record.JobSuperseded:
			result = errors.Join(result, errNeedsAttention)
		case record.JobCanceled:
			if !canceling {
				result = errors.Join(result, errJobCanceled)
			}
		}
	}
	return result
}

// result writes an action's outcome: the JSON result, the compact summary at
// the info level, or the full record with identifiers when -v was given.
func (r *runtime) result(out io.Writer, level progress.Level, result ActionResult) error {
	if r.json {
		return r.emit(result)
	}
	if level < progress.Verbose {
		return renderSummary(out, result.Status)
	}
	if err := renderStatus(out, result.Status); err != nil {
		return err
	}
	for _, entry := range result.Status.Jobs {
		if pending := pendingGuidance(entry.Job); pending != "" {
			if _, err := fmt.Fprintf(out, "\n%s\n", pending); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderVerificationSource(out io.Writer, bound workflow.BoundVerification) error {
	spec := bound.Request.Spec
	if c := spec.Checkout; c != nil {
		branch := c.Branch
		if branch == "" {
			branch = "detached HEAD"
		}
		if _, err := fmt.Fprintf(out, "Source: working tree (%s); HEAD %s; %d modified files captured.\n", branch, c.Head, c.ModifiedFiles); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "Source: committed branch %s; commit %s.\n", bound.Request.Branch.Name, spec.Source.Commit); err != nil {
			return err
		}
	}
	for _, target := range spec.Targets {
		origin := ""
		if bound.Request.Branch != nil && bound.Request.Branch.InferredTarget != nil {
			origin = "; inferred from tracked contribution"
		}
		var variants []string
		for name, enabled := range target.Variants {
			prefix := "-"
			if enabled {
				prefix = "+"
			}
			variants = append(variants, prefix+name)
		}
		slices.Sort(variants)
		if _, err := fmt.Fprintf(out, "Target: %s (%s)%s\n", target.Name, target.Portfile, origin); err != nil {
			return err
		}
		if len(variants) > 0 {
			if _, err := fmt.Fprintf(out, "Variants: %s\n", strings.Join(variants, " ")); err != nil {
				return err
			}
		}
	}
	return nil
}
