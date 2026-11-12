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
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/remotewrite"
	"github.com/herbygillot/dockhand/internal/store"
)

// ReviewReport is what dockhand would say about someone's pull request
// (Design v3 §6.11): its commits and Portfiles against §8's rules, which
// findings of the last review are resolved, what upstream's change means
// for each port it changes, and the ports that depend on them.
// AccessUnknown is a role on the repository the forge wouldn't say.
const AccessUnknown = "unknown"

// Access words a role on the repository: "write access", "no access", or
// "access dockhand couldn't read with this login".
func Access(permission string) string {
	switch permission {
	case "", "none":
		return "no access"
	case AccessUnknown:
		return "access dockhand couldn't read with this login"
	}
	return permission + " access"
}

type ReviewReport struct {
	Ref   forge.PullRequestRef
	Title string
	// Author is who opened the pull request, whose Portfiles the review
	// evaluates.
	Author string
	// Login is who would post it, and Permission their role on the
	// repository: admin, maintain, write, triage, read, or none, or
	// AccessUnknown where the forge wouldn't say.
	Login, Permission string
	// Head is the commit reviewed; Base where it leaves master.
	Head, Base string
	// Behind says that master here was behind the pull request's base,
	// and where it was reviewed from instead; empty where it wasn't.
	Behind   string
	Commits  []git.HistoryCommit
	Ports    []string
	Findings []commitrules.Finding
	// Previous is the last review of the pull request, if any, Resolved
	// its findings that no longer hold, and Carried those whose rules this
	// review didn't run, which it neither found nor resolved: a text-only
	// review's (textOnlyUnrun). They are recorded with its findings, so the
	// next comparison still has them.
	Previous *model.Review
	Resolved []model.ReviewFinding
	Carried  []model.ReviewFinding
	// Upstream is what upstream's change means for each port the pull
	// request changes, against its base, assessed as a branch's revision
	// is, its patches included; UpstreamUnread why it couldn't be.
	Upstream       []PortComparison
	UpstreamUnread string
	// Dependents are the other ports that depend on those it changes, from
	// the index at its base: candidates to look at, not proof of anything;
	// DependentsUnread why they couldn't be read.
	Dependents       []treequery.Dependent
	DependentsUnread string
	// Evaluated is a full review, which evaluated the pull request's
	// Portfiles; a text-only review didn't, and LeftOut are what it
	// didn't run, in words (#162).
	Evaluated bool
	LeftOut   []string
	// Check is the check review --check made of the head reviewed, which
	// the posted text says in place of "Not checked": a failed check's
	// review was posted saying Dockhand hadn't built it (Codex's workflow
	// review of 1caa6c3d, finding 7). Nil where it made none.
	Check *ReviewCheck
}

// ReviewCheck is review --check's check of the head reviewed: its run, as
// it ended, and the environments it built on, as checks.DescribeEnvironment
// says them.
type ReviewCheck struct {
	Run          model.Run
	Environments []string
}

// Words say the check in a sentence: "Dockhand ran `port lint` and the
// build in check-12 on tart macOS 15 (Sequoia) arm64, which failed; MacPorts
// CI runs both too."
func (c ReviewCheck) Words() string {
	how := "which is " + string(c.Run.State)
	switch c.Run.State {
	case model.RunPassed, model.RunFailed:
		how = "which " + string(c.Run.State)
	case model.RunCanceled:
		how = "which was canceled"
	case model.RunAttention:
		how = "which needs attention"
	}
	return fmt.Sprintf("Dockhand ran `port lint` and the build in %s on %s, %s; MacPorts CI runs both too.", c.Run.Name(), prose.And(c.Environments), how)
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
	summary := fmt.Sprintf("%s changing %s", prose.Plural(len(r.Commits), "commit"), ports)
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
// readingPullRequest is the error of a pull request that couldn't be read:
// where one isn't found, it says where it was looked for, the sandbox
// DOCKHAND_PULL_REQUESTS names included, rather than a bare "404 Not
// Found" (the rc6 full stage, E9).
func (e *Engine) readingPullRequest(number int, err error) error {
	if !errors.Is(err, forge.ErrNotFound) {
		return fmt.Errorf("reading #%d: %w", number, err)
	}
	repository := e.PullRequestRepository()
	where := repository
	if repository != UpstreamRepository {
		where += ", the sandbox DOCKHAND_PULL_REQUESTS names"
	}
	return fmt.Errorf("no pull request #%d in %s: %w", number, where, err)
}

// ReviewTarget is a pull request fetched for review, before anything of
// its author's is evaluated: who opened it, its head and base, and the
// Portfiles and PortGroups it changes, which the person is asked about
// before a full review evaluates them (#162).
type ReviewTarget struct {
	Number                           int
	Ref                              forge.PullRequestRef
	Title, Author, Login, Permission string
	Head, Base, Behind               string
	Commits                          []git.HistoryCommit
	Ports                            []string
	// Own is the person's own pull request, and Allowed one whose head
	// the person allowed to be evaluated: neither asks.
	Own, Allowed bool
	// Since is the newest author's commit the person allowed before this
	// head, where there is one: the question shows what changed since.
	Since *model.EvaluationConsent
	// Portfiles are the Portfiles and PortGroups the pull request
	// changes, since Since where there is one, else since its base.
	Portfiles []ChangedFile
	trees     map[string]string
	changed   []string
}

// ChangedFile is a file a change makes, with the lines it adds and
// removes.
type ChangedFile struct {
	Path           string
	Added, Removed int
}

// Whose is whose Portfiles the target's are: "@alice's", or "#35174's
// author's" where the author couldn't be read.
func (t ReviewTarget) Whose() string { return pullRequestWhose(t.Number, t.Author) }

// Asks reports whether a full review of the target asks first: another
// person's pull request whose head the person hasn't allowed.
func (t ReviewTarget) Asks() bool { return !t.Own && !t.Allowed }

// OpenReview fetches pull request number of MacPorts' repository for
// review, evaluating nothing of its author's: its head, its base, its
// commits, the ports it changes as master's index names them, and the
// Portfiles and PortGroups it changes, with whether the person allowed
// its head to be evaluated.
func (e *Engine) OpenReview(ctx context.Context, number int) (ReviewTarget, error) {
	ref := pullRequestRef(e.PullRequestRepository(), number)
	target := ReviewTarget{Number: number, Ref: ref}
	f := e.forge()
	observed, err := f.Observe(ctx, ref)
	if err != nil {
		return target, e.readingPullRequest(number, err)
	}
	pr := observed.PullRequest
	target.Title, target.Ref.URL, target.Author = pr.Title, pr.Ref.URL, pr.Author
	if target.Login, err = f.AuthenticatedUser(ctx); err != nil {
		return target, loginError("review", err)
	}
	target.Own = target.Author != "" && target.Author == target.Login
	// A role the forge won't say, as GitHub won't to a fine-grained
	// token, is unknown: the review goes on, and only requesting changes,
	// which needs one, waits (the M1's rerun, E9).
	if target.Permission, err = f.Permission(ctx, e.PullRequestRepository(), target.Login); errors.Is(err, forge.ErrAccessUnknown) {
		target.Permission = AccessUnknown
	} else if err != nil {
		return target, fmt.Errorf("reading your access to %s: %w", e.PullRequestRepository(), err)
	}
	if target.Head, err = e.Repo.FetchPullRequest(ctx, e.pullRequestRemote(), e.PullRequestRepository(), number); err != nil {
		return target, fmt.Errorf("fetching #%d: %w", number, err)
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return target, err
	}
	if target.Base, err = e.Repo.MergeBase(ctx, string(master), target.Head); err != nil {
		return target, err
	}
	if target.Commits, err = e.Repo.History(ctx, target.Base, target.Head); err != nil {
		return target, err
	}
	// Master here behind the pull request's own base would make what it
	// changes everything master changed since, each port of it assessed
	// and its index built: a one-port pull request's review ran 99
	// minutes against a pinned master (the M1's run at d302e744, E9). The
	// forge's count of its commits says where it leaves its base, which
	// is reviewed from instead, and said.
	if n := pr.Commits; n > 0 && len(target.Commits) > n {
		own, err := e.Repo.Resolve(ctx, fmt.Sprintf("%s~%d^{commit}", target.Head, n))
		if err != nil {
			return target, err
		}
		target.Behind = fmt.Sprintf("The local master is behind pull request #%d's base. The pull request has %s, but %s separate the local master from its head. Reviewing from %s, the commit before the pull request's own commits.", number, prose.Plural(n, "commit"), prose.Plural(len(target.Commits), "commit"), model.ObjectID(own).Short())
		target.Base = own
		if target.Commits, err = e.Repo.History(ctx, target.Base, target.Head); err != nil {
			return target, err
		}
	}
	if target.trees, err = e.Repo.CommitTrees(ctx, []string{target.Base, target.Head}); err != nil {
		return target, err
	}
	if target.changed, err = e.Repo.ChangedPaths(ctx, target.trees[target.Base], target.trees[target.Head]); err != nil {
		return target, err
	}
	target.Ports = macports.ScopeOf(target.changed).PortNames()
	// A changed directory's other ports are said too, as the base's index
	// names them: devel/libuv's libuv-devel. The base is master's, merged.
	baseSource := model.Source{Commit: model.ObjectID(target.Base), Base: model.ObjectID(target.Base), Tree: model.ObjectID(target.trees[target.Base])}
	defined := e.portsDefined(ctx, baseSource, macports.ScopeOf(target.changed).Ports)
	for _, directory := range macports.ScopeOf(target.changed).Ports {
		for _, name := range AlsoDefined(directory, defined) {
			if !slices.Contains(target.Ports, name) {
				target.Ports = append(target.Ports, name)
			}
		}
	}
	if target.Own {
		return target, nil
	}
	consents, err := e.EvaluationConsents(ctx, number)
	if err != nil {
		return target, err
	}
	for i, consent := range consents {
		if consent.Head == model.ObjectID(target.Head) {
			target.Allowed = true
			break
		}
		if target.Since == nil {
			target.Since = &consents[i]
		}
	}
	if target.Since != nil && !target.Allowed {
		if _, err := e.Repo.CommitTrees(ctx, []string{string(target.Since.Head)}); err != nil {
			target.Since = nil
		}
	}
	diff, err := e.ReviewDiff(ctx, target)
	if err != nil {
		return target, err
	}
	target.Portfiles = changedFiles(diff)
	return target, nil
}

// code are the paths of a change that are code MacPorts evaluates: its
// Portfiles and PortGroups, a changed PortGroup being the author's code
// too.
func code(changed []string) []string {
	var paths []string
	for _, path := range changed {
		if strings.HasSuffix(path, "/Portfile") || strings.HasPrefix(path, macports.PortGroupDirectory+"/") {
			paths = append(paths, path)
		}
	}
	return paths
}

// changedFiles are the files a unified diff changes, each with the lines
// it adds and removes, in the diff's order.
func changedFiles(diff []byte) []ChangedFile {
	var files []ChangedFile
	for _, line := range strings.Split(string(diff), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			path := line[strings.LastIndex(line, " b/")+3:]
			files = append(files, ChangedFile{Path: path})
		case len(files) == 0, strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
		case strings.HasPrefix(line, "+"):
			files[len(files)-1].Added++
		case strings.HasPrefix(line, "-"):
			files[len(files)-1].Removed++
		}
	}
	return files
}

// ReviewDiff is the diff the question offers: of the target's Portfiles
// and PortGroups, since the head the person allowed last where there is
// one, else since its base.
//
// Its paths are those changed between the two trees it compares: since an
// allowed head, a PortGroup that head added and this one removes is code
// the person hasn't seen go, though the base and the head agree on it
// (Codex's review of #162, finding 3).
func (e *Engine) ReviewDiff(ctx context.Context, target ReviewTarget) ([]byte, error) {
	since, changed := target.trees[target.Base], target.changed
	if target.Since != nil {
		if trees, err := e.Repo.CommitTrees(ctx, []string{string(target.Since.Head)}); err == nil {
			since = trees[string(target.Since.Head)]
			if changed, err = e.Repo.ChangedPaths(ctx, since, target.trees[target.Head]); err != nil {
				return nil, err
			}
		}
	}
	return e.Repo.DiffTrees(ctx, since, target.trees[target.Head], code(changed)...)
}

// textOnlyLeftOut are what a text-only review doesn't run, in its words.
var textOnlyLeftOut = []string{"restated values", "path-style dependencies", "the upstream comparison"}

// textOnlyUnrun are the rules a text-only review doesn't run, by their
// findings' codes: restated values and path-style dependencies evaluate,
// and a commit's subject isn't judged by its port where only the head
// defines it (commitsDefining's Unread). An earlier finding of one of them
// is carried forward, never resolved, since its rule didn't run (Codex's
// review of #162, finding 2).
var textOnlyUnrun = []string{"restated", "path-style", "subject-port"}

// Review reviews a pull request OpenReview fetched: applies the commit
// rules to its commits and Portfiles, compares the findings with the last
// review's, and reads the dependents of the ports it changes from master's
// index, as update finds them for one's own (the libuv run's finding 2).
// With evaluate, the person's own pull request or one whose head they
// allowed, it evaluates its Portfiles too, for the rules that evaluate,
// and assesses each port it changes as a branch's revision is, without
// recording it. Without, it is a text-only review, which runs no rule
// that needs the head evaluated and says what it left out (#162). What
// couldn't be assessed or read is said, never an error. It posts nothing.
func (e *Engine) Review(ctx context.Context, target ReviewTarget, evaluate bool) (ReviewReport, error) {
	number := target.Number
	report := ReviewReport{Ref: target.Ref, Title: target.Title, Author: target.Author, Login: target.Login, Permission: target.Permission,
		Head: target.Head, Base: target.Base, Behind: target.Behind, Commits: target.Commits, Ports: target.Ports, Evaluated: evaluate}
	trees, changed := target.trees, target.changed
	baseSource := model.Source{Commit: model.ObjectID(report.Base), Base: model.ObjectID(report.Base), Tree: model.ObjectID(trees[report.Base])}
	headSource := model.Source{Commit: model.ObjectID(report.Head), Base: model.ObjectID(report.Base), Tree: model.ObjectID(trees[report.Head])}
	base, head := model.ObjectID(trees[report.Base]), model.ObjectID(trees[report.Head])
	if evaluate {
		// Another person's Portfiles go through the door, which says
		// whose once.
		if !target.Own {
			ctx = workspace.WithOwner(ctx, workspace.Owner{Whose: target.Whose(), Of: fmt.Sprintf("#%d", number)})
		}
		report.Findings = commitrules.CheckCommits(e.ruleCommits(ctx, baseSource, headSource, report.Commits))
		portfiles, err := e.portfileFindings(ctx, e.Repo, trees[report.Base], trees[report.Head], changed)
		if err != nil {
			return report, err
		}
		report.Findings = append(report.Findings, portfiles...)
		found, made, err := e.makeAssessments(ctx, "", model.ObjectID(report.Base), base, head, changed, nil, nil)
		if err != nil {
			report.UpstreamUnread = err.Error()
		}
		for _, a := range append(found, made...) {
			if !SourceUnchanged(a.Comparison) {
				report.Upstream = append(report.Upstream, PortComparison{Port: a.Port, Comparison: a.Comparison})
			}
		}
	} else {
		report.LeftOut = textOnlyLeftOut
		report.Findings = commitrules.CheckCommits(e.commitsDefining(ctx, baseSource, nil, report.Commits))
		portfiles, err := e.textFindings(ctx, e.Repo, trees[report.Base], trees[report.Head], changed)
		if err != nil {
			return report, err
		}
		report.Findings = append(report.Findings, portfiles...)
	}
	scope := macports.ScopeOf(changed)
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

	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		previous, err := r.LastReview(e.PullRequestRepository(), number)
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
		switch {
		case slices.ContainsFunc(now, func(f model.ReviewFinding) bool { return sameFinding(f, finding) }):
		case !report.Evaluated && slices.Contains(textOnlyUnrun, finding.Code):
			report.Carried = append(report.Carried, finding)
		default:
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

// TextOnlyWords is a text-only review's first line, which every form of
// it carries, so the author and other reviewers know what it didn't look
// at: "Text-only review: #35174's Portfiles were not evaluated. Not run:
// restated values, path-style dependencies, the upstream comparison."
// Empty for a full review.
func (r ReviewReport) TextOnlyWords() string {
	if r.Evaluated {
		return ""
	}
	return fmt.Sprintf("Text-only review: #%d's Portfiles were not evaluated. Not run: %s.", r.Ref.Number, strings.Join(r.LeftOut, ", "))
}

// Markdown is the review's text as it would be posted. Findings on a
// Portfile line are also posted as comments on that line.
func (r ReviewReport) Markdown() string {
	var b strings.Builder
	if words := r.TextOnlyWords(); words != "" {
		fmt.Fprintf(&b, "%s\n\n", words)
	}
	fmt.Fprintf(&b, "Reviewed at %s with [dockhand](https://github.com/herbygillot/dockhand)'s commit rules.\n\n", model.ObjectID(r.Head).Short())
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
		fmt.Fprintf(&b, "\nResolved since the review at %s:\n", r.Previous.Head.Short())
		for _, finding := range r.Resolved {
			fmt.Fprintf(&b, "- ~~%s~~ [%s]\n", finding.Message, finding.Code)
		}
	}
	if len(r.Carried) > 0 {
		fmt.Fprintf(&b, "\nFound by the review at %s, by rules this review didn't run:\n", r.Previous.Head.Short())
		for _, finding := range r.Carried {
			fmt.Fprintf(&b, "- %s [%s]\n", finding.Message, finding.Code)
		}
	}
	switch lines := r.UpstreamWords(); {
	case r.UpstreamUnread != "":
		fmt.Fprintf(&b, "\nCould not assess upstream changes: %s.\n", r.UpstreamUnread)
	case len(lines) > 0:
		b.WriteString("\nUpstream changes found by comparing source archives with the base version:\n")
		for _, line := range lines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	case len(r.Upstream) > 0:
		b.WriteString("\nThe source archive comparison found no changes to licenses, build files, dependencies, or patches.\n")
	}
	if words := r.DependentsWords(); words != "" {
		fmt.Fprintf(&b, "\n%s\n", words)
	}
	if r.Check != nil {
		fmt.Fprintf(&b, "\n%s\n", r.Check.Words())
	} else {
		b.WriteString("\nNot checked by Dockhand: `port lint` and the build; MacPorts CI runs both.\n")
	}
	return b.String()
}

// UpstreamWords are what the assessment of each port found, a line each,
// naming its port where the pull request changes several.
func (r ReviewReport) UpstreamWords() []string {
	var lines []string
	grouped := Grouped(r.Upstream)
	for _, found := range grouped {
		port := ""
		if len(grouped) > 1 {
			port = found.Port + ": "
		}
		if found.Comparison.Problem != "" {
			lines = append(lines, port+"archives not compared: "+found.Comparison.Problem)
		}
		for _, change := range found.Comparison.Changes {
			lines = append(lines, port+change.Message)
		}
		if words := CoverageWords(found.Comparison); words != "" {
			lines = append(lines, port+words)
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
		return "Could not determine dependents: " + r.DependentsUnread + "."
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
	return fmt.Sprintf("%s from the index at %s: %s. Review these candidates.", prose.Plural(len(r.Dependents), "dependent"), model.ObjectID(r.Base).Short(), words)
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
			Findings: append(reviewFindings(report.Findings), report.Carried...), Posted: posted, At: e.now()}); err != nil {
			return err
		}
		message := fmt.Sprintf("reviewed #%d at %s: %s", report.Ref.Number, model.ObjectID(report.Head).Short(), prose.Plural(len(report.Findings), "finding"))
		if posted != "" {
			message += ", posted as " + strings.ReplaceAll(posted, "-", " ")
		}
		_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: model.EventReview, Level: model.LevelInfo, Message: message})
		return err
	})
}

// PostReview posts a review's text, and its Portfile comments, on the pull
// request at the commit it reviewed, and records it.
func (e *Engine) PostReview(ctx context.Context, report ReviewReport, body string, requestChanges bool) (string, error) {
	if requestChanges && !report.CanRequestChanges() {
		return "", fmt.Errorf("requesting changes is left to people with write or triage access to %s; you have %s, so post it as a comment", report.Ref.Repository, Access(report.Permission))
	}
	url, err := remotewrite.Review(ctx, e.pullRequests(), remotewrite.Named, forge.ReviewInput{Ref: report.Ref, Commit: report.Head, RequestChanges: requestChanges, Body: body, Comments: report.Comments()})
	if err != nil {
		return "", fmt.Errorf("posting the review on #%d: %w", report.Ref.Number, err)
	}
	posted := "comment"
	if requestChanges {
		posted = "request-changes"
	}
	return url, e.RecordReview(ctx, report, posted)
}
