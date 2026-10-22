package submit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/tidy"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	var note string
	var request engine.SubmitRequest
	var yes, testedBinaries, testedVariants, ready, preview bool
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Open or update the pull request",
		Long: `Pushes the branch's committed head to your fork and opens its pull request
against macports/macports-ports, or updates the one it has. The preview shows
the commits, where they go, the title, the checks, the commit rules, and any
other open pull requests for the same ports.

The committed files must have passed a check: every changed port in every
environment. A failed extra (--also) or revision-bump-only port can be
acknowledged with --accept <port>, and so can a changed port whose
baseline (check --baseline) fails it the same way, at the same phase on
the same release: the pull request says the base fails it too. --draft
allows unfinished or failing checks; --no-check submits without one, and
the pull request says so.

dockhand ship checks the committed head first, and submits exactly that
commit once its check passes; ship --passing, every branch whose check
passed. --ready takes a draft pull request out of draft, once its commit
passes as submit requires.

--note adds your own note to the pull request's description, under its
Description, kept with the branch and given each time dockhand writes the
description; --note "" takes it out.

Every push is conditional on the fork's branch being where submit last saw
it, so nobody else's push is ever overwritten. A description you edited on
GitHub is kept; --rewrite-description replaces it with dockhand's, as for
a branch you adopted whose description dockhand didn't write, your --note
kept.

--plan shows the preview and changes nothing, here or on GitHub.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			request.TestedBinaries, request.TestedVariants = testedBinaries, testedVariants
			// --note "" clears the note, which leaving --note out keeps.
			if cmd.Flags().Changed("note") {
				request.Note = &note
			}
			if preview && yes {
				return errors.New("--plan previews the submission; it goes without --yes")
			}
			if request.Branch, err = where.Resolve(ctx, e, streams); err != nil {
				return err
			}
			plan, err := e.PlanSubmit(ctx, request)
			// Edits not committed are committed as tidy would, shown as tidy
			// shows them, and then submitted, where submit refused and named
			// tidy (Design v3 §3's chaining); --head submits the committed
			// head alone, as before.
			if uncommitted := new(engine.Uncommitted); errors.As(err, &uncommitted) {
				if preview {
					return previewTidy(ctx, e, streams, request.Branch)
				}
				branch, applied, tidyErr := tidy.Stage(ctx, e, streams, request.Branch, "nothing was submitted")
				if tidyErr != nil || !applied {
					return tidyErr
				}
				request.Branch = branch
				plan, err = e.PlanSubmit(ctx, request)
			}
			if err != nil {
				return err
			}
			streams.Emit(view.SubmitView(plan))
			view.WriteSubmitPlan(streams.Out, plan)
			if len(plan.Blocking) > 0 {
				return errors.New("nothing was submitted")
			}
			if preview {
				writeReadyPreview(streams.Out, plan, ready)
				writeDescription(streams.Out, plan)
				fmt.Fprintln(streams.Out, "Nothing was submitted (--plan).")
				return nil
			}
			if !streams.Terminal() {
				if _, _, err := streams.Decide(yes, term.Decision{Undone: "nothing was submitted", ByYes: "--yes submits exactly what is shown"}); err != nil {
					return err
				}
				return finishSubmit(ctx, e, streams, plan, ready, s.File.Submit.RerequestReview)
			}
			if err := AskTested(streams, &plan); err != nil {
				return err
			}
			for {
				answer, err := streams.Ask("Preview [p] · Edit description [e] · Submit [s] · Cancel [q]\n> ")
				if err != nil {
					return err
				}
				switch strings.ToLower(answer) {
				case "p":
					fmt.Fprintf(streams.Out, "\n%s\n\n%s\n", plan.Title, plan.Body)
				case "e":
					edited, err := settings.EditText(ctx, settings.EditorCommand(s), plan.Body)
					if err != nil {
						fmt.Fprintln(streams.Err, err)
						continue
					}
					plan.Describe(edited)
				case "s":
					return finishSubmit(ctx, e, streams, plan, ready, s.File.Submit.RerequestReview)
				case "q", "":
					fmt.Fprintln(streams.Out, "Nothing was submitted.")
					return nil
				}
			}
		},
	}
	where.Register(cmd, s, "submit")
	cmd.Flags().BoolVar(&preview, "plan", false, "show what would be pushed and opened, and change nothing")
	cmd.Flags().BoolVar(&request.Head, "head", false, "submit the committed head, leaving uncommitted edits out")
	cmd.Flags().BoolVar(&request.Draft, "draft", false, "open the pull request as a draft, which unfinished or failing checks allow")
	cmd.Flags().BoolVar(&request.NoCheck, "no-check", false, "submit without a check; the pull request says no local build ran")
	cmd.Flags().BoolVar(&ready, "ready", false, "take the pull request out of draft once it is submitted")
	cmd.Flags().StringSliceVar(&request.Accept, "accept", nil, "acknowledge a failed extra, revision-bump-only port, or changed port its baseline fails alike, for this exact commit")
	cmd.Flags().BoolVar(&request.Standing, "standing", false, "with --accept, accept each port's dependency failing to install for every branch, until the dependency's version or the image changes")
	cmd.Flags().StringVar(&request.Title, "title", "", "the pull request's title (default: the commit subject)")
	cmd.Flags().BoolVar(&request.Rewrite, "rewrite-description", false, "replace the pull request's description with dockhand's, a description you wrote included; your --note is kept")
	cmd.Flags().StringVar(&note, "note", "", "your own note in the pull request's description, kept as dockhand rewrites it; \"\" takes it out")
	cmd.Flags().StringSliceVar(&request.Types, "type", nil, "the template's Type(s): bugfix, enhancement, security fix (default: enhancement for an update dockhand made)")
	cmd.Flags().StringVar(&request.Remote, "remote", "", "the Git remote of your fork, when several could be")
	cmd.Flags().BoolVar(&request.SkipNotification, "skip-notification", false, "add [skip notification], so maintainers are not mentioned")
	cmd.Flags().BoolVar(&testedBinaries, "tested-binaries", false, "state that you tested the basic functionality of all binary files")
	cmd.Flags().BoolVar(&testedVariants, "tested-variants", false, "state that you checked the most important variants")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "submit without asking, when there is no terminal")
	cmd.MarkFlagsMutuallyExclusive("draft", "no-check")
	cmd.MarkFlagsMutuallyExclusive("draft", "ready")
	settings.SupportsJSON(cmd)
	return cmd
}

// askTested asks the template's last two questions, which only the person
// can answer, unless they answered them with flags or the description
// they edited stands.
func AskTested(streams term.Streams, plan *engine.SubmitPlan) error {
	if plan.Request.TestedBinaries || plan.Request.TestedVariants || plan.Existing != nil && plan.BodyKept {
		return nil
	}
	binaries, err := streams.AskYes("? Did you test the basic functionality of all binary files? [y/N] ", false)
	if err != nil {
		return err
	}
	// A --variants each check that passed has answered this one.
	variants := false
	if plan.Evidence != nil {
		_, _, variants = plan.Evidence.VariantsBuilt()
	}
	if !variants {
		if variants, err = streams.AskYes("? Did you check that the most important variants aren't broken? [y/N] ", false); err != nil {
			return err
		}
	}
	plan.Answer(binaries, variants)
	return nil
}

// writeReadyPreview says what --ready would do after the submission.
func writeReadyPreview(out io.Writer, plan engine.SubmitPlan, ready bool) {
	if !ready {
		return
	}
	switch pr := plan.Branch.PullRequest; {
	case pr != nil && pr.Draft:
		fmt.Fprintf(out, "Then marks #%d ready for review.\n", pr.Number)
	case pr != nil:
		fmt.Fprintf(out, "#%d isn't a draft, so --ready has nothing to do.\n", pr.Number)
	case plan.Request.Draft:
		fmt.Fprintln(out, "Opens it as a draft, then marks it ready for review.")
	default:
		fmt.Fprintln(out, "It opens ready for review, not as a draft, so --ready has nothing to do.")
	}
}

// writeDescription shows the pull request's description as the submission
// would leave it, which a preview otherwise shows only in its JSON.
func writeDescription(out io.Writer, plan engine.SubmitPlan) {
	fmt.Fprintln(out, "\nProposed pull request description:")
	for _, line := range strings.Split(strings.TrimRight(plan.Body, "\n"), "\n") {
		fmt.Fprintln(out, strings.TrimRight("    "+line, " "))
	}
	fmt.Fprintln(out)
}

// finishSubmit applies a plan, then takes its pull request out of draft
// when --ready asked.
func finishSubmit(ctx context.Context, e *engine.Engine, streams term.Streams, plan engine.SubmitPlan, ready bool, rerequest string) error {
	// A person's own submission: what holds it is shown, and it goes on.
	if err := Apply(ctx, e, streams, plan, rerequest, model.AnsweredAsked); err != nil {
		return err
	}
	if !ready {
		return nil
	}
	branch, byCLI, err := e.Ready(ctx, plan.Branch)
	if err != nil {
		return err
	}
	if byCLI {
		organization, _, _ := strings.Cut(branch.PullRequest.Repository, "/")
		fmt.Fprintf(streams.Out, "Marked #%d ready for review with the GitHub CLI: the %s organization refuses dockhand's app.\n", branch.PullRequest.Number, organization)
		return nil
	}
	fmt.Fprintf(streams.Out, "Marked #%d ready for review.\n", branch.PullRequest.Number)
	return nil
}

// updatedWords says what submit changed on a pull request it pushed
// nothing to: its title, its description, both, or nothing at all.
func updatedWords(edited engine.SubmitEdits, number int) string {
	switch {
	case edited.Title && edited.Body:
		return fmt.Sprintf("Updated #%d's title and description", number)
	case edited.Title:
		return fmt.Sprintf("Updated #%d's title", number)
	case edited.Body:
		return fmt.Sprintf("Updated #%d's description", number)
	}
	return fmt.Sprintf("#%d has nothing new: the fork has its commit, and its title and description are current", number)
}

// applySubmit submits a plan and reports it. After pushing to a pull
// request whose reviewers requested changes, it asks them to review again
// as rerequest says: ask (the default), always, or never. Its JSON names
// them, and whether each was asked (the rc8 full stage's B4), however it
// ends once the submission is made.
func Apply(ctx context.Context, e *engine.Engine, streams term.Streams, plan engine.SubmitPlan, rerequest string, answered model.Answered) error {
	submitted, err := e.ApplySubmit(ctx, plan, answered)
	if err != nil {
		return err
	}
	pr := submitted.PullRequest
	result := view.SubmitView(plan)
	result.PullRequest = &view.SubmittedJSON{Number: pr.Ref.Number, URL: pr.Ref.URL, Created: submitted.Created, Pushed: submitted.Pushed, Draft: plan.Request.Draft,
		UpdatedTitle: submitted.Edited.Title, UpdatedDescription: submitted.Edited.Body, ChangesRequestedBy: []view.ChangesRequestedJSON{}}
	defer func() { streams.Emit(result) }()
	switch {
	case submitted.Created && plan.Request.Draft:
		fmt.Fprintf(streams.Out, "Opened draft #%d  %s\n", pr.Ref.Number, pr.Ref.URL)
	case submitted.Created:
		fmt.Fprintf(streams.Out, "Opened #%d  %s\n", pr.Ref.Number, pr.Ref.URL)
	case submitted.Pushed && plan.Replaces:
		fmt.Fprintf(streams.Out, "Updated #%d: replaced its history with %s (confirmed the remote branch had not changed).\n", pr.Ref.Number, prose.Plural(len(plan.Commits), "commit"))
	case submitted.Pushed:
		fmt.Fprintf(streams.Out, "Updated #%d: pushed up to %s\n", pr.Ref.Number, model.ObjectID(plan.Commit).Short())
	default:
		fmt.Fprintln(streams.Out, updatedWords(submitted.Edited, pr.Ref.Number))
	}
	observed := plan.Branch.PullRequest
	if submitted.Created || observed == nil || observed.Observed == nil {
		return nil
	}
	for _, login := range observed.Observed.ChangesRequestedBy {
		result.PullRequest.ChangesRequestedBy = append(result.PullRequest.ChangesRequestedBy, view.ChangesRequestedJSON{Login: login})
	}
	if !submitted.Pushed || len(observed.Observed.ChangesRequestedBy) == 0 || rerequest == "never" {
		return nil
	}
	who := "@" + strings.Join(observed.Observed.ChangesRequestedBy, ", @")
	if rerequest != "always" {
		again, how, err := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? ask %s to review again? [Y/n] ", who), Yes: true, Nobody: term.Answer(false)})
		if how == model.AnsweredNobody {
			fmt.Fprintf(streams.Out, "%s requested changes; ask them to review again on GitHub, or set submit.rerequest_review = \"always\"\n", who)
		}
		if err != nil || !again {
			return err
		}
	}
	asked, err := e.RequestReview(ctx, plan.Branch)
	if err != nil {
		return err
	}
	for i, requested := range result.PullRequest.ChangesRequestedBy {
		result.PullRequest.ChangesRequestedBy[i].Rerequested = slices.Contains(asked, requested.Login)
	}
	fmt.Fprintf(streams.Out, "Asked %s to review again.\n", who)
	return nil
}

// previewTidy is submit --plan of a branch whose edits aren't committed:
// the commits submit would make first, as tidy previews them, and that
// the submission's preview follows once they're made, since it's of their
// commit.
func previewTidy(ctx context.Context, e *engine.Engine, streams term.Streams, branch model.Branch) error {
	proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch})
	if err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "%s · commits first, %s\n", branch.ShortName(), view.DescribeWork(proposal))
	streams.Emit(view.TidyView(proposal))
	view.WriteTidyPlan(streams.Out, proposal, false)
	fmt.Fprintln(streams.Out, "Nothing was committed or submitted (--plan). dockhand submit makes these commits, then shows what it submits.")
	return nil
}
