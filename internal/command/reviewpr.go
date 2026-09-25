package command

import (
	"context"

	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
)

func reviewCommand(s *settings, streams Streams) *cobra.Command {
	var comment, requestChanges, markdown bool
	cmd := &cobra.Command{
		Use:   "review <pr>",
		Short: "Say what a reviewer would about a pull request",
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
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			report, err := e.Review(ctx, number)
			if err != nil {
				return err
			}
			body := report.Markdown()
			streams.emit(reviewView(report))
			if markdown {
				fmt.Fprint(streams.Out, body)
				return e.RecordReview(ctx, report, "")
			}
			writeReview(streams.Out, report)
			switch {
			case comment || requestChanges:
				return postReview(ctx, e, streams, report, body, requestChanges)
			case !streams.terminal():
				fmt.Fprintln(streams.Out, "Nothing was posted; --comment or --request-changes posts it.")
				return e.RecordReview(ctx, report, "")
			}
			for {
				choices := "c comment · r request changes · e edit first · n don't post"
				if !report.CanRequestChanges() {
					choices = "c comment · e edit first · n don't post"
				}
				answer, err := ask(streams, "? post as: "+choices+"  ")
				if err != nil {
					return err
				}
				switch strings.ToLower(answer) {
				case "c":
					return postReview(ctx, e, streams, report, body, false)
				case "r":
					if !report.CanRequestChanges() {
						fmt.Fprintf(streams.Err, "Requesting changes is left to people with write or triage access; you have %s access.\n", report.Permission)
						continue
					}
					return postReview(ctx, e, streams, report, body, true)
				case "e":
					edited, err := editText(ctx, body)
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
	return cmd
}

func postReview(ctx context.Context, e *engine.Engine, streams Streams, report engine.ReviewReport, body string, requestChanges bool) error {
	url, err := e.PostReview(ctx, report, body, requestChanges)
	if err != nil {
		return err
	}
	result := reviewView(report)
	result.Body, result.Posted, result.PostedURL = body, "comment", url
	if requestChanges {
		result.Posted = "request-changes"
	}
	streams.emit(result)
	how := "a comment"
	if requestChanges {
		how = "changes requested"
	}
	fmt.Fprintf(streams.Out, "✓ review posted on #%d: %s, %s\n  %s\n", report.Ref.Number, how, plural(len(report.Comments()), "inline comment"), url)
	return nil
}

func writeReview(out io.Writer, report engine.ReviewReport) {
	access := report.Permission + " access"
	if report.Permission == "none" || report.Permission == "" {
		access = "no access"
	}
	fmt.Fprintf(out, "review of #%d %q, as @%s (%s to %s)\n", report.Ref.Number, report.Title, report.Login, access, report.Ref.Repository)
	fmt.Fprintf(out, "  %s\n", report.Summary())
	for _, finding := range report.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(out, "  ✓ the commits and Portfiles follow the rules dockhand checks")
	}
	for _, finding := range report.Resolved {
		fmt.Fprintf(out, "  ✓ resolved since %s: %s [%s]\n", engine.Short(report.Previous.Head), finding.Message, finding.Code)
	}
	fmt.Fprintln(out, "  · not checked here: port lint and the build; MacPorts CI runs both")
}
