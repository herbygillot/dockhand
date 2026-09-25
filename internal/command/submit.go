package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

func submitCommand(s *settings, streams Streams) *cobra.Command {
	var selector string
	var request engine.SubmitRequest
	var yes, testedBinaries, testedVariants bool
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Open or update the pull request",
		Long: `Pushes the branch's committed head to your fork and opens its pull request
against macports/macports-ports, or updates the one it has. The preview shows
the commits, where they go, the title, the checks, the commit rules, and any
other open pull requests for the same ports.

The committed files must have passed a check: every changed port in every
environment. A failed extra (--also) or revision-bump-only port can be
acknowledged with --accept <port>. --draft allows unfinished or failing
checks; --no-check submits without one, and the pull request says so.

Every push is conditional on the fork's branch being where submit last saw
it, so nobody else's push is ever overwritten. A description you edited on
GitHub is kept.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if request.Branch, err = workingBranch(ctx, e, selector); err != nil {
				return err
			}
			request.TestedBinaries, request.TestedVariants = testedBinaries, testedVariants
			plan, err := e.PlanSubmit(ctx, request)
			if err != nil {
				return err
			}
			writeSubmitPlan(streams.Out, plan)
			if len(plan.Blocking) > 0 {
				return errors.New("nothing was submitted")
			}
			if !streams.terminal() {
				if !yes {
					return errors.New("nothing was submitted: without a terminal, --yes submits exactly what is shown")
				}
				return applySubmit(ctx, e, streams, plan)
			}
			if !testedBinaries && !testedVariants && (plan.Existing == nil || !plan.BodyKept) {
				binaries, err := confirm(streams, "? Did you test the basic functionality of all binary files? [y/N] ")
				if err != nil {
					return err
				}
				variants, err := confirm(streams, "? Did you check that the most important variants aren't broken? [y/N] ")
				if err != nil {
					return err
				}
				plan.Answer(binaries, variants)
			}
			for {
				answer, err := ask(streams, "Preview [p] · Edit description [e] · Submit [s] · Cancel [q]\n> ")
				if err != nil {
					return err
				}
				switch strings.ToLower(answer) {
				case "p":
					fmt.Fprintf(streams.Out, "\n%s\n\n%s\n", plan.Title, plan.Body)
				case "e":
					edited, err := editText(ctx, plan.Body)
					if err != nil {
						fmt.Fprintln(streams.Err, err)
						continue
					}
					plan.Describe(edited)
				case "s":
					return applySubmit(ctx, e, streams, plan)
				case "q", "":
					fmt.Fprintln(streams.Out, "Nothing was submitted.")
					return nil
				}
			}
		},
	}
	cmd.Flags().StringVar(&selector, "branch", "", "submit this tracked branch")
	cmd.Flags().BoolVar(&request.Head, "head", false, "submit the committed head, leaving uncommitted edits out")
	cmd.Flags().BoolVar(&request.Draft, "draft", false, "open the pull request as a draft, which unfinished or failing checks allow")
	cmd.Flags().BoolVar(&request.NoCheck, "no-check", false, "submit without a check; the pull request says no local build ran")
	cmd.Flags().StringSliceVar(&request.Accept, "accept", nil, "acknowledge a failed extra or revision-bump-only port for this exact commit")
	cmd.Flags().StringVar(&request.Title, "title", "", "the pull request's title (default: the commit subject)")
	cmd.Flags().StringSliceVar(&request.Types, "type", nil, "the template's Type(s): bugfix, enhancement, security fix")
	cmd.Flags().StringVar(&request.Remote, "remote", "", "the Git remote of your fork, when several could be")
	cmd.Flags().BoolVar(&request.SkipNotification, "skip-notification", false, "add [skip notification], so maintainers are not mentioned")
	cmd.Flags().BoolVar(&testedBinaries, "tested-binaries", false, "state that you tested the basic functionality of all binary files")
	cmd.Flags().BoolVar(&testedVariants, "tested-variants", false, "state that you checked the most important variants")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "submit without asking, when there is no terminal")
	cmd.MarkFlagsMutuallyExclusive("draft", "no-check")
	return cmd
}

func writeSubmitPlan(out io.Writer, plan engine.SubmitPlan) {
	state := "ready to submit"
	if len(plan.Blocking) > 0 {
		state = "can't submit yet"
	}
	fmt.Fprintf(out, "%s · %s\n", plan.Branch.ShortName(), state)
	title := plan.Title
	if title == "" {
		title = "(none)"
	}
	fmt.Fprintf(out, "  Title    %s\n", title)
	fmt.Fprintf(out, "  From     %s\n", plan.Head())
	fmt.Fprintf(out, "  To       %s:%s\n", plan.Repository, engine.UpstreamBranch)
	rules := "follows MacPorts' commit rules"
	if len(plan.Findings) > 0 {
		rules = plural(len(plan.Findings), "finding") + " below"
	}
	fmt.Fprintf(out, "  Commits  %d, %s\n", len(plan.Commits), rules)
	fmt.Fprintf(out, "  Push     %s\n", pushWords(plan))
	fmt.Fprintf(out, "  Checks   %s\n", checkWords(plan))
	fmt.Fprintf(out, "  Other PRs  %s\n", otherWords(plan))
	fmt.Fprintf(out, "  PR       %s\n", pullRequestWords(plan))
	if len(plan.LeftOut) > 0 {
		fmt.Fprintf(out, "  Left out %s (not committed)\n", strings.Join(plan.LeftOut, ", "))
	}
	for _, finding := range plan.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
	for _, blocking := range plan.Blocking {
		fmt.Fprintf(out, "✗ %s\n", blocking)
	}
}

func pushWords(plan engine.SubmitPlan) string {
	switch {
	case plan.RemoteHead.Exists && plan.RemoteHead.Object == plan.Commit:
		return "nothing new; the fork already has " + engine.Short(model.ObjectID(plan.Commit))
	case plan.Replaces:
		return fmt.Sprintf("replaces the fork's branch at %s with %s, only if it is still there", engine.Short(model.ObjectID(plan.RemoteHead.Object)), engine.Short(model.ObjectID(plan.Commit)))
	case plan.RemoteHead.Exists:
		return fmt.Sprintf("adds to the fork's branch, up to %s", engine.Short(model.ObjectID(plan.Commit)))
	}
	return "creates the fork's branch at " + engine.Short(model.ObjectID(plan.Commit))
}

func checkWords(plan engine.SubmitPlan) string {
	evidence := plan.Evidence
	switch {
	case plan.Request.NoCheck:
		return "none (--no-check); the pull request says MacPorts CI is its only check"
	case evidence == nil:
		return "none has finished for this commit's files"
	}
	var environments []string
	for _, environment := range evidence.Plan.Environments {
		environments = append(environments, fmt.Sprintf("%s %s %s", environment.Provider, environment.Platform.Version, environment.Platform.Architecture))
	}
	failed := evidence.Failed()
	if len(failed) == 0 {
		return fmt.Sprintf("passed on %s for this commit's files (%s)", strings.Join(environments, ", "), evidence.Run.Name())
	}
	var names []string
	for _, target := range failed {
		names = append(names, target.Target.Target.Name)
	}
	return fmt.Sprintf("%s: %s did not pass on %s", evidence.Run.Name(), strings.Join(names, ", "), strings.Join(environments, ", "))
}

func otherWords(plan engine.SubmitPlan) string {
	if plan.SearchProblem != "" {
		return "not searched: " + plan.SearchProblem
	}
	if len(plan.Others) == 0 {
		return "none open for " + strings.Join(plan.Ports, ", ")
	}
	var found []string
	for _, pr := range plan.Others {
		found = append(found, fmt.Sprintf("#%d %s", pr.Number, pr.Title))
	}
	return strings.Join(found, " · ")
}

func pullRequestWords(plan engine.SubmitPlan) string {
	if plan.Existing == nil {
		if plan.Request.Draft {
			return "opens a new draft"
		}
		return "opens a new one"
	}
	words := fmt.Sprintf("updates #%d", plan.Existing.PullRequest.Ref.Number)
	if pr := plan.Branch.PullRequest; pr != nil && pr.Draft && !plan.Request.Draft {
		words += ", a draft: mark it ready for review on GitHub when it is"
	}
	if plan.BodyKept {
		words += "; its description is yours, and stays as it is"
	}
	return words
}

func applySubmit(ctx context.Context, e *engine.Engine, streams Streams, plan engine.SubmitPlan) error {
	submitted, err := e.ApplySubmit(ctx, plan)
	if err != nil {
		return err
	}
	pr := submitted.PullRequest
	switch {
	case submitted.Created && plan.Request.Draft:
		fmt.Fprintf(streams.Out, "Opened draft #%d  %s\n", pr.Ref.Number, pr.Ref.URL)
	case submitted.Created:
		fmt.Fprintf(streams.Out, "Opened #%d  %s\n", pr.Ref.Number, pr.Ref.URL)
	case submitted.Pushed && plan.Replaces:
		fmt.Fprintf(streams.Out, "Updated #%d: replaced its history with %s (the fork's branch was where submit saw it)\n", pr.Ref.Number, plural(len(plan.Commits), "commit"))
	case submitted.Pushed:
		fmt.Fprintf(streams.Out, "Updated #%d: pushed up to %s\n", pr.Ref.Number, engine.Short(model.ObjectID(plan.Commit)))
	default:
		fmt.Fprintf(streams.Out, "Updated #%d's title and description\n", pr.Ref.Number)
	}
	return nil
}

func confirm(streams Streams, question string) (bool, error) {
	answer, err := ask(streams, question)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), err
}

// editText opens text in $VISUAL or $EDITOR and returns what was saved.
func editText(ctx context.Context, text string) (string, error) {
	editor := firstOf(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if editor == "" {
		return "", errors.New("set $EDITOR to edit the description here, or edit it on GitHub after submitting; dockhand keeps your edits")
	}
	directory, err := os.MkdirTemp("", "dockhand-description-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "DESCRIPTION.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "sh", path)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("the editor failed: %w", err)
	}
	data, err := os.ReadFile(path)
	return string(data), err
}
