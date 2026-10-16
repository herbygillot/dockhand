package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
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

// PassingBranches is the one definition of a branch that passed and is
// ready to submit, for submit --passing and for serve: it has commits and
// a finished check, its pull request doesn't already have its head, and
// the checks of exactly its committed files built every changed target
// and passed, whatever --only narrowed.
func (e *Engine) PassingBranches(ctx context.Context) (Passing, error) {
	statuses, err := e.Status(ctx)
	if err != nil {
		return Passing{}, err
	}
	var passing Passing
	for _, status := range statuses {
		switch {
		case status.Missing || status.Commits == 0 || status.Latest == nil || status.Pushed():
		case status.CheckStanding() == CheckPassed && len(status.Edited) == 0 && status.Evidence != nil:
			passing.Ready = append(passing.Ready, status)
		default:
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
// and held when anything asks for a person (SubmitPlan.held); and the
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
		if branch.Origin != model.OriginServe {
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
		candidate.Held = plan.held()
		candidates = append(candidates, candidate)
	}
	return candidates, yours, nil
}

// Held are the reasons a submission no person looked over waits for one,
// as dockhand bump's does (SubmitPlan.held), for a branch of any origin.
func (e *Engine) Held(_ context.Context, plan SubmitPlan) ([]model.Concern, error) {
	return plan.held(), nil
}

// held are the concerns a submission no person looked over waits for one
// over (Design v3 §11's guardrails): what blocks it, and its concerns.
func (p SubmitPlan) held() []model.Concern {
	var held []model.Concern
	for _, blocking := range p.Blocking {
		held = append(held, model.Concern{Origin: model.FromSubmission, Rule: "blocking", Detail: blocking})
	}
	return append(held, p.Concerns()...)
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

// Concerns are what a submission would wait on a person's look for, were
// nobody to look it over (the assessment design, E), each once: what its
// assessments found that a passing build can't catch, or couldn't check
// (D4); a Git-fetched source whose tag moved between its update, its
// check, and now (Moved); a commit-rule finding; and another open pull
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
	for _, finding := range p.Findings {
		add(model.Concern{Origin: model.FromCommitRules, Rule: finding.Code, Detail: "commit rules: " + finding.String()})
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
		if planned.Commit == "" || current.Commit == "" || current.Commit == planned.Commit {
			continue
		}
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: "source-moved", Subject: string(current.Commit), Class: model.Introduced,
			Detail: fmt.Sprintf("%s's git.branch %s named %s when %s planned it, and names %s now: the check built another source than this would submit", target.Target.Name, planned.Ref, planned.Commit.Short(), evidence.Run.Name(), current.Commit.Short())})
	}
	tags := map[[2]string]model.GitSource{}
	for _, p := range plannedTags(evidence.Plan) {
		target, planned := p.target, p.source
		key := [2]string{planned.URL, planned.Ref}
		if _, read := tags[key]; !read {
			tags[key] = e.resolveTag(ctx, model.GitSource{URL: planned.URL, Ref: planned.Ref})
		}
		current := tags[key]
		if planned.Commit == "" || current.Commit == "" || current.Commit == planned.Commit {
			continue
		}
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: "tag-moved", Subject: string(current.Commit), Class: model.Introduced,
			Detail: TagMovedWords(planned, current, evidence.Run.Name(), target.Target.Name)})
	}
	return concerns
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
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: "release-moved", Subject: string(planned.Commit), Class: model.Introduced,
			Detail: fmt.Sprintf("%s's git.branch %s named %s when its update chose it, and %s when %s planned it: the check built another source than the update chose", target.Target.Name, planned.Ref, model.ObjectID(chosen.Commit).Short(), planned.Commit.Short(), evidence.Run.Name())})
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
		concerns = append(concerns, model.Concern{Origin: model.FromUpstream, Port: target.Target.Name, Rule: "assessment-moved", Subject: string(planned.Commit), Class: model.Introduced,
			Detail: fmt.Sprintf("%s's assessment read %s, and %s planned %s: what upstream's change was judged of isn't the source the check built", target.Target.Name, model.ObjectID(read).Short(), evidence.Run.Name(), planned.Commit.Short())})
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
	return fmt.Sprintf("%s is held for a look before its edit: %s", h.Port, strings.Join(HoldLines(h.Held), "; "))
}

// heldBeforeEdit looks for the port's other open pull requests before an
// unattended update prepares its edit. One found, or not knowing, would
// hold the submission once the check passed, so it holds the update now,
// before anything is downloaded or built.
func (e *Engine) heldBeforeEdit(ctx context.Context, port string, except int) error {
	held := othersConcerns(e.openPullRequests(ctx, []string{port}, except))
	if len(held) > 0 {
		return &HeldBeforeEdit{Port: port, Held: held}
	}
	return nil
}

// assessedHolds are why the recorded assessments of a branch's files hold
// it for a person's look (UpstreamComparison.Holds), and how far they're
// recorded: status reads them, and collects nothing.
func (e *Engine) assessedHolds(ctx context.Context, branch model.Branch, tree model.ObjectID, directories []string) ([]model.Concern, AssessmentState, error) {
	assessments, err := e.revisionAssessments(ctx, branch.ID, branch.Base, tree, false)
	if err != nil {
		return nil, "", err
	}
	slices.SortFunc(assessments, func(a, b model.Assessment) int { return macports.ComparePortNames(a.Port, b.Port) })
	var held []model.Concern
	state := AssessmentAvailable
	for _, a := range assessments {
		held = append(held, a.Comparison.Concerns(a.Port)...)
		if a.Comparison.Problem != "" {
			state = AssessmentIncomplete
		}
	}
	for _, directory := range directories {
		if !slices.ContainsFunc(assessments, func(a model.Assessment) bool { return a.Directory == directory }) {
			state = AssessmentPending
		}
	}
	return held, state, nil
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
		return Submitted{}, fmt.Errorf("%s is held for a look: %s", candidate.Branch.ShortName(), HoldLines(candidate.Held)[0])
	}
	plan := candidate.Plan
	plan.Body = plan.Body + "\n\n" + ServeNote + "\n"
	submitted, err := e.ApplySubmit(ctx, plan)
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
							Detail: fmt.Sprintf("%s now builds on Darwin %d and later, where its platforms said %q: older Macs lose it, which holds for a look", port.Port, to, field.From)})
					}
				case "macosx_deployment_target":
					// Both sides evaluated on this Mac, where Base sets it to
					// the host's unless the Portfile does: one raised is the
					// Portfile's.
					if field.From != "" && field.To != "" && !macports.BaseAtLeast(field.From, field.To) {
						held = append(held, model.Concern{Origin: model.FromHolds, Port: port.Port, Rule: deploymentTargetRule, Subject: field.To,
							Detail: fmt.Sprintf("%s raises macosx_deployment_target from %s to %s: older Macs may not run what it builds, which holds for a look", port.Port, field.From, field.To)})
					}
				}
			}
		}
	}
	return held
}

// majorHold is the hold on an update nobody looks over whose major version
// moves, unless holds.majors allows it: hyperfine went from 1.21.0 to 2.0.0
// and bump opened #35353 without a pause (field testing at b8baa26b). It's
// said before the edit, so nothing is downloaded or built; nil where it
// doesn't hold.
func (e *Engine) majorHold(port, from, to string) *model.Concern {
	if e.options.AllowMajors || from == "" || !version.CrossesMajor(from, to) {
		return nil
	}
	return &model.Concern{Origin: model.FromHolds, Port: port, Rule: majorRule, Subject: to,
		Detail: fmt.Sprintf("%s moves from %s to %s, a new major version, which holds.majors holds for a look", port, from, to)}
}

// HoldsMajor reports whether an update nobody looks over from one version
// to another is held for its major version (majorHold), for outdated to
// mark the updates it would hold.
func (e *Engine) HoldsMajor(from, to string) bool { return e.majorHold("", from, to) != nil }

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
