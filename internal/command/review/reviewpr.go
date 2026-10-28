package review

import (
	"context"
	"errors"

	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var comment, requestChanges, markdown, check bool
	var on []string
	cmd := &cobra.Command{
		Use:   "review <pr>",
		Short: "Review a pull request against MacPorts' rules",
		Long: `Fetches a macports-ports pull request, such as 34905, and applies
MacPorts' commit rules to its commits and Portfiles, as tidy and submit
do. Reviewing it again after it changes shows which findings were
resolved. port lint and the build are left to MacPorts CI.

It posts nothing unless asked: on a terminal it shows the text and asks;
--comment posts it as a comment, and --request-changes as a request for
changes, which MacPorts honors only from people with write or triage
access. --markdown prints the text for pasting.

--check also builds it: it adopts the pull request as adopt --pr does,
as pr-<number>, and checks it in Tart, or where --on says, reporting as
check does. Planning the check evaluates its author's Portfiles with
MacPorts on this Mac, which it says; the builds run in the guests.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			number, err := strconv.Atoi(strings.TrimPrefix(args[0], "#"))
			if err != nil || number <= 0 {
				return fmt.Errorf("%q is not a pull request number, such as 34905", args[0])
			}
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			report, err := e.Review(ctx, number)
			if err != nil {
				return err
			}
			body := report.Markdown()
			// The review is the command's one result, its check and its
			// posting gathered into it (term.LinkSteps).
			streams.LinkSteps(&view.ReviewJSON{})
			streams.Emit(view.ReviewView(report))
			if markdown {
				// Text for pasting is written as it is.
				streams.Unwrapped()
				fmt.Fprint(streams.Out, body)
				return e.RecordReview(ctx, report, "")
			}
			writeReview(streams.Out, report)
			// --check adopts it and builds it, and a script that posts
			// nothing ends with the check's answer (Then's item 12).
			if check {
				checked := checkPullRequest(ctx, e, s, streams, number, on)
				if !comment && !requestChanges && !streams.Terminal() {
					return errors.Join(checked, e.RecordReview(ctx, report, ""))
				}
				if checked != nil {
					fmt.Fprintf(streams.Err, "check: %v\n", checked)
				}
			}
			switch {
			case comment || requestChanges:
				return postReview(ctx, e, streams, report, body, requestChanges)
			case !streams.Terminal():
				if err := e.RecordReview(ctx, report, ""); err != nil {
					return err
				}
				return &term.ExitError{Code: term.ExitFailure, Message: "No review was posted. In a script, use --comment or --request-changes to post it."}
			}
			for {
				choices := "c comment · r request changes · e edit first · n don't post"
				if !report.CanRequestChanges() {
					choices = "c comment · e edit first · n don't post"
				}
				answer, err := streams.Ask("? post as: " + choices + "  ")
				if err != nil {
					return err
				}
				switch strings.ToLower(answer) {
				case "c":
					return postReview(ctx, e, streams, report, body, false)
				case "r":
					if !report.CanRequestChanges() {
						fmt.Fprintf(streams.Err, "Requesting changes requires write or triage access; your access is %s.\n", engine.Access(report.Permission))
						continue
					}
					return postReview(ctx, e, streams, report, body, true)
				case "e":
					edited, err := settings.EditText(ctx, settings.EditorCommand(s), body)
					if err != nil {
						fmt.Fprintln(streams.Err, err)
						continue
					}
					body = edited
				default:
					fmt.Fprintln(streams.Out, "Nothing was posted.")
					return e.RecordReview(ctx, report, "")
				}
			}
		},
	}
	cmd.Flags().BoolVar(&comment, "comment", false, "post the review as a comment")
	cmd.Flags().BoolVar(&requestChanges, "request-changes", false, "post the review as a request for changes")
	cmd.Flags().BoolVar(&markdown, "markdown", false, "print the review's text, for pasting, and post nothing")
	cmd.Flags().BoolVar(&check, "check", false, "adopt the pull request and check it in Tart, as check does")
	cmd.Flags().StringArrayVar(&on, "on", nil, "with --check, where it builds, as check --on takes it; tart where none is given")
	cmd.MarkFlagsMutuallyExclusive("comment", "request-changes", "markdown")
	cmd.MarkFlagsMutuallyExclusive("check", "markdown")
	settings.SupportsJSON(cmd)
	return cmd
}

// checkPullRequest adopts a pull request, or takes the branch it was
// adopted as, and checks its commits where on says, Tart where it says
// nothing, reporting as check does.
func checkPullRequest(ctx context.Context, e *engine.Engine, s *settings.Settings, streams term.Streams, number int, on []string) error {
	adoption, err := e.AdoptPullRequest(ctx, number)
	if err != nil {
		return err
	}
	branch := adoption.Branch
	if adoption.Already {
		fmt.Fprintf(streams.Out, "#%d is tracked as %s; checking it.\n", number, branch.ShortName())
	} else {
		fmt.Fprintf(streams.Out, "Adopted #%d as %s; checking it.\n", number, branch.ShortName())
	}
	if len(on) == 0 {
		on = []string{"tart"}
	}
	place, err := e.Where(ctx, engine.WhereRequest{On: on})
	if err != nil {
		return err
	}
	proposal, err := e.ProposeCheck(ctx, engine.CheckRequest{Capture: engine.CaptureRequest{Branch: branch, Mode: engine.CaptureHead, Named: true}, Where: place})
	if err != nil {
		return err
	}
	view.WritePlan(streams.Out, proposal.Plan, e.PlanNotes(ctx, proposal.Plan), e.PlanSiblings(ctx, proposal.Plan), e.Remedy, view.ListShown(streams))
	if err := engine.Runnable(proposal.Plan); err != nil {
		return fmt.Errorf("nothing was checked: %w", err)
	}
	// A check running that covers this one, by the engine's rule
	// (PlanCovers), is followed.
	queued, err := e.QueueCheck(ctx, engine.QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: model.OriginPerson, Beside: engine.BesideFollow})
	if err != nil {
		return err
	}
	if queued.Followed {
		fmt.Fprintf(streams.Err, "%s is already %s and covers this check; following it.\n", queued.Run.Name(), queued.Run.State)
		return checkrun.Wait(ctx, e, queued.Run, streams, s.Places())
	}
	return checkrun.RunQueued(ctx, e, queued.Run, streams, false, s.Places(), s.Drain)
}

func postReview(ctx context.Context, e *engine.Engine, streams term.Streams, report engine.ReviewReport, body string, requestChanges bool) error {
	url, err := e.PostReview(ctx, report, body, requestChanges)
	if err != nil {
		return err
	}
	result := view.ReviewView(report)
	result.Body, result.Posted, result.PostedURL = body, "comment", url
	if requestChanges {
		result.Posted = "request-changes"
	}
	streams.Emit(result)
	how := "a comment"
	if requestChanges {
		how = "changes requested"
	}
	fmt.Fprintf(streams.Out, "✓ review posted on #%d: %s, %s\n  %s\n", report.Ref.Number, how, prose.Plural(len(report.Comments()), "inline comment"), url)
	return nil
}

func writeReview(out io.Writer, report engine.ReviewReport) {
	access := engine.Access(report.Permission)
	fmt.Fprintf(out, "review of #%d %q, as @%s (%s to %s)\n", report.Ref.Number, report.Title, report.Login, access, report.Ref.Repository)
	fmt.Fprintf(out, "  %s\n", report.Summary())
	if report.Behind != "" {
		fmt.Fprintf(out, "    %s\n", report.Behind)
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(out, "  ✓ the commits and Portfiles follow the rules dockhand checks")
	}
	for _, finding := range report.Resolved {
		fmt.Fprintf(out, "  ✓ resolved since %s: %s [%s]\n", report.Previous.Head.Short(), finding.Message, finding.Code)
	}
	switch lines := view.ComparisonLines(report.Upstream); {
	case report.UpstreamUnread != "":
		fmt.Fprintf(out, "  ! Upstream changes could not be assessed: %s\n", report.UpstreamUnread)
	case len(lines) > 0:
		// What MacPorts has of a dependency is read at the pull request's
		// commits, which master may have moved past, as it had for an
		// older or merged one: the commits are named (field testing's
		// py-mlx-vlm, 2026-10-04: transformers 5.3.0, where master had
		// 5.18.0 by then).
		heading := "  Upstream:"
		if report.Head != "" && report.Base != "" {
			head := model.ObjectID(report.Head).Short()
			heading = fmt.Sprintf("  Upstream comparison using ports at pull request commit %s (based on master %s):", head, model.ObjectID(report.Base).Short())
			// Where a finding names the head, it's the pull request's head
			// here, not named again (part 5, item 3).
			if slices.ContainsFunc(report.Findings, func(f commitrules.Finding) bool { return strings.Contains(f.String(), head) }) {
				heading = fmt.Sprintf("  Upstream comparison using ports at the pull request's head (based on master %s):", model.ObjectID(report.Base).Short())
			}
		}
		fmt.Fprintln(out, heading)
		for _, line := range lines {
			fmt.Fprintf(out, "    %s\n", line)
		}
	case len(report.Upstream) > 0:
		fmt.Fprintln(out, "  ✓ Upstream comparison: no license, build file, dependency, or patch changes")
	}
	if words := report.DependentsWords(); words != "" {
		fmt.Fprintf(out, "  %s\n", words)
	}
	fmt.Fprintln(out, "    Not checked by Dockhand: port lint and the build; MacPorts CI runs both")
}
