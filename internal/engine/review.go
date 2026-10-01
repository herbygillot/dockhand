package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ReviewReport is what dockhand would say about someone's pull request
// (Design v3 §6.11): its commits and Portfiles against §8's rules, which
// findings of the last review are resolved, what upstream's change means
// for each port it changes, and the ports that depend on them.
type ReviewReport struct {
	Ref   forge.PullRequestRef
	Title string
	State forge.PullRequestState
	// Login is who would post it, and Permission their role on the
	// repository: admin, maintain, write, triage, read, or none.
	Login, Permission string
	// Head is the commit reviewed; Base where it leaves master.
	Head, Base string
	Commits    []git.HistoryCommit
	Ports      []string
	Findings   []commitrules.Finding
	// Previous is the last review of the pull request, if any, and
	// Resolved its findings that no longer hold.
	Previous *model.Review
	Resolved []model.ReviewFinding
	// Upstream is what upstream's change means for each port the pull
	// request changes, against its base, assessed as a branch's revision
	// is, its patches included; UpstreamUnread why it couldn't be.
	Upstream       []PortComparison
	UpstreamUnread string
	// Dependents are the other ports that depend on those it changes, from
	// the index at its base: candidates to look at, not proof of anything;
	// DependentsUnread why they couldn't be read.
	Dependents       []Dependent
	DependentsUnread string
}

// CanRequestChanges reports whether the reviewer may request changes:
// GitHub lets anyone review, but MacPorts reads a request for changes
// from someone with write or triage access.
func (r ReviewReport) CanRequestChanges() bool {
	return slices.Contains([]string{"admin", "maintain", "write", "triage"}, r.Permission)
}

// Summary is the review's first line.
func (r ReviewReport) Summary() string {
	ports := strings.Join(r.Ports, ", ")
	if ports == "" {
		ports = "no port"
	}
	summary := fmt.Sprintf("%s changing %s", plural(len(r.Commits), "commit"), ports)
	if len(r.Commits) > max(1, len(r.Ports)) {
		summary += "; MacPorts asks for one commit per logical change. To squash: dockhand tidy, or git rebase -i master and push with --force-with-lease"
	}
	return summary
}

// Review reads pull request number of MacPorts' repository: fetches its
// head, applies the commit rules to its commits and Portfiles, compares
// the findings with the last review's, assesses each port it changes as
// a branch's revision is assessed, without recording it, and reads their
// dependents, as update finds them for one's own (the libuv run's finding
// 2). What couldn't be assessed or read is said, never an error. It posts
// nothing.
func (e *Engine) Review(ctx context.Context, number int) (ReviewReport, error) {
	ref := forge.PullRequestRef{Forge: forge.GitHub, Repository: UpstreamRepository, Number: number}
	report := ReviewReport{Ref: ref}
	f := e.forge()
	observed, err := f.Observe(ctx, ref)
	if err != nil {
		return report, fmt.Errorf("reading #%d: %w", number, err)
	}
	pr := observed.PullRequest
	report.Title, report.State, report.Ref.URL = pr.Title, pr.State, pr.Ref.URL
	if report.Login, err = f.AuthenticatedUser(ctx); err != nil {
		return report, fmt.Errorf("review needs your GitHub login: %w", err)
	}
	if report.Permission, err = f.Permission(ctx, UpstreamRepository, report.Login); err != nil {
		return report, fmt.Errorf("reading your access to %s: %w", UpstreamRepository, err)
	}
	if report.Head, err = e.Repo.FetchPullRequest(ctx, e.Upstream(), number); err != nil {
		return report, fmt.Errorf("fetching #%d: %w", number, err)
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return report, err
	}
	if report.Base, err = e.Repo.MergeBase(ctx, string(master), report.Head); err != nil {
		return report, err
	}
	if report.Commits, err = e.Repo.History(ctx, report.Base, report.Head); err != nil {
		return report, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{report.Base, report.Head})
	if err != nil {
		return report, err
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[report.Base], trees[report.Head])
	if err != nil {
		return report, err
	}
	report.Ports = ScopeOf(changed).PortNames()
	report.Findings = commitrules.CheckCommits(ruleCommits(report.Commits))
	portfiles, err := portfileFindings(ctx, e.Repo, trees[report.Base], trees[report.Head], changed)
	if err != nil {
		return report, err
	}
	report.Findings = append(report.Findings, portfiles...)
	base, head := model.ObjectID(trees[report.Base]), model.ObjectID(trees[report.Head])
	found, made, err := e.makeAssessments(ctx, "", model.ObjectID(report.Base), base, head, changed, nil)
	if err != nil {
		report.UpstreamUnread = err.Error()
	}
	for _, a := range append(found, made...) {
		report.Upstream = append(report.Upstream, PortComparison{Port: a.Port, Comparison: a.Comparison})
	}
	scope := ScopeOf(changed)
	if len(scope.Ports) > 0 {
		dependents, err := e.dependents(ctx, model.Source{Commit: model.ObjectID(report.Base), Base: model.ObjectID(report.Base), Tree: base}, scope.Ports)
		if err != nil {
			report.DependentsUnread = err.Error()
		}
		for _, dependent := range dependents {
			if !slices.Contains(scope.Ports, dependent.Directory) {
				report.Dependents = append(report.Dependents, dependent)
			}
		}
	}

	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		previous, err := r.LastReview(UpstreamRepository, number)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		report.Previous = &previous
		return err
	})
	if err != nil || report.Previous == nil {
		return report, err
	}
	now := reviewFindings(report.Findings)
	for _, finding := range report.Previous.Findings {
		if !slices.ContainsFunc(now, func(f model.ReviewFinding) bool { return sameFinding(f, finding) }) {
			report.Resolved = append(report.Resolved, finding)
		}
	}
	return report, nil
}

// sameFinding reports whether two reviews found the same thing. Commit
// IDs change when a branch is rebased, so a commit finding is known by
// its code and message.
func sameFinding(a, b model.ReviewFinding) bool {
	return a.Code == b.Code && a.Message == b.Message && a.Where == b.Where
}

func reviewFindings(findings []commitrules.Finding) []model.ReviewFinding {
	all := []model.ReviewFinding{}
	for _, f := range findings {
		all = append(all, model.ReviewFinding{Code: f.Code, Severity: string(f.Severity), Where: f.Where, Message: f.Message})
	}
	return all
}

// Markdown is the review's text as it would be posted. Findings on a
// Portfile line are also posted as comments on that line.
func (r ReviewReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Reviewed at %s with [dockhand](https://github.com/herbygillot/dockhand)'s commit rules.\n\n", short(model.ObjectID(r.Head)))
	fmt.Fprintf(&b, "%s.\n", r.Summary())
	if len(r.Findings) > 0 {
		b.WriteString("\n")
		for _, finding := range r.Findings {
			fmt.Fprintf(&b, "- %s\n", finding)
		}
	} else {
		b.WriteString("\nThe commits and Portfiles follow the rules dockhand checks.\n")
	}
	if len(r.Resolved) > 0 {
		fmt.Fprintf(&b, "\nResolved since the review at %s:\n", short(r.Previous.Head))
		for _, finding := range r.Resolved {
			fmt.Fprintf(&b, "- ~~%s~~ [%s]\n", finding.Message, finding.Code)
		}
	}
	switch lines := r.UpstreamWords(); {
	case r.UpstreamUnread != "":
		fmt.Fprintf(&b, "\nUpstream's change wasn't assessed: %s.\n", r.UpstreamUnread)
	case len(lines) > 0:
		b.WriteString("\nWhat upstream's change means, comparing the source archives with the base's:\n")
		for _, line := range lines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	case len(r.Upstream) > 0:
		b.WriteString("\nComparing the source archives with the base's found no license, build file, dependency, or patch changes.\n")
	}
	if words := r.DependentsWords(); words != "" {
		fmt.Fprintf(&b, "\n%s\n", words)
	}
	b.WriteString("\nNot checked here: `port lint` and the build; MacPorts CI runs both.\n")
	return b.String()
}

// UpstreamWords are what the assessment of each port found, a line each,
// naming its port where the pull request changes several.
func (r ReviewReport) UpstreamWords() []string {
	var lines []string
	for _, found := range r.Upstream {
		port := ""
		if len(r.Upstream) > 1 {
			port = found.Port + ": "
		}
		if found.Comparison.Problem != "" {
			lines = append(lines, port+"archives not compared: "+found.Comparison.Problem)
		}
		for _, change := range found.Comparison.Changes {
			lines = append(lines, port+change.Message)
		}
	}
	return lines
}

// reviewDependentsNamed is how many dependents a review names.
const reviewDependentsNamed = 8

// DependentsWords says the pull request's dependents in a line, naming the
// first few with how they depend, and why they couldn't be read where they
// couldn't; empty where it changes no port that has any.
func (r ReviewReport) DependentsWords() string {
	switch {
	case r.DependentsUnread != "":
		return "Dependents weren't read: " + r.DependentsUnread + "."
	case len(r.Dependents) == 0:
		return ""
	}
	var named []string
	for _, dependent := range r.Dependents[:min(reviewDependentsNamed, len(r.Dependents))] {
		named = append(named, dependent.Words())
	}
	words := strings.Join(named, ", ")
	if more := len(r.Dependents) - len(named); more > 0 {
		words += fmt.Sprintf(", and %d more", more)
	}
	return fmt.Sprintf("%s, from the index at %s: %s; candidates to look at.", plural(len(r.Dependents), "dependent"), short(model.ObjectID(r.Base)), words)
}

// Comments are the findings on a Portfile line, as comments on that line.
func (r ReviewReport) Comments() []forge.ReviewComment {
	var comments []forge.ReviewComment
	for _, finding := range r.Findings {
		path, line, ok := strings.Cut(finding.Where, ":")
		n, err := strconv.Atoi(line)
		if !ok || err != nil {
			continue
		}
		comments = append(comments, forge.ReviewComment{Path: path, Line: n, Body: fmt.Sprintf("%s [%s]", finding.Message, finding.Code)})
	}
	return comments
}

// RecordReview keeps a review, posted or not, so the next one of the same
// pull request can say what is resolved. posted is "comment",
// "request-changes", or "".
func (e *Engine) RecordReview(ctx context.Context, report ReviewReport, posted string) error {
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddReview(model.Review{Repository: report.Ref.Repository, Number: report.Ref.Number, Head: model.ObjectID(report.Head),
			Findings: reviewFindings(report.Findings), Posted: posted, At: e.now()}); err != nil {
			return err
		}
		message := fmt.Sprintf("reviewed #%d at %s: %s", report.Ref.Number, short(model.ObjectID(report.Head)), plural(len(report.Findings), "finding"))
		if posted != "" {
			message += ", posted as " + strings.ReplaceAll(posted, "-", " ")
		}
		_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "review", Level: model.LevelInfo, Message: message})
		return err
	})
}

// PostReview posts a review's text, and its Portfile comments, on the pull
// request at the commit it reviewed, and records it.
func (e *Engine) PostReview(ctx context.Context, report ReviewReport, body string, requestChanges bool) (string, error) {
	if requestChanges && !report.CanRequestChanges() {
		return "", fmt.Errorf("requesting changes is left to people with write or triage access to %s; you have %s access, so post it as a comment", report.Ref.Repository, report.Permission)
	}
	url, err := e.forge().PostReview(ctx, forge.ReviewInput{Ref: report.Ref, Commit: report.Head, RequestChanges: requestChanges, Body: body, Comments: report.Comments()})
	if err != nil {
		return "", fmt.Errorf("posting the review on #%d: %w", report.Ref.Number, err)
	}
	posted := "comment"
	if requestChanges {
		posted = "request-changes"
	}
	return url, e.RecordReview(ctx, report, posted)
}
