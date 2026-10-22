package review

import (
	"context"

	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var comment, requestChanges, markdown bool
	cmd := &cobra.Command{
		Use:   "review <pr>",
		Short: "Review a pull request against MacPorts' rules",
		Long: `Fetches a macports-ports pull request, such as 34905, and applies MacPorts'
commit rules to its commits and Portfiles, as tidy and submit do. Reviewing
it again after it changes says which findings are resolved. port lint and
the build are left to MacPorts CI.

It posts nothing unless asked: on a terminal it shows the text and asks;
--comment posts it as a comment, and --request-changes as a request for
changes, which MacPorts reads only from people with write or triage
access. --markdown prints the text, for pasting.`,
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
			fmt.Fprintln(streams.Err, view.ExperimentalWords(number, report.Author))
			body := report.Markdown()
			streams.Emit(view.ReviewView(report))
			if markdown {
				// Text for pasting is written as it is.
				streams.Unwrapped()
				fmt.Fprint(streams.Out, body)
				return e.RecordReview(ctx, report, "")
			}
			writeReview(streams.Out, report)
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
	cmd.MarkFlagsMutuallyExclusive("comment", "request-changes", "markdown")
	settings.SupportsJSON(cmd)
	return cmd
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
		fmt.Fprintf(out, "  ! upstream's change wasn't assessed: %s\n", report.UpstreamUnread)
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
		fmt.Fprintln(out, "  ✓ upstream: no license, build file, dependency, or patch changes")
	}
	if words := report.DependentsWords(); words != "" {
		fmt.Fprintf(out, "  %s\n", words)
	}
	fmt.Fprintln(out, "    not checked here: port lint and the build; MacPorts CI runs both")
}
