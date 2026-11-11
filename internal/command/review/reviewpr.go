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

	"github.com/herbygillot/dockhand/internal/checks"
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
	var comment, requestChanges, markdown, check, evaluateFlag bool
	var on []string
	cmd := &cobra.Command{
		Use:   "review <pr>",
		Short: "Review a pull request against MacPorts' rules",
		Long: `Fetches a macports-ports pull request, such as 34905, and applies
MacPorts' commit rules to its commits and Portfiles, as tidy and submit
do. Reviewing it again after it changes shows which findings were
resolved. Without --check, port lint and the build are left to MacPorts CI.

It posts nothing unless asked: on a terminal it shows the text and asks;
--comment posts it as a comment, and --request-changes as a request for
changes, which MacPorts honors only from people with write or triage
access. --markdown prints the text for pasting.

A full review evaluates the pull request's Portfiles with MacPorts on
this Mac, as you, for the rules that evaluate and the comparison with
upstream. A Portfile is code: before evaluating another person's, review
asks, listing the Portfiles and PortGroups it changes, with their diff one
key away; the answer is kept for that head, and a new push asks again.
No, the default, gives a text-only review, which runs the rules that
read text and says what it left out. In a script, --evaluate answers yes
and --evaluate=false reviews the text only; without either, a script's
review is text-only. Only --evaluate answers it.

--check adopts the pull request as pr-<number>, as adopt --pr does, and
checks it in Tart or the environments selected with --on. Reports the
result as check does and includes it in any posted review. Planning
evaluates the author's Portfiles with MacPorts on this Mac, so --check
needs the answer yes; the builds run in the selected environments.`,
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
			// A branch --check would find by number, tracking another
			// repository's #number, is refused before the review fetches.
			if check {
				if err := e.TrackedElsewhere(ctx, number); err != nil {
					return err
				}
			}
			target, err := e.OpenReview(ctx, number)
			if err != nil {
				return err
			}
			evaluate, err := decideEvaluation(ctx, e, streams, target, evaluation{asked: evaluateFlag, given: cmd.Flags().Changed("evaluate"), check: check})
			if err != nil {
				return err
			}
			report, err := e.Review(ctx, target, evaluate)
			if err != nil {
				return err
			}
			body := report.Markdown()
			// The review is the command's one result, its check and its
			// posting gathered into it (term.LinkSteps).
			streams.LinkSteps(&view.ReviewJSON{})
			streams.Emit(view.ReviewView(report))
			if markdown {
				// Text for pasting is written raw.
				fmt.Fprint(streams.Out, body)
				return e.RecordReview(ctx, report, "")
			}
			writeReview(streams.Prose(), report)
			// --check adopts it and builds it, and a script that posts
			// nothing ends with the check's answer (Then's item 12).
			// The text posted says the check, rendered after it, and the
			// command ends with the check's answer whether or not it posts:
			// a posted review turned a failed check into success, saying
			// Dockhand hadn't built it (Codex's workflow review of 1caa6c3d,
			// finding 7).
			var checked error
			if check {
				var run model.Run
				var environments []string
				run, environments, checked = checkPullRequest(ctx, e, s, streams, number, model.ObjectID(report.Head), on)
				if run.ID != "" {
					if ended, err := e.Run(ctx, run.ID); err == nil {
						run = ended
					}
					report.Check = &engine.ReviewCheck{Run: run, Environments: environments}
					body = report.Markdown()
				}
				if !comment && !requestChanges && !streams.Terminal() {
					return errors.Join(checked, e.RecordReview(ctx, report, ""))
				}
			}
			switch {
			case comment || requestChanges:
				return errors.Join(postReview(ctx, e, streams, report, body, requestChanges), checked)
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
					return errors.Join(postReview(ctx, e, streams, report, body, false), checked)
				case "r":
					if !report.CanRequestChanges() {
						fmt.Fprintf(streams.Err, "Requesting changes requires write or triage access; your access is %s.\n", engine.Access(report.Permission))
						continue
					}
					return errors.Join(postReview(ctx, e, streams, report, body, true), checked)
				case "e":
					edited, err := settings.EditText(ctx, settings.EditorCommand(s), body)
					if err != nil {
						fmt.Fprintln(streams.Err, err)
						continue
					}
					body = edited
				default:
					fmt.Fprintln(streams.Prose(), "Nothing was posted.")
					return errors.Join(e.RecordReview(ctx, report, ""), checked)
				}
			}
		},
	}
	cmd.Flags().BoolVar(&comment, "comment", false, "post the review as a comment")
	cmd.Flags().BoolVar(&requestChanges, "request-changes", false, "post the review as a request for changes")
	cmd.Flags().BoolVar(&markdown, "markdown", false, "print the review's text, for pasting, and post nothing")
	cmd.Flags().BoolVar(&check, "check", false, "adopt the pull request and check it in Tart, as check does")
	cmd.Flags().StringArrayVar(&on, "on", nil, "with --check, select build environments as for check --on (default: tart)")
	cmd.Flags().BoolVar(&evaluateFlag, "evaluate", false, "evaluate another person's Portfiles on this Mac without asking, for this head; --evaluate=false reviews the text only")
	cmd.MarkFlagsMutuallyExclusive("comment", "request-changes", "markdown")
	cmd.MarkFlagsMutuallyExclusive("check", "markdown")
	settings.SupportsJSON(cmd)
	return cmd
}

// evaluation is what the command line says of evaluating another
// person's Portfiles: --evaluate's value, whether it was given, and
// whether --check needs it.
type evaluation struct {
	asked, given, check bool
}

// decideEvaluation decides whether a review evaluates the pull request's
// Portfiles (#162): the person's own, or a head the person allowed, as
// --evaluate=false doesn't say otherwise; another's, at --evaluate's word,
// kept for the head, or the person's answer on a terminal, kept where it
// is yes; and in a script without --evaluate, a text-only review, which
// review --check refuses before anything is adopted.
func decideEvaluation(ctx context.Context, e *engine.Engine, streams term.Streams, target engine.ReviewTarget, asked evaluation) (bool, error) {
	refuseCheck := func() (bool, error) {
		return false, fmt.Errorf("review --check plans the check by evaluating #%d's Portfiles on this Mac, which needs your yes; nothing was checked. In a script, add --evaluate", target.Number)
	}
	if asked.given && !asked.asked {
		if asked.check {
			return refuseCheck()
		}
		return false, nil
	}
	if !target.Asks() {
		return true, nil
	}
	if asked.given {
		return true, e.AllowEvaluation(ctx, target.Number, model.ObjectID(target.Head))
	}
	if !streams.Terminal() {
		if asked.check {
			return refuseCheck()
		}
		return false, nil
	}
	writeQuestion(streams.Prose(), target)
	for {
		choices := "y yes · d show the diff first · n review the text only  [n] "
		if target.Since != nil {
			choices = "y yes · d show what changed · n review the text only  [n] "
		}
		answer, err := streams.Ask("? Evaluate them: " + choices)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, e.AllowEvaluation(ctx, target.Number, model.ObjectID(target.Head))
		case "d":
			diff, err := e.ReviewDiff(ctx, target)
			if err != nil {
				return false, err
			}
			// The diff is written raw, as tidy's is (#94).
			fmt.Fprint(streams.Out, string(diff))
		default:
			if asked.check {
				return refuseCheck()
			}
			return false, nil
		}
	}
}

// writeQuestion says what the question is about: whose the pull request
// is, what evaluating it does, and the Portfiles and PortGroups it
// changes, since the head allowed last where there is one.
func writeQuestion(w io.Writer, target engine.ReviewTarget) {
	if target.Since != nil {
		fmt.Fprintf(w, "#%d has a new head, %s. You allowed %s on %s.\n", target.Number, model.ObjectID(target.Head).Short(), target.Since.Head.Short(), target.Since.At.Local().Format("2006-01-02"))
	} else {
		fmt.Fprintf(w, "#%d is %s. A full review evaluates its Portfiles with MacPorts on this Mac, as you. A Portfile is code: it runs with your files and your GitHub login in reach.\n", target.Number, target.Whose())
	}
	width := 0
	for _, file := range target.Portfiles {
		width = max(width, len(file.Path))
	}
	for _, file := range target.Portfiles {
		since := ""
		if target.Since != nil {
			since = "   (since " + target.Since.Head.Short() + ")"
		}
		fmt.Fprintf(w, "  %-*s  +%d -%d%s\n", width, file.Path, file.Added, file.Removed, since)
	}
}

// checkPullRequest adopts a pull request at head, the commit the review
// read, or takes the branch it was adopted as, moved to head where it's
// untouched since, and checks that commit where on says, Tart where it
// says nothing, reporting as check does: the review and its check are of
// one commit, where review --check reviewed B and checked A (Codex's
// review of 2ad896d2, W2). It returns the check's run, where one was
// queued, and the environments it builds on, for the review's text.
func checkPullRequest(ctx context.Context, e *engine.Engine, s *settings.Settings, streams term.Streams, number int, head model.ObjectID, on []string) (model.Run, []string, error) {
	adoption, err := e.AdoptPullRequest(ctx, number, head)
	if err != nil {
		return model.Run{}, nil, err
	}
	branch := adoption.Branch
	switch {
	case !adoption.Already:
		fmt.Fprintf(streams.Prose(), "Adopted #%d at %s as %s; checking it.\n", number, head.Short(), branch.ShortName())
	case adoption.Head != head:
		if branch, err = e.RefreshAdopted(ctx, branch, head); err != nil {
			return model.Run{}, nil, fmt.Errorf("nothing was checked: %w", err)
		}
		fmt.Fprintf(streams.Prose(), "#%d is tracked as %s; moved it from %s to the head reviewed, %s, and checking it.\n", number, branch.ShortName(), adoption.Head.Short(), head.Short())
	default:
		fmt.Fprintf(streams.Prose(), "#%d is tracked as %s, at the head reviewed, %s; checking it.\n", number, branch.ShortName(), head.Short())
	}
	if len(on) == 0 {
		on = []string{"tart"}
	}
	place, err := e.Where(ctx, engine.WhereRequest{On: on})
	if err != nil {
		return model.Run{}, nil, err
	}
	proposal, err := e.ProposeCheck(ctx, engine.CheckRequest{Capture: engine.CaptureRequest{Branch: branch, Mode: engine.CaptureHead, Named: true}, Where: place})
	if err != nil {
		return model.Run{}, nil, err
	}
	if captured := proposal.Capture.Revision.Source.Commit; captured != head {
		return model.Run{}, nil, fmt.Errorf("%s moved to %s while review --check ran; nothing was checked", branch.ShortName(), captured.Short())
	}
	view.WritePlan(streams.Prose(), proposal.Plan, e.PlanNotes(ctx, proposal.Plan), e.PlanSiblings(ctx, proposal.Plan), e.Remedy, view.ListShown(streams))
	if err := checks.Runnable(proposal.Plan); err != nil {
		return model.Run{}, nil, fmt.Errorf("nothing was checked: %w", err)
	}
	// A check running that covers this one, by the engine's rule
	// (PlanCovers), is followed.
	queued, err := e.QueueCheck(ctx, checks.QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: model.OriginPerson, Beside: checks.BesideFollow})
	if err != nil {
		return model.Run{}, nil, err
	}
	var environments []string
	for _, environment := range proposal.Plan.Environments {
		environments = append(environments, checks.DescribeEnvironment(environment))
	}
	if queued.Followed {
		fmt.Fprintf(streams.Err, "%s is already %s and covers this check; following it.\n", queued.Run.Name(), queued.Run.State)
		return queued.Run, environments, checkrun.Wait(ctx, e, queued.Run, streams, s.Places())
	}
	return queued.Run, environments, checkrun.RunQueued(ctx, e, queued.Run, streams, false, s.Places(), s.Drain)
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
	fmt.Fprintf(streams.Prose(), "✓ review posted on #%d: %s, %s\n  %s\n", report.Ref.Number, how, prose.Plural(len(report.Comments()), "inline comment"), url)
	return nil
}

func writeReview(out io.Writer, report engine.ReviewReport) {
	access := engine.Access(report.Permission)
	if words := report.TextOnlyWords(); words != "" {
		fmt.Fprintln(out, words)
	}
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
