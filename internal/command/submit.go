package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/term"
)

func submitCommand(s *settings, streams term.Streams) *cobra.Command {
	var where branchFlags
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
			e, err := s.open(ctx)
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
			if request.Branch, err = where.resolve(ctx, e, streams); err != nil {
				return err
			}
			plan, err := e.PlanSubmit(ctx, request)
			if err != nil {
				return err
			}
			streams.Emit(submitView(plan))
			writeSubmitPlan(streams.Out, plan)
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
				return finishSubmit(ctx, e, streams, plan, ready, s.file.Submit.RerequestReview)
			}
			if err := askTested(streams, &plan); err != nil {
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
					edited, err := editText(ctx, s.editorCommand(), plan.Body)
					if err != nil {
						fmt.Fprintln(streams.Err, err)
						continue
					}
					plan.Describe(edited)
				case "s":
					return finishSubmit(ctx, e, streams, plan, ready, s.file.Submit.RerequestReview)
				case "q", "":
					fmt.Fprintln(streams.Out, "Nothing was submitted.")
					return nil
				}
			}
		},
	}
	where.register(cmd, s, "submit")
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
	supportsJSON(cmd)
	return cmd
}

// askTested asks the template's last two questions, which only the person
// can answer, unless they answered them with flags or the description
// they edited stands.
func askTested(streams term.Streams, plan *engine.SubmitPlan) error {
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
	if err := applySubmit(ctx, e, streams, plan, rerequest); err != nil {
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
	fmt.Fprintf(out, "  As       %s\n", loginWords(plan))
	if owner, another := plan.ForkOwner(); another {
		fmt.Fprintf(out, "           ! %s belongs to %s; you will push and create or update the pull request as %s\n", plan.HeadRepository, owner, plan.Login.Account)
	}
	fmt.Fprintf(out, "  To       %s:%s\n", plan.Repository, engine.UpstreamBranch)
	if !strings.EqualFold(plan.Repository, engine.UpstreamRepository) {
		fmt.Fprintf(out, "           a sandbox, not %s, as %s says\n", engine.UpstreamRepository, pullRequestsVariable)
	}
	rules := "follows MacPorts' commit rules"
	if len(plan.Findings) > 0 {
		rules = prose.Plural(len(plan.Findings), "finding") + " below"
	}
	fmt.Fprintf(out, "  Commits  %d, %s\n", len(plan.Commits), rules)
	fmt.Fprintf(out, "  Push     %s\n", pushWords(plan))
	fmt.Fprintf(out, "  Checks   %s\n", checkWords(plan))
	if len(plan.Upstream) > 0 || len(plan.Moved) > 0 {
		lines := upstreamLines(plan)
		if len(lines) == 0 {
			lines = []string{"compared; no license, build file, or dependency changes"}
		}
		fmt.Fprintf(out, "  Upstream %s\n", strings.Join(lines, "\n           "))
		if slices.ContainsFunc(lines, func(line string) bool { return strings.HasPrefix(line, "! ") }) {
			fmt.Fprintf(out, "           %s\n", holdLegend)
		}
	}
	fmt.Fprintf(out, "  Other PRs  %s\n", otherWords(plan))
	fmt.Fprintf(out, "  PR       %s\n", pullRequestWords(plan))
	if plan.Note != "" {
		fmt.Fprintf(out, "  Note     %s\n", noteLine(plan))
	} else if plan.DescriptionSaysNothing() {
		fmt.Fprintln(out, "  Note     missing; the commit has no body, so the description does not explain the change. Add --note \"…\".")
	}
	if len(plan.LeftOut) > 0 {
		fmt.Fprintf(out, "  Left out %s (not committed)\n", strings.Join(plan.LeftOut, ", "))
	}
	for _, finding := range plan.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
	for _, commit := range plan.ModifiedBuilds {
		fmt.Fprintf(out, "  ! commit %s names a Dockhand build from uncommitted source in Generated-By. Run tidy again using a build from a pushed commit so others can identify the source.\n", model.ObjectID(commit).Short())
	}
	for _, build := range plan.UnfoundBuilds {
		fmt.Fprintf(out, "  ! %s\n", unfoundBuildWords(build))
	}
	if plan.BuildsProblem != "" {
		fmt.Fprintf(out, "  ! Could not verify the Dockhand builds listed in Generated-By on GitHub: %s\n", plan.BuildsProblem)
	}
	for _, blocking := range plan.Blocking {
		fmt.Fprintf(out, "✗ %s\n", blocking)
	}
}

// noteLine says the person's note in a line: its first, with an ellipsis
// for the rest, and where the description won't give it, why.
func noteLine(plan engine.SubmitPlan) string {
	words, rest, more := strings.Cut(plan.Note, "\n")
	if more && strings.TrimSpace(rest) != "" {
		words = strings.TrimSpace(words) + " …"
	}
	// A kept note outlives the commits it was written beside: one given
	// before a commit was amended to carry a body read twice in the
	// Description (field testing, grafana 13.2.4, #35488).
	if plan.NoteKept() {
		words += "\n           kept from an earlier submit, across rewritten commits; --note \"\" clears it"
	}
	if plan.NoteRepeatsCommits() && !plan.NoteLeftOut {
		words += "\n           ! it mostly repeats the commit message's body, which the Description gives above it; --note \"\" clears it"
	}
	if plan.NoteLeftOut {
		words += "\n           ! note omitted: the description was edited or removed on GitHub, so Dockhand preserves it"
	}
	return words
}

// upstreamLines are what comparing the branch's upstream archives found,
// a line each, marked as update marks them: a change a passing build can't
// catch, and archives that couldn't be compared, with "!". Each names its
// port when the branch updated several, and, under the Upstream label
// every list of them has, doesn't say upstream again.
func upstreamLines(plan engine.SubmitPlan) []string {
	lines := comparisonLines(plan.Upstream)
	// A Git-fetched port whose tag has moved since its check: the check
	// built another source than this would submit.
	for _, moved := range plan.Moved {
		lines = append(lines, "! "+moved.Detail)
	}
	return lines
}

// comparisonLines are what each port's assessment found, a line each,
// marked as update marks them, under an Upstream label. Where there are
// several ports, each line names the ports that found it, said once for
// all of them: a python port's subports each repeated a license finding
// and a requirement's, whose other lines differed by provider (the Vx
// port's field testing, 2026-10-04, py-coremltools and py-mlx-vlm).
func comparisonLines(comparisons []engine.PortComparison) []string {
	var lines []string
	several := len(engine.Grouped(comparisons)) > 1
	for _, l := range engine.ComparisonLines(comparisons) {
		mark, port := "· ", ""
		if l.Hold {
			mark = "! "
		}
		if several {
			port = strings.Join(l.Ports, ", ") + ": "
		}
		lines = append(lines, mark+port+l.Words)
	}
	return lines
}

func pushWords(plan engine.SubmitPlan) string {
	switch {
	// What the blocker holds isn't said as what the push would do (the
	// rc6 full stage, D-S1: "replaces the fork's branch at d2b5022 …").
	case plan.OthersPushed != "":
		return "won't push: someone else pushed " + plan.OthersPushed.Short()
	case plan.RemoteHead.Exists && plan.RemoteHead.Object == plan.Commit:
		return "nothing new; the fork already has " + model.ObjectID(plan.Commit).Short()
	case plan.Replaces:
		return fmt.Sprintf("replaces the fork's branch at %s with %s, only if it is still there", model.ObjectID(plan.RemoteHead.Object).Short(), model.ObjectID(plan.Commit).Short())
	case plan.RemoteHead.Exists:
		return fmt.Sprintf("adds to the fork's branch, up to %s", model.ObjectID(plan.Commit).Short())
	}
	return "creates the fork's branch at " + model.ObjectID(plan.Commit).Short()
}

func checkWords(plan engine.SubmitPlan) string {
	evidence := plan.Evidence
	switch {
	case plan.Request.NoCheck:
		return "none (--no-check); the pull request says MacPorts CI is its only check"
	case plan.CheckNeeded:
		return "runs first; it is submitted only if it passes"
	case evidence == nil:
		return "none has finished for this commit's files"
	}
	var environments, tested, excluded []string
	for i, environment := range evidence.Plan.Environments {
		words := engine.DescribeEnvironment(environment)
		environments = append(environments, words)
		switch {
		case evidence.Tested(i):
			tested = append(tested, words)
		case evidence.ProfileSkips(i):
			// The profile's choice, as minmax's past a port's oldest and
			// newest releases, isn't the ports' exclusion, and the table
			// leaves it out too (field testing at 9c5919dd: tart's line
			// said macOS 26, which minmax skipped, was excluded).
		case evidence.ExcludesAll(i):
			excluded = append(excluded, words)
		}
	}
	failed := evidence.Failed()
	if len(failed) == 0 {
		passed := fmt.Sprintf("passed on %s for this commit's files (%s)", strings.Join(tested, ", "), evidence.Run.Name())
		if len(excluded) > 0 {
			passed += fmt.Sprintf("; nothing built on %s, where every port's Portfile excludes it", strings.Join(excluded, ", "))
		}
		// What MacPorts' CI builds on that this check didn't: tart 2.40.1
		// passed on 26 alone and failed CI on 15 (field testing, #35157).
		if len(plan.UncoveredCI) > 0 {
			passed += fmt.Sprintf("; MacPorts CI also builds on macOS %s, which no check here built on (--build macports-ci checks those)", prose.And(plan.UncoveredCI))
		}
		return passed
	}
	// Each port is said where it failed and where nothing of these files
	// built it, apart: check-323 had passed grafana on macOS 12, and the
	// line said it "did not pass on" 12 and 26 alike (field testing,
	// grafana 13.2.4).
	var said []string
	for _, target := range failed {
		var failedOn, unbuiltOn []string
		for _, cell := range target.Outcomes {
			switch {
			case cell.Unbuilt():
				unbuiltOn = append(unbuiltOn, engine.DescribeEnvironment(cell.Environment))
			case cell.Kind == engine.CellRecorded && cell.Outcome != model.OutcomePassed:
				failedOn = append(failedOn, engine.DescribeEnvironment(cell.Environment))
			}
		}
		name := target.Target.Target.Name
		// An accepted failure says so, and why: the line listed
		// py-textual-fastdatatable's five accepted subports as "did not
		// pass" with no word of it (field testing, #35662).
		accepted := ""
		if why, ok := plan.Accepted[name]; ok {
			accepted = ", accepted: " + why.Words()
		}
		if len(failedOn) > 0 {
			said = append(said, fmt.Sprintf("%s did not pass on %s%s", name, strings.Join(failedOn, ", "), accepted))
		}
		if len(unbuiltOn) > 0 {
			said = append(said, fmt.Sprintf("%s isn't built for these files on %s", name, strings.Join(unbuiltOn, ", ")))
		}
		if len(failedOn) == 0 && len(unbuiltOn) == 0 {
			said = append(said, fmt.Sprintf("%s did not pass on %s", name, strings.Join(environments, ", ")))
		}
	}
	return fmt.Sprintf("%s: %s", evidence.Run.Name(), strings.Join(said, "; "))
}

func otherWords(plan engine.SubmitPlan) string {
	if plan.SearchProblem != "" {
		return "not searched: " + plan.SearchProblem
	}
	if len(plan.Others) == 0 {
		// The ports it looked under, which "none open for terraform" didn't
		// say of terraform-1.16's update (field testing, 2026-10-02).
		return "none open for " + strings.Join(plan.Searched, ", ")
	}
	var found []string
	for _, pr := range plan.Others {
		found = append(found, fmt.Sprintf("#%d %s", pr.Number, pr.Title))
	}
	return strings.Join(found, " · ")
}

// unfoundBuildWords say a build the commits name in Generated-By that
// nobody else can find, and why: what it was built from isn't on GitHub
// until it's pushed there, or it recorded nothing to find. Tidy keeps a
// commit whose only change would be its Generated-By, unless its build
// was of uncommitted source, so tidying again isn't what's said here.
func unfoundBuildWords(build engine.UnfoundBuild) string {
	var commits []string
	for _, commit := range build.Commits {
		commits = append(commits, model.ObjectID(commit).Short())
	}
	words := fmt.Sprintf("commit %s's Generated-By names dockhand %s", commits[0], build.Build)
	if len(commits) > 1 {
		words = fmt.Sprintf("commits %s name dockhand %s in Generated-By", strings.Join(commits, ", "), build.Build)
	}
	switch {
	case build.Source.Commit != "":
		return words + ", built from a commit dockhand's GitHub repository doesn't have, which nobody else can find until it's pushed there"
	case build.Source.Release != "":
		return words + ", built at a tag dockhand's GitHub repository doesn't have, which nobody else can find until it's pushed there"
	}
	return words + ", which recorded no commit, so nobody can find what it was built from"
}

func pullRequestWords(plan engine.SubmitPlan) string {
	if plan.Existing == nil {
		words := "opens a new one"
		if plan.Request.Draft {
			words = "opens a new draft"
		}
		if earlier := plan.Earlier; earlier != nil {
			words += fmt.Sprintf("; #%d, %s, came from a branch of this name before", earlier.Ref.Number, earlier.State)
		}
		return words
	}
	words := fmt.Sprintf("updates #%d", plan.Existing.PullRequest.Ref.Number)
	if pr := plan.Branch.PullRequest; pr != nil && pr.Draft && !plan.Request.Draft {
		words += ", a draft: dockhand submit --ready marks it ready for review"
	}
	var refreshing []string
	if plan.Sections.Description == engine.SectionRefreshed {
		refreshing = append(refreshing, "its Description section")
	}
	if plan.Sections.Types == engine.SectionRefreshed {
		refreshing = append(refreshing, "its Type(s)")
	}
	if plan.Sections.TestedOn == engine.SectionRefreshed {
		refreshing = append(refreshing, "its description from Tested on down")
	}
	// A Description a person wrote is theirs, as the part from Tested on
	// down is once they edit it.
	yours := plan.BodyKept || plan.Sections.Description == engine.SectionKept
	switch {
	case plan.Theirs:
		words += "; its title and description are theirs, and stay as they are"
	case plan.Request.Rewrite:
		words += "; replaces its whole description with dockhand's (--rewrite-description)"
	case len(refreshing) == 0 && yours:
		words += "; its description is yours, and stays as it is"
	case len(refreshing) == 0:
		words += "; its description is current"
	default:
		last := len(refreshing) - 1
		words += "; refreshes " + strings.TrimPrefix(strings.Join(refreshing[:last], ", ")+" and "+refreshing[last], " and ")
		if yours {
			words += "; the rest of its description is yours"
		}
	}
	return words
}

// loginWords say who a submission pushes and writes the pull request as,
// and where the token comes from: "ada, from GH_TOKEN".
func loginWords(plan engine.SubmitPlan) string {
	if plan.Login.From == "" {
		return plan.Login.Account
	}
	return plan.Login.Account + ", from " + plan.Login.From
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
func applySubmit(ctx context.Context, e *engine.Engine, streams term.Streams, plan engine.SubmitPlan, rerequest string) error {
	submitted, err := e.ApplySubmit(ctx, plan)
	if err != nil {
		return err
	}
	pr := submitted.PullRequest
	result := submitView(plan)
	result.PullRequest = &submittedJSON{Number: pr.Ref.Number, URL: pr.Ref.URL, Created: submitted.Created, Pushed: submitted.Pushed, Draft: plan.Request.Draft,
		UpdatedTitle: submitted.Edited.Title, UpdatedDescription: submitted.Edited.Body, ChangesRequestedBy: []changesRequestedJSON{}}
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
		result.PullRequest.ChangesRequestedBy = append(result.PullRequest.ChangesRequestedBy, changesRequestedJSON{Login: login})
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

// editText opens text in $VISUAL or $EDITOR and returns what was saved.
func editText(ctx context.Context, editor, text string) (string, error) {
	if editor == "" {
		return "", errors.New("Set $VISUAL or $EDITOR to edit the description here, or edit it on GitHub after submitting. Dockhand preserves your edits.")
	}
	directory, err := scratch.Dir("description-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "DESCRIPTION.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	if err := runEditor(ctx, editor, path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
}
