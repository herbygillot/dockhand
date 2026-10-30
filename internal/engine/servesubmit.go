package engine

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ServeCandidate is a branch serve prepared whose check passed, and what
// submitting it would do, or why it is held for a person's look (Design
// v3 §11's guardrails).
type ServeCandidate struct {
	Branch model.Branch
	Plan   SubmitPlan
	// Held are the concerns it waits for a person over; empty when serve
	// may submit it.
	Held []model.Concern
}

// Passing is the open branches with checked work not yet on a pull
// request, sorted by whether it may be submitted as passing.
type Passing struct {
	// Ready are those whose latest check passed for exactly their
	// committed files, with nothing edited since.
	Ready []BranchStatus
	// Others counts those whose latest check didn't pass, or whose files
	// have changed since it ran.
	Others int
}

// PassingBranches are the branches that passed and are ready to submit,
// for ship --passing and for serve, by branchstate's one definition
// (SubmitReadiness).
func (e *Engine) PassingBranches(ctx context.Context) (Passing, error) {
	// A branch no check has finished for can't be passing, and the store
	// says which those are before any git read: serve's submit pass read
	// every open branch's status each five minutes, an idle serve's 148
	// CPU-minutes in 16 hours over 838 branches (rc6's B7).
	checked := map[model.BranchID]bool{}
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{States: model.Finished, NoBaselines: true})
		for _, run := range runs {
			checked[run.Branch] = true
		}
		return err
	}); err != nil {
		return Passing{}, err
	}
	statuses, err := e.statusWhere(ctx, []model.BranchState{model.BranchOpen}, func(branch model.Branch) bool { return checked[branch.ID] })
	if err != nil {
		return Passing{}, err
	}
	var passing Passing
	for _, status := range statuses {
		switch branchstate.SubmitReadiness(status.Facts()) {
		case branchstate.SubmitReady:
			passing.Ready = append(passing.Ready, status)
		case branchstate.NotReady:
			passing.Others++
		}
	}
	return passing, nil
}

// ServeNote closes the description of a pull request serve opens by
// itself.
const ServeNote = "Opened by `dockhand serve` for an update it prepared and checked, without a person's review."

// serveCandidates are the passing branches (PassingBranches) serve itself
// started, with no pull request yet, each planned as submit would plan it
// and held when anything asks for a person (SubmitPlan.Held); and the
// passing branches a person started, which serve leaves to them: update
// --outdated --check's seven passing branches waited, unsaid (field
// testing's seventh report).
func (e *Engine) serveCandidates(ctx context.Context) ([]ServeCandidate, []model.Branch, error) {
	passing, err := e.PassingBranches(ctx)
	if err != nil {
		return nil, nil, err
	}
	var candidates []ServeCandidate
	var yours []model.Branch
	for _, status := range passing.Ready {
		branch := status.Branch
		if branch.PullRequest != nil {
			continue
		}
		// A serve branch a person checked where they named is theirs to
		// submit: serve submits what passed where build.unattended builds
		// (D17).
		if branch.Origin != model.OriginServe || e.personAsked(status) {
			yours = append(yours, branch)
			continue
		}
		plan, err := e.PlanSubmit(ctx, SubmitRequest{Branch: branch})
		candidate := ServeCandidate{Branch: branch, Plan: plan}
		if err != nil {
			candidate.Held = append(candidate.Held, model.Concern{Origin: model.FromSubmission, Rule: "unplanned", Detail: err.Error()})
			candidates = append(candidates, candidate)
			continue
		}
		candidate.Held = plan.Held()
		candidates = append(candidates, candidate)
	}
	return candidates, yours, nil
}

// personAsked reports whether a branch's governing check was asked, by a
// person, to build somewhere other than build.unattended's profile: --on,
// or another --build. A check narrowed after it doesn't hand the branch
// back to serve (evidence.Ask).
func (e *Engine) personAsked(status BranchStatus) bool {
	if status.Evidence == nil || status.Evidence.Ask == nil {
		return false
	}
	ask := status.Evidence.Ask
	return ask.Asked && ask.Profile != e.unattendedProfile()
}

// Held are the concerns a submission no person looked over waits for one
// over (Design v3 §11's guardrails): what blocks it, and its concerns, as
// dockhand bump's and serve's alike, for a branch of any origin. It was
// Engine.Held, with a context it ignored and an error always nil (the
// architecture review of 5897a82, R3-10's signature).
func (p SubmitPlan) Held() []model.Concern {
	var held []model.Concern
	for _, blocking := range p.Blocking {
		held = append(held, model.Concern{Origin: model.FromSubmission, Rule: "blocking", Detail: blocking})
	}
	return append(held, undecided(holding(p.Concerns(), p.allowed), p.Decided)...)
}

// HoldLines are concerns as they're said, a line each, the same finding of
// several ports once: two subports' "setup.py changed" read twice in a
// submission's held line (field testing at f41fbad6, py-kubernetes).
func HoldLines(concerns []model.Concern) []string {
	var lines []string
	said := map[string]bool{}
	for _, concern := range concerns {
		if key := concern.Unported(); !said[key] {
			said[key] = true
			lines = append(lines, concern.Detail)
		}
	}
	return lines
}

// HoldRuleLines are HoldLines, each naming the rule that raised it, as
// decide and holds.allow name it: "upstream's COPYING changed
// [license-changed]". A held line said neither what it stopped nor what
// let it go (the abook session, 2026-10-28).
func HoldRuleLines(concerns []model.Concern) []string {
	var lines []string
	said := map[string]bool{}
	for _, concern := range concerns {
		if key := concern.Unported(); !said[key] {
			said[key] = true
			lines = append(lines, WithRule(concern.Detail, concern.Rule))
		}
	}
	return lines
}

// WithRule is a held line with the rule that raised it, in brackets as a
// commit rule's finding ends with its code; as it is where none is named.
func WithRule(line, rule string) string {
	if rule == "" {
		return line
	}
	return line + " [" + rule + "]"
}

// HoldsLetGo says once, after the held lines, what they stop and what
// lets them go.
const HoldsLetGo = `serve and ship --yes wait on these, and your own submit goes ahead; dockhand decide <rule> -p <port> --because "…" answers one for this branch, and holds.allow lets a rule go for every branch`

// Concerns are what a submission would wait on a person's look for, were
// nobody to look it over (the assessment design, E), each once: what its
// assessments found that a passing build can't catch, or couldn't check
// (D4); a Git-fetched source whose tag moved between its update, its
// check, and now (Moved); a commit-rule error; and another open pull
// request for its ports, or not knowing whether there is one. A person's own submission
// shows them and goes ahead, and each kind of submission keeps its own
// rules over its evidence (§3's publication rule).
func (p SubmitPlan) Concerns() []model.Concern {
	var concerns []model.Concern
	add := func(concern model.Concern) {
		if !slices.ContainsFunc(concerns, func(c model.Concern) bool { return c.Key() == concern.Key() }) {
			concerns = append(concerns, concern)
		}
	}
	for _, found := range p.Upstream {
		for _, concern := range found.Comparison.Concerns(found.Port) {
			add(concern)
		}
	}
	for _, moved := range p.Moved {
		add(moved)
	}
	for _, major := range p.Majors {
		add(major)
	}
	for _, floor := range p.Floors {
		add(floor)
	}
	for _, after := range p.After {
		add(after)
	}
	for _, link := range p.Undeclared {
		add(link)
	}
	// A commit-rule error holds, as tidy rewrites the commits it's in; a
	// warning is said and holds nothing, as tidy keeps the commits it's
	// in: serve held a branch for a body line over 72 characters that tidy
	// called nothing to tidy (the direction synthesis's first contradiction).
	for _, finding := range p.Findings {
		if finding.Severity == commitrules.Error {
			add(model.Concern{Origin: model.FromCommitRules, Rule: finding.Code, Detail: "commit rules: " + finding.String()})
		}
	}
	for _, concern := range othersConcerns(p.Others, p.SearchProblem) {
		add(concern)
	}
	return concerns
}

// movedSources are the Git-fetched ports whose git.branch names another
// commit now than when the check a submission rests on planned it: the
// build proved one source, and the pull request would ship another, since
// a tag binds nothing (the assessment design, A). A target --only left out
// is among them: the plan resolved its tag too, and an earlier check's
// result of it stands only where it built that commit (Counts). Each
// repository and ref is read once; one that can't be read now isn't said
// to have moved, as the check's own evidence says what it built.
//
// So are the ports fetched as a GitHub tag's archive whose tag names
// another commit now (D-N5, tag-moved): the archive the pull request's
// checksums describe may not be the one fetched now, a stealth update.
// Their tags are read with git ls-remote, as the check read them.
func (e *Engine) movedSources(ctx context.Context, evidence *Evidence) []model.Concern {
	if evidence == nil {
		return nil
	}
	now := map[[2]string]model.GitSource{}
	var concerns []model.Concern
	for _, p := range plannedSources(evidence.Plan) {
		target, planned := p.target, p.source
		key := [2]string{planned.URL, planned.Ref}
		if _, read := now[key]; !read {
			now[key] = e.resolveGit(ctx, model.GitSource{URL: planned.URL, Ref: planned.Ref})
		}
		current := now[key]
		// A current commit that couldn't be read leaves the source's
		// freshness unknown, which holds serve, as an archive that
		// couldn't be compared does, and a person's submission says it
		// and goes on (the person's word, 2026-10-23; Codex's review of
		// 8c007979, C8-5's gap: it was skipped, and serve submitted).
		if planned.Commit != "" && current.Commit == "" && current.Unresolved != "" {
			concerns = append(concerns, unknownSource(target.Target.Name, "git.branch "+planned.Ref, planned.Commit, current.Unresolved, evidence.Run.Name()))
			continue
		}
		if planned.Commit == "" || current.Commit == "" || current.Commit == planned.Commit {
			continue
		}
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: model.RuleSourceMoved, Subject: model.MovedSubject(planned.Commit, current.Commit), Class: model.Introduced,
			Detail: fmt.Sprintf("%s: git.branch %s pointed to %s when %s was planned, but now points to %s. The checked source differs from the source this submission would use", target.Target.Name, planned.Ref, planned.Commit.Short(), evidence.Run.Name(), current.Commit.Short())})
	}
	tags := map[[2]string]model.GitSource{}
	for _, p := range plannedTags(evidence.Plan) {
		target, planned := p.target, p.source
		key := [2]string{planned.URL, planned.Ref}
		if _, read := tags[key]; !read {
			tags[key] = e.resolveTag(ctx, model.GitSource{URL: planned.URL, Ref: planned.Ref})
		}
		current := tags[key]
		if planned.Commit != "" && current.Commit == "" && current.Unresolved != "" && !git.ValidAbbreviation(planned.Ref) {
			concerns = append(concerns, unknownSource(target.Target.Name, "tag "+planned.Ref, planned.Commit, current.Unresolved, evidence.Run.Name()))
			continue
		}
		if planned.Commit == "" || current.Commit == "" || current.Commit == planned.Commit {
			continue
		}
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: model.RuleTagMoved, Subject: model.MovedSubject(planned.Commit, current.Commit), Class: model.Introduced,
			Detail: tagMovedWords(planned, current, evidence.Run.Name(), target.Target.Name)})
	}
	return concerns
}

// sourceUnknownRule is a source whose current commit couldn't be read.
// It's no rule holds.allow may name: what couldn't be checked holds (D4).
const sourceUnknownRule = "source-unknown"

// unknownSource is the concern of a source whose current commit couldn't
// be read, so whether the checked source is the one a submission would use
// isn't known.
func unknownSource(port, ref string, planned model.ObjectID, why, run string) model.Concern {
	return model.Concern{Origin: model.FromUpstream, Port: port, Rule: sourceUnknownRule, Subject: string(planned), Class: model.UnknownBaseline,
		Detail: fmt.Sprintf("%s: whether %s still points to %s, as when %s was planned, isn't known: %s", port, ref, planned.Short(), run, why)}
}

// preparedSources are the Git-fetched ports whose update chose a release
// whose tag named another commit then than when the check a submission
// rests on planned it: the update prepared one source, and the check
// built another, since a tag binds nothing (the assessment design, A). An
// update is matched to a planned target by its port and by the Git source
// its release names (model.Release.Names); the newest such edit is the one
// the branch's files carry. A release or plan that names no commit says
// nothing of whether the tag moved.
func preparedSources(evidence *Evidence, edits []model.Edit) []model.Concern {
	if evidence == nil {
		return nil
	}
	var concerns []model.Concern
	for _, p := range plannedSources(evidence.Plan) {
		target, planned := p.target, p.source
		var chosen *model.Release
		for _, edit := range edits {
			if edit.Port == target.Target.Name && edit.Release != nil && edit.Release.Names(planned) {
				chosen = edit.Release
			}
		}
		if chosen == nil || chosen.Commit == "" || planned.Commit == "" || model.ObjectID(chosen.Commit) == planned.Commit {
			continue
		}
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: model.RuleReleaseMoved, Subject: model.MovedSubject(model.ObjectID(chosen.Commit), planned.Commit), Class: model.Introduced,
			Detail: fmt.Sprintf("%s: git.branch %s pointed to %s when the update was prepared, then to %s when %s was planned. The check used a different source from the update", target.Target.Name, planned.Ref, model.ObjectID(chosen.Commit).Short(), planned.Commit.Short(), evidence.Run.Name())})
	}
	return concerns
}

// assessedSources are the Git-fetched ports whose assessment read another
// commit than the check a submission rests on planned: what upstream's
// change means was judged of one source, and the check built another.
// release-moved says it of an update's release; a hand edit records no
// release, and nothing said it (batch 22's leftover, batch 32). An
// assessment that kept no commit says nothing.
func assessedSources(evidence *Evidence, upstream []PortComparison) []model.Concern {
	if evidence == nil {
		return nil
	}
	var concerns []model.Concern
	for _, p := range plannedSources(evidence.Plan) {
		target, planned := p.target, p.source
		at := slices.IndexFunc(upstream, func(found PortComparison) bool { return found.Port == target.Target.Name })
		if at < 0 || planned.Commit == "" {
			continue
		}
		read := upstream[at].Comparison.Commit
		if read == "" || model.ObjectID(read) == planned.Commit {
			continue
		}
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: model.RuleAssessmentMoved, Subject: model.MovedSubject(model.ObjectID(read), planned.Commit), Class: model.Introduced,
			Detail: fmt.Sprintf("%s: the upstream assessment examined %s, but %s planned to build %s. The assessment and check refer to different source commits", target.Target.Name, model.ObjectID(read).Short(), evidence.Run.Name(), planned.Commit.Short())})
	}
	return concerns
}

// plannedSource is a Git-fetched target of a plan, and the source the plan
// expects it to fetch.
type plannedSource struct {
	target model.PlanTarget
	source model.GitSource
}

// plannedSources are the sources a plan expects its Git-fetched targets to
// fetch, in the plan's order, with those --only left out after: each
// target's once for each repository and ref its environments evaluate,
// since one resolution serves every environment (resolveGitSources).
func plannedSources(plan model.Plan) []plannedSource {
	return plannedBy(plan, func(build model.EnvironmentPlan) map[model.TargetID]model.GitSource { return build.Git })
}

// plannedTags are the GitHub tags a plan's targets fetched as an archive
// are fetched by, with the commit each named as the check was planned
// (EnvironmentPlan.Tags), as plannedSources has them.
func plannedTags(plan model.Plan) []plannedSource {
	return plannedBy(plan, func(build model.EnvironmentPlan) map[model.TargetID]model.GitSource { return build.Tags })
}

// plannedBy are a plan's targets with the sources one of its environment
// plans' maps (of) has for them, each target's once for each repository
// and ref.
func plannedBy(plan model.Plan, of func(model.EnvironmentPlan) map[model.TargetID]model.GitSource) []plannedSource {
	var planned []plannedSource
	for _, target := range slices.Concat(plan.Targets, plan.Omitted) {
		for _, build := range plan.Builds {
			source, ok := of(build)[target.ID]
			if ok && !slices.ContainsFunc(planned, func(p plannedSource) bool {
				return p.target.ID == target.ID && p.source.URL == source.URL && p.source.Ref == source.Ref
			}) {
				planned = append(planned, plannedSource{target: target, source: source})
			}
		}
	}
	return planned
}

// othersConcerns are what other open pull requests for a port ask of a
// submission no person looked over: each one found, or not knowing
// whether there is one. Bump's search before its edit and a submission's
// gate say them in one voice.
func othersConcerns(others []forge.PullRequestSummary, problem string) []model.Concern {
	var concerns []model.Concern
	for _, pr := range others {
		concerns = append(concerns, model.Concern{Origin: model.FromOtherPullRequests, Rule: "open", Subject: fmt.Sprint(pr.Number),
			Detail: fmt.Sprintf("#%d is open for the same port: %s", pr.Number, pr.Title)})
	}
	if problem != "" {
		concerns = append(concerns, model.Concern{Origin: model.FromOtherPullRequests, Rule: "search-failed", Detail: "couldn't look for other open pull requests: " + problem})
	}
	return concerns
}

// HeldBeforeEdit is an update no person looks over, bump's, held for a
// look before its edit was prepared (UpdateRequest.Unattended), for what
// would hold its submission after the check: another open pull request
// for the port, or not knowing whether there is one. Held says why, as a
// held submission does.
type HeldBeforeEdit struct {
	Port string
	Held []model.Concern
}

func (h *HeldBeforeEdit) Error() string {
	return fmt.Sprintf("%s needs review before Dockhand can prepare its update: %s", h.Port, strings.Join(HoldLines(h.Held), "; "))
}

// heldBeforeEdit looks for the port's other open pull requests before an
// unattended update prepares its edit. One found, or not knowing, would
// hold the submission once the check passed, so it holds the update now,
// before anything is downloaded or built.
func (e *Engine) heldBeforeEdit(ctx context.Context, port string, except int) error {
	held := holding(othersConcerns(e.openPullRequests(ctx, []string{port}, except)), e.allowedFor)
	if len(held) > 0 {
		return &HeldBeforeEdit{Port: port, Held: held}
	}
	return nil
}

// assessedHolds are why the recorded assessments of a branch's files hold
// it for a person's look (UpstreamComparison.Holds), and how far they're
// recorded: status reads them, and collects nothing.
func (e *Engine) assessedHolds(ctx context.Context, branch model.Branch, tree model.ObjectID, directories []string) ([]model.Concern, []PortComparison, AssessmentState, error) {
	assessments, err := e.revisionAssessments(ctx, branch.ID, branch.Base, tree, false)
	if err != nil {
		return nil, nil, "", err
	}
	slices.SortFunc(assessments, func(a, b model.Assessment) int { return macports.ComparePortNames(a.Port, b.Port) })
	var held []model.Concern
	var comparisons []PortComparison
	state := AssessmentAvailable
	for _, a := range assessments {
		held = append(held, a.Comparison.Concerns(a.Port)...)
		if !SourceUnchanged(a.Comparison) {
			comparisons = append(comparisons, PortComparison{Port: a.Port, Comparison: a.Comparison})
		}
		if a.Comparison.Problem != "" {
			state = AssessmentIncomplete
		}
	}
	for _, directory := range directories {
		if !slices.ContainsFunc(assessments, func(a model.Assessment) bool { return a.Directory == directory }) {
			state = AssessmentPending
		}
	}
	return holding(held, e.allowedFor), comparisons, state, nil
}

// AssessmentState is how far a branch's files are assessed, as status
// reads what's recorded: every port's assessment recorded, one of them
// incomplete, or some not yet recorded, which a check or a submission
// collects.
type AssessmentState string

const (
	AssessmentAvailable  AssessmentState = "available"
	AssessmentIncomplete AssessmentState = "incomplete"
	AssessmentPending    AssessmentState = "pending"
)

// SubmitForServe opens the pull request for a candidate serve may submit,
// saying in its description that no person reviewed it.
func (e *Engine) SubmitForServe(ctx context.Context, candidate ServeCandidate) (Submitted, error) {
	if len(candidate.Held) > 0 {
		return Submitted{}, fmt.Errorf("%s needs review before submission: %s", candidate.Branch.ShortName(), strings.Join(HoldLines(candidate.Held), "; "))
	}
	plan := candidate.Plan
	plan.Body = plan.Body + "\n\n" + ServeNote + "\n"
	submitted, err := e.ApplySubmit(ctx, plan, model.AnsweredNobody)
	if err != nil {
		return submitted, err
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: candidate.Branch.ID, Kind: model.EventServeSubmit, Level: model.LevelInfo,
			Message: fmt.Sprintf("serve opened #%d for %s, which passed its check", submitted.PullRequest.Ref.Number, candidate.Branch.ShortName())})
		return err
	})
	return submitted, err
}

// majorRule is the hold on an update whose major version moves.
const majorRule = "major-version"

// afterRule is the hold of a port whose holds name another to submit first
// (holds.ports."<port>".after).
const afterRule = "after"

// HoldRules are the rules whose concerns holds.allow may name, each said
// and holding nothing then: every assessment rule, every commit rule whose
// finding is an error, and the holds the engine raises itself. What blocks
// a submission, and archives that couldn't be compared, hold whatever the
// list says, as what couldn't be checked does (D4).
func HoldRules() []string {
	rules := slices.Concat(assess.Rules(), commitrules.ErrorCodes(), []string{majorRule, darwinFloorRule, deploymentTargetRule,
		model.RuleSourceMoved, model.RuleTagMoved, model.RuleReleaseMoved, model.RuleAssessmentMoved, "open", "search-failed",
		upstreamMovedRule, upstreamRenamedPortRule, upstreamTransferredRule, mirrorConflictRule, manifestHookRule, afterRule, linkUndeclaredRule})
	slices.Sort(rules)
	return slices.Compact(rules)
}

// holding are the concerns that hold, those the configuration's hold list
// allows for their port set apart (holds.allow, or the port's own), which
// are still said.
func holding(concerns []model.Concern, allowed func(port string) []string) []model.Concern {
	return slices.DeleteFunc(slices.Clone(concerns), func(c model.Concern) bool {
		return c.Origin != model.FromSubmission && c.Rule != "not-compared" && allowed != nil && slices.Contains(allowed(c.Port), c.Rule)
	})
}

// PortHolds are one port's own holds, as the configuration gives them
// ([holds.ports."<port>"]): AllowMajors and Allow, where set, over the
// global ones; FollowMoves, --follow-moves standing for an unattended
// update of it; After, the ports whose update to the same version is
// submitted before its own.
type PortHolds struct {
	AllowMajors *bool
	Allow       []string
	FollowMoves bool
	After       []string
}

// heldAs is what holds an update of a port nobody looks over, the global
// holds with the port's own over them: composed here alone (Then's item
// 2).
type heldAs struct {
	allowMajors bool
	allow       []string
	followMoves bool
	after       []string
}

// holdsFor composes a port's holds.
func (e *Engine) holdsFor(port string) heldAs {
	held := heldAs{allowMajors: e.options.AllowMajors, allow: e.options.AllowHolds}
	own, ok := e.options.PortHolds[port]
	if !ok {
		return held
	}
	if own.AllowMajors != nil {
		held.allowMajors = *own.AllowMajors
	}
	if own.Allow != nil {
		held.allow = own.Allow
	}
	held.followMoves, held.after = own.FollowMoves, own.After
	return held
}

// allowedFor are the hold rules a port's update goes on past.
func (e *Engine) allowedFor(port string) []string { return e.holdsFor(port).allow }

// The holds on a revision that raises the oldest macOS a port builds on:
// its platforms' darwin floor, or its macosx_deployment_target. Older Macs
// lose it, which a person decides (part 2's new holds, the person's order
// of 2026-10-13); read from the change record, not a Portfile pattern.
const (
	darwinFloorRule      = "darwin-floor"
	deploymentTargetRule = "deployment-target"
)

// floorHolds are a revision's raised floors, as its change records say:
// none where they can't be read, as the records then say nothing.
func (e *Engine) floorHolds(ctx context.Context, branch model.BranchID, base, tree model.ObjectID) []model.Concern {
	records, err := e.revisionChanges(ctx, branch, base, tree, false)
	if err != nil {
		return nil
	}
	return floorConcerns(records)
}

// floorConcerns are the floors change records raise: a port's Darwin
// floor from its platforms, or its macosx_deployment_target, raised.
func floorConcerns(records map[string]model.ChangeRecord) []model.Concern {
	directories := slices.Sorted(maps.Keys(records))
	var held []model.Concern
	for _, directory := range directories {
		for _, port := range records[directory].Ports {
			for _, field := range port.Fields {
				switch field.Field {
				case "platforms":
					from, to := macports.DarwinFloor(field.From), macports.DarwinFloor(field.To)
					if to > from {
						held = append(held, model.Concern{Origin: model.FromHolds, Port: port.Port, Rule: darwinFloorRule, Subject: strconv.Itoa(to),
							Detail: fmt.Sprintf("%s now requires Darwin %d or later; platforms was %q. This drops support for older Macs and requires review before automatic submission", port.Port, to, field.From)})
					}
				case "macosx_deployment_target":
					// Both sides evaluated on this Mac, where Base sets it to
					// the host's unless the Portfile does: one raised is the
					// Portfile's.
					if field.From != "" && field.To != "" && !macports.BaseAtLeast(field.From, field.To) {
						held = append(held, model.Concern{Origin: model.FromHolds, Port: port.Port, Rule: deploymentTargetRule, Subject: field.To,
							Detail: fmt.Sprintf("%s raises macosx_deployment_target from %s to %s. Built binaries may no longer run on older Macs; review is required before automatic submission", port.Port, field.From, field.To)})
					}
				}
			}
		}
	}
	return held
}

// linkUndeclaredRule is the hold of a build that links a library from a
// port its Portfile doesn't declare (the person's "Hold it", 2026-10-27
// 12:53Z): rustup linked liblzma with no depends_lib on xz.
const linkUndeclaredRule = "link-undeclared"

// linkHolds are the undeclared links of the check a submission rests on
// (UndeclaredLinks), each a hold for the port that links it, naming the
// port it links; a declared link, or a build whose links weren't read,
// holds nothing, as the scan says them.
func (e *Engine) linkHolds(ctx context.Context, checked *Evidence) []model.Concern {
	if checked == nil {
		return nil
	}
	findings, err := e.linkFindings(ctx, *checked)
	if err != nil {
		return nil
	}
	var held []model.Concern
	for _, finding := range findings {
		if finding.Unread || finding.Clean || finding.Port == "" {
			continue
		}
		port := string(finding.Target)
		if i := slices.IndexFunc(checked.Targets, func(t TargetEvidence) bool { return t.Target.ID == finding.Target }); i >= 0 {
			port = checked.Targets[i].Target.Target.Name
		}
		held = append(held, model.Concern{Origin: model.FromHolds, Port: port, Rule: linkUndeclaredRule, Subject: finding.Port,
			Detail: fmt.Sprintf("%s links %s from %s, which it doesn't declare in depends_lib; review is required before automatic submission", port, path.Base(finding.Library), finding.Port)})
	}
	return held
}

// majorHold is the hold on an update nobody looks over whose major version
// moves, unless holds.majors allows it: hyperfine went from 1.21.0 to 2.0.0
// and bump opened #35353 without a pause (field testing at b8baa26b). It's
// said before the edit, so nothing is downloaded or built; nil where it
// doesn't hold.
func (e *Engine) majorHold(port, from, to string) *model.Concern {
	held := e.holdsFor(port)
	if held.allowMajors || slices.Contains(held.allow, majorRule) || from == "" || !version.CrossesMajor(from, to) {
		return nil
	}
	return &model.Concern{Origin: model.FromHolds, Port: port, Rule: majorRule, Subject: to,
		Detail: fmt.Sprintf("%s moves from %s to %s, a major version update. holds.majors requires review before an unattended update", port, from, to)}
}

// HoldsMajor reports whether an update of a port nobody looks over from
// one version to another is held for its major version (majorHold), for
// outdated to mark the updates it would hold.
func (e *Engine) HoldsMajor(port, from, to string) bool { return e.majorHold(port, from, to) != nil }

// majorHolds are the major-version holds of a branch's updates, from what
// each edit's release moved from and to, for a submission nobody looks
// over: serve prepares an update whose check then passes.
func (e *Engine) majorHolds(edits []model.Edit) []model.Concern {
	var held []model.Concern
	for _, edit := range edits {
		if edit.Kind != model.EditUpdate || edit.Release == nil {
			continue
		}
		if major := e.majorHold(edit.Port, edit.Release.CurrentVersion, edit.Release.Version); major != nil && !slices.ContainsFunc(held, func(c model.Concern) bool { return c.Key() == major.Key() }) {
			held = append(held, *major)
		}
	}
	return held
}

// LeavesStableWords say an update that takes a port from a stable version
// to a prerelease, as update and submit's preview say it, once each (the
// docs dig's I32): automatic selection never chooses such a version, so
// only one the person named does, and it holds nothing.
func LeavesStableWords(port, from, to string) string {
	return fmt.Sprintf("this takes %s out of stable (%s → %s)", port, from, to)
}

// prereleases are the LeavesStableWords of a branch's updates that take a
// port from a stable version to a prerelease, once each.
func prereleases(edits []model.Edit) []string {
	var words []string
	for _, edit := range edits {
		if edit.Kind != model.EditUpdate || edit.Release == nil || !version.LeavesStable(edit.Release.CurrentVersion, edit.Release.Version) {
			continue
		}
		if said := LeavesStableWords(edit.Port, edit.Release.CurrentVersion, edit.Release.Version); !slices.Contains(words, said) {
			words = append(words, said)
		}
	}
	return words
}

// afterHolds hold the update of a port whose holds name ports to submit
// first, holds.ports."<port>".after, until each named port is at the same
// version on master, as dockhand last fetched it, or has a pull request
// open at it: harfbuzz is updated after harfbuzz-devel (Then's item 2).
// Not knowing holds, as not knowing of another pull request does.
func (e *Engine) afterHolds(ctx context.Context, edits []model.Edit) []model.Concern {
	var held []model.Concern
	for _, edit := range edits {
		if edit.Kind != model.EditUpdate || edit.Release == nil {
			continue
		}
		for _, first := range e.holdsFor(edit.Port).after {
			why, ok := e.submittedFirst(ctx, first, edit.Release.Version)
			if ok {
				continue
			}
			concern := model.Concern{Origin: model.FromHolds, Port: edit.Port, Rule: afterRule, Subject: first,
				Detail: fmt.Sprintf("%s waits for %s's update to %s, as its holds say (holds.ports.%q.after): %s", edit.Port, first, edit.Release.Version, edit.Port, why)}
			if !slices.ContainsFunc(held, func(c model.Concern) bool { return c.Key() == concern.Key() }) {
				held = append(held, concern)
			}
		}
	}
	return held
}

// submittedFirst reports whether port is at version on the master dockhand
// last fetched, or has a pull request open at it; where not, why.
func (e *Engine) submittedFirst(ctx context.Context, port, version string) (string, bool) {
	at := ""
	if master, ok := e.lastMaster(ctx); ok {
		if directory, ok := e.PortDirectory(ctx, port); ok {
			if _, text, err := e.Repo.File(ctx, string(master), directory+"/Portfile"); err == nil {
				subport := ""
				if path.Base(directory) != port {
					subport = port
				}
				if declared, ok := portfile.DeclaredVersion(text, subport); ok {
					if declared == version {
						return "", true
					}
					at = fmt.Sprintf("%s is at %s on master", port, declared)
				}
			}
		}
	}
	// Searched afresh, as submit's look is: the port named first may have
	// been submitted moments ago, in the same pass.
	open, problem := e.pullRequestsNow(ctx, []string{port}, 0)
	if problem != "" {
		return fmt.Sprintf("its pull requests couldn't be read: %s", problem), false
	}
	for _, pr := range open {
		if slices.Contains(strings.Fields(pr.Title), version) {
			return "", true
		}
	}
	why := "with no pull request open at " + version
	if at != "" {
		why = at + ", " + why
	}
	return why, false
}
