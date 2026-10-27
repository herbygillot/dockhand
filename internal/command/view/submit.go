package view

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// WriteSubmitPlan is a submission's plan: where it goes, what it pushes,
// and what holds it. noteStill is where --note can still be added, a
// preview's or a question's, the only places a missing note is said
// (part 5, item 3: it was in 56 runs, most where nobody could add one).
func WriteSubmitPlan(out io.Writer, plan engine.SubmitPlan, noteStill bool) {
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
		fmt.Fprintf(out, "           a sandbox, not %s, as %s says\n", engine.UpstreamRepository, settings.PullRequestsVariable)
	}
	rules := "follows MacPorts' commit rules"
	if len(plan.Findings) > 0 {
		rules = prose.Plural(len(plan.Findings), "finding") + " below"
	}
	fmt.Fprintf(out, "  Commits  %d, %s\n", len(plan.Commits), rules)
	fmt.Fprintf(out, "  Push     %s\n", PushWords(plan))
	fmt.Fprintf(out, "  Checks   %s\n", CheckWords(plan))
	if len(plan.Upstream) > 0 || len(plan.Moved) > 0 {
		lines := UpstreamLines(plan)
		if len(lines) == 0 {
			lines = []string{"compared; no license, build file, or dependency changes"}
		}
		fmt.Fprintf(out, "  Upstream %s\n", strings.Join(lines, "\n           "))
	}
	// What would hold serve's submission of it, which a person's own goes
	// ahead of: its major versions, a raised floor, and the ports its
	// holds name first (the M1's C15 on rc2, which the text never said).
	for i, hold := range ServeHolds(plan) {
		label := "           "
		if i == 0 {
			label = "  Serve    "
			fmt.Fprintf(out, "%swould hold it:\n", label)
			label = "           "
		}
		fmt.Fprintf(out, "%s%s\n", label, hold)
	}
	fmt.Fprintf(out, "  Other PRs  %s\n", otherWords(plan))
	fmt.Fprintf(out, "  PR       %s\n", PullRequestWords(plan))
	if plan.Note != "" {
		fmt.Fprintf(out, "  Note     %s\n", noteLine(plan))
	} else if noteStill && plan.DescriptionSaysNothing() {
		fmt.Fprintln(out, "  Note     missing; the commit has no body, so the description does not explain the change. Add --note \"…\".")
	}
	if len(plan.LeftOut) > 0 {
		fmt.Fprintf(out, "  Left out %s (not committed)\n", strings.Join(plan.LeftOut, ", "))
	}
	// A patch changed under its own name is said, a notice: the upstream
	// assessment reads the base's source for it, and a check applies it.
	for i, patch := range plan.ChangedPatches {
		label := "           "
		if i == 0 {
			label = "  Patches  "
		}
		fmt.Fprintf(out, "%s%s changed; dockhand diff shows it\n", label, patch)
	}
	for _, finding := range plan.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
	// Said once, naming each commit, where it was a line a commit (part 5,
	// item 3: in 69 runs).
	if len(plan.ModifiedBuilds) > 0 {
		var commits []string
		for _, commit := range plan.ModifiedBuilds {
			commits = append(commits, model.ObjectID(commit).Short())
		}
		which := "commit "
		if len(commits) > 1 {
			which = "commits "
		}
		fmt.Fprintf(out, "  ! Generated-By names a dockhand build from uncommitted source in %s%s. Run tidy again using a build from a pushed commit so others can identify the source.\n", which, prose.And(commits))
	}
	for _, build := range plan.UnfoundBuilds {
		fmt.Fprintf(out, "  ! %s\n", UnfoundBuildWords(build))
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
		words += "\n           ! the note mostly repeats the commit message body, which the pull request description already includes above it; --note \"\" clears it"
	}
	if plan.NoteLeftOut {
		words += "\n           ! note omitted: the pull request description was edited or removed on GitHub, so Dockhand will preserve it"
	}
	return words
}

// upstreamLines are what comparing the branch's upstream archives found,
// a line each, marked as update marks them: a change a passing build can't
// catch, and archives that couldn't be compared, with "!". Each names its
// port when the branch updated several, and, under the Upstream label
// every list of them has, doesn't say upstream again.
func UpstreamLines(plan engine.SubmitPlan) []string {
	lines := ComparisonLines(plan.Upstream)
	// A Git-fetched port whose tag has moved since its check: the check
	// built another source than this would submit.
	for _, moved := range plan.Moved {
		lines = append(lines, Look+" held: "+moved.Detail)
	}
	return lines
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
func UnfoundBuildWords(build engine.UnfoundBuild) string {
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

func PullRequestWords(plan engine.SubmitPlan) string {
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
		words += "; the author wrote its title and description, and Dockhand will preserve them"
	case plan.Request.Rewrite:
		words += "; Dockhand will replace the entire pull request description with its own (--rewrite-description)"
	case len(refreshing) == 0 && yours:
		words += "; you wrote the pull request description, and Dockhand will preserve it"
	case len(refreshing) == 0:
		words += "; the pull request description is current"
	default:
		last := len(refreshing) - 1
		words += "; refreshes " + strings.TrimPrefix(strings.Join(refreshing[:last], ", ")+" and "+refreshing[last], " and ")
		if yours {
			words += "; the rest of the description is yours, and Dockhand will preserve it"
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

// ServeHolds are the words of each hold serve's submission of the branch
// would wait on, its majors, floors, and holds naming ports first, a line
// each, once each; none hold a person's own submission.
func ServeHolds(plan engine.SubmitPlan) []string {
	var words []string
	for _, holds := range [][]model.Concern{plan.Majors, plan.Floors, plan.After} {
		for _, hold := range holds {
			if hold.Detail != "" && !slices.Contains(words, hold.Detail) {
				words = append(words, hold.Detail)
			}
		}
	}
	return words
}
