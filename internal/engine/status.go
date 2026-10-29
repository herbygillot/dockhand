package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// BranchStatus is what status shows for one branch (Design v3 §10). Each
// field answers one question; nothing folds source, checks, and review
// into one "done".
type BranchStatus struct {
	Branch model.Branch
	// Accepted, AtBase, and Decisions are what publication reads beside a
	// failed check of the files as they are (branchstate.Check).
	Accepted  []string
	AtBase    map[string][]string
	Decisions []model.Decision
	// CI are its pull request's failed CI jobs at the head last observed,
	// as each was classified (classifyCI).
	CI []model.CIFailure
	// NetworkHeldAt is when the last submission plan of its files found
	// what in Held took the network to find; zero where none did.
	NetworkHeldAt time.Time
	// HandEdits are the branch's hand edits as its working files have
	// them (update --hand-edit), by directory.
	HandEdits []HandEdit
	// Missing is true when the Git branch is gone.
	Missing bool
	// WorktreeStays is a Git branch gone whose worktree dockhand made is
	// still there, which isn't cleaned.
	WorktreeStays bool
	// Renamed is the branch's name before a rename by hand that this
	// status followed (followRenames), said once.
	Renamed string
	// NoFork is true where no Git remote pushes to a GitHub repository
	// but MacPorts' own, so submit has nowhere to push yet: read from
	// the remotes' addresses alone, as status reads no network.
	NoFork bool
	// Pruned is true for an ended branch whose checks cleanup has
	// removed but its newest, kept without what it built (D6): Latest is
	// that check, and there's no Evidence.
	Pruned  bool
	Head    string
	Commits int
	// Edited lists tracked files changed and not committed.
	Edited []string
	Scope  Scope
	// Changes are the change records of the files as they are now, by
	// directory, where they've been made; status makes none.
	Changes map[string]model.ChangeRecord
	// Tree is the files as they are now: the working files when edited,
	// else the head's.
	Tree string
	// Latest is the check status judges by: the newest finished check of
	// the files as they are now, as submit credits one, else the newest
	// finished check. Active is every run queued or running, baselines
	// and runs asked to stop among them; Checking is the branch's check.
	Latest   *model.Run
	Active   []model.Run
	Evidence *Evidence
	// Current is true when Latest checked exactly the files as they are
	// now, whatever their history.
	Current bool
	// Included are the untracked files Latest's snapshot took in with
	// --include, which the files as they are now are compared with: a
	// check that passed with an untracked patch included read as of files
	// that had changed since, and asked for a check of the same files.
	Included []string
	// LatestRevision is what Latest checked.
	LatestRevision *model.Revision
	// Held are the findings of its files' recorded assessments that hold
	// a branch serve prepared for a person's look, and Assessment how far
	// they're recorded; status collects none.
	Held       []model.Concern
	Assessment AssessmentState
	// Decided are the person's decisions on its concerns that stand for
	// its files, each said by status, and Awaiting those waiting on
	// something, which block its submission (the person's voice).
	Decided, Awaiting []model.Decision
	// Wider is the check a passing attended host check didn't build that
	// the update asks for (wider).
	Wider *branchstate.Wider
	// PastMerge are a merged branch's commits past what its pull request
	// merged (Engine.PastMerge), counted.
	PastMerge int
	// Stopped is the active run recorded as running that no live process
	// drives, once JudgeStopped has looked; nil otherwise.
	Stopped *model.Run
	// Releases are the releases the branch's updates chose, each port's
	// latest, in the order the ports were first updated.
	Releases []PortRelease
	// Moved are the Git-fetched ports whose update chose a release whose
	// tag named another commit then than when Latest's check planned it
	// (preparedSources' release-moved), or whose recorded assessment read
	// another commit (assessedSources' assessment-moved), where Latest
	// checked the files as they are now, which hold a submission nobody
	// looks over, as submit says them, unless a decision answers them.
	// They're read from the store and the plan alone. Whether a
	// tag names another commit now than the
	// check planned (source-moved) takes the network, which status never
	// reads, and nothing records it: submit's plan isn't kept, serve says
	// its holds on its own terminal, and an assessment is made once for
	// its files.
	Moved []model.Concern
	// assessed are the recorded assessments' comparisons, which
	// assessment-moved reads.
	assessed []PortComparison
	// OnMaster is the master dockhand last fetched where it has every
	// change the branch's files make, so its work landed by another
	// route, as duckdb-cxx14's C++14 fix did while status still asked to
	// commit it for review (cleaning up duckdb-cxx14, finding 1); empty
	// otherwise, or for a branch that changes nothing.
	OnMaster model.ObjectID
}

// PortRelease is the release an update chose for a port.
type PortRelease struct {
	Port    string
	Release model.Release
}

// Stopped reports whether a run recorded as running has no live process
// driving it: the process running it ended without settling it, as a
// killed foreground check does. It is resumable: the next serve takes it
// up where it stopped, and so does dockhand wait; cancel ends it. Judging
// who is alive takes a session.
func Stopped(ctx context.Context, session *coord.Session, run model.Run) (bool, error) {
	if run.State != model.RunRunning {
		return false, nil
	}
	holder, err := session.Holder(ctx, RunResource(run.ID))
	return err == nil && holder == nil, err
}

// JudgeStopped marks the statuses whose active run has stopped, their
// holders read in one transaction (SQL-O4).
func (e *Engine) JudgeStopped(ctx context.Context, session *coord.Session, statuses []BranchStatus) error {
	var active []model.Run
	for _, status := range statuses {
		active = append(active, status.Active...)
	}
	if len(active) == 0 {
		return nil
	}
	var holders map[string]*model.Session
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		holders, err = session.Holders(r, runResources(active)...)
		return err
	}); err != nil {
		return err
	}
	for i := range statuses {
		for _, run := range statuses[i].Active {
			if run.State == model.RunRunning && holders[RunResource(run.ID)] == nil {
				statuses[i].Stopped = &run
			}
		}
	}
	return nil
}

// readFiles reads what status says of a branch's files: its head and
// commits, the files as they are now, the edits not yet committed, what
// they change, and their change records; Missing where its Git branch is
// gone. It returns the paths changed, and the worktree where there is one.
func (e *Engine) readFiles(ctx context.Context, status *BranchStatus, reads *statusReads) ([]string, *git.Repository, error) {
	branch := status.Branch
	if reads == nil {
		var err error
		if reads, err = e.readBranches(ctx, []model.Branch{branch}); err != nil {
			return nil, nil, err
		}
	}
	head, ok := reads.heads[branch.Name]
	if !ok {
		status.Missing = true
		status.WorktreeStays = branch.Managed && exists(branch.Worktree)
		return nil, nil, nil
	}
	status.Head = head
	var err error
	if status.Commits, err = reads.count(ctx, e, branch, head); err != nil {
		return nil, nil, err
	}
	status.Tree = reads.trees[head]
	// Status only reads: a worktree clean removed isn't checked out again
	// for it, so its files are the branch's committed ones.
	worktree := reads.worktree(e, branch)
	if worktree != nil {
		if status.Edited, err = worktree.TrackedChanges(ctx); err != nil {
			return nil, nil, err
		}
		if len(status.Edited) > 0 {
			if _, status.Tree, err = worktree.WorkingTree(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	analysis, err := e.analyze(ctx, branch.Base, model.ObjectID(status.Tree))
	if err != nil {
		return nil, nil, err
	}
	status.Scope = analysis.Scope
	status.Changes, err = e.revisionChanges(ctx, branch.ID, branch.Base, model.ObjectID(status.Tree), false)
	return analysis.Paths, worktree, err
}

// statusReads are what status reads of every branch's Git branch at once
// (Then's item 6): their heads by one for-each-ref, their commits' trees
// by one cat-file, the worktrees each is checked out in by one worktree
// list, and their commit counts by one for-each-ref a base, where the Git
// counts so; status ran some ten git processes a branch, 3,962 over
// dhtest's 683 on rc2.
type statusReads struct {
	heads     map[string]string
	trees     map[string]string
	checkouts map[string][]string
	// counts are each branch's commits beyond its base, by its name,
	// where for-each-ref counted them; a branch not here is counted alone.
	counts map[string]int
	// master is the master dockhand last fetched, whose tree is in trees;
	// empty where none was. fromMaster are what each open branch's head
	// tree changes from master's, by the tree, read for landedOnMaster.
	master     model.ObjectID
	fromMaster map[string][]string
}

// readBranches reads what status reads of the branches' Git branches.
func (e *Engine) readBranches(ctx context.Context, branches []model.Branch) (*statusReads, error) {
	reads := &statusReads{heads: map[string]string{}, counts: map[string]int{}}
	refs, err := e.Repo.ReadRefs(ctx, "refs/heads/")
	if err != nil {
		return nil, err
	}
	var commits []string
	seen := map[string]bool{}
	add := func(commit string) {
		if commit != "" && !seen[commit] {
			seen[commit] = true
			commits = append(commits, commit)
		}
	}
	bases := map[string][]string{}
	if master, ok := e.lastMaster(ctx); ok {
		reads.master = master
		add(string(master))
	}
	for _, branch := range branches {
		ref, ok := refs["refs/heads/"+branch.Name]
		if !ok || !ref.Exists {
			continue
		}
		reads.heads[branch.Name] = ref.Object
		add(ref.Object)
		add(string(branch.Base))
		bases[string(branch.Base)] = append(bases[string(branch.Base)], branch.Name)
	}
	if reads.trees, err = e.Repo.CommitTrees(ctx, commits); err != nil {
		return nil, err
	}
	if reads.checkouts, err = e.Repo.AllCheckouts(ctx); err != nil {
		return nil, err
	}
	// What each head changes from its base, and an open one's from
	// master, is one diff-tree for them all; the analyses are kept as
	// analyze keeps its own, by base and tree.
	var pairs [][2]string
	var analyzed []model.Branch
	var landed []string
	masterTree := reads.trees[string(reads.master)]
	for _, branch := range branches {
		head, ok := reads.heads[branch.Name]
		if !ok || reads.trees[string(branch.Base)] == "" || reads.trees[head] == "" {
			continue
		}
		pairs = append(pairs, [2]string{reads.trees[string(branch.Base)], reads.trees[head]})
		analyzed = append(analyzed, branch)
		if branch.State == model.BranchOpen && masterTree != "" {
			landed = append(landed, reads.trees[head])
		}
	}
	for _, tree := range landed {
		pairs = append(pairs, [2]string{masterTree, tree})
	}
	if len(pairs) > 1 {
		changed, err := e.Repo.ChangedPathsOf(ctx, pairs)
		if err != nil {
			return nil, err
		}
		for i, branch := range analyzed {
			tree := model.ObjectID(reads.trees[reads.heads[branch.Name]])
			e.keepAnalysis(RevisionAnalysis{Base: branch.Base, BaseTree: model.ObjectID(pairs[i][0]), Tree: tree, Paths: changed[i], Scope: macports.ScopeOf(changed[i])})
		}
		reads.fromMaster = map[string][]string{}
		for i, tree := range landed {
			reads.fromMaster[tree] = changed[len(analyzed)+i]
		}
	}
	// Several branches are counted by base, where the Git says counts:
	// most share a few bases, master as each started from it.
	if len(reads.heads) > 1 {
		if version, err := git.ExecutableVersion(ctx, e.options.git()); err == nil && version.AtLeast(git.AheadBehindVersion) {
			for base, names := range bases {
				counts, err := e.Repo.CountsFrom(ctx, base, names)
				if err != nil {
					return nil, err
				}
				maps.Copy(reads.counts, counts)
			}
		}
	}
	return reads, nil
}

// count is a branch's commits beyond its base, as read for every branch,
// else counted for it alone.
func (r *statusReads) count(ctx context.Context, e *Engine, branch model.Branch, head string) (int, error) {
	if n, ok := r.counts[branch.Name]; ok {
		return n, nil
	}
	return e.Repo.CountCommits(ctx, string(branch.Base), head)
}

// worktree is the branch's worktree, where dockhand manages one and the
// branch is checked out in it, as openWorktree finds it; nil otherwise.
func (r *statusReads) worktree(e *Engine, branch model.Branch) *git.Repository {
	if branch.Worktree == "" || !exists(branch.Worktree) {
		return nil
	}
	for _, checkout := range r.checkouts[branch.Name] {
		if samePath(checkout, branch.Worktree) {
			return e.Repo.Worktree(checkout)
		}
	}
	return nil
}

// BranchPort is the one port a branch is about (OnlyPort), for a command
// run in it that names none, read from its files and change records alone:
// branchPort ran a whole status, its checks and evidence among it, to read
// one port's name (the architecture review of 5897a82, R3-37).
func (e *Engine) BranchPort(ctx context.Context, branch model.Branch) (string, bool, error) {
	status := BranchStatus{Branch: branch}
	if _, _, err := e.readFiles(ctx, &status, nil); err != nil {
		return "", false, err
	}
	port, ok := status.OnlyPort()
	return port, ok, nil
}

// Cleaned reports whether a branch's Git branch is gone as clean removes
// it: a merged branch's, once its work is in master, or an archived or
// closed one's that held nothing master lacks: nothing about it needs
// anyone. An open branch whose Git branch is gone was lost, as to a
// rename. A worktree that stays is no clean's, whatever its Git branch:
// status said "cleaned" of branches whose worktrees clean kept (field
// testing's xapian-core-dsq3).
func (s BranchStatus) Cleaned() bool {
	return s.Missing && !s.WorktreeStays && s.Branch.State != model.BranchOpen
}

// Check is what's recorded of the branch's checks, as the verdict reads
// it (branchstate.Check).
func (s BranchStatus) Check() branchstate.Check {
	return branchstate.Check{Latest: s.Latest, Current: s.Current, Active: s.Active, Stopped: s.Stopped, Evidence: s.Evidence,
		Accepted: s.Accepted, AtBase: s.AtBase, Decisions: s.Decisions}
}

// Facts are what's recorded of the branch, as the verdict reads it
// (branchstate.Facts).
func (s BranchStatus) Facts() branchstate.Facts {
	b := branchstate.Branch{Record: s.Branch, Missing: s.Missing, Cleaned: s.Cleaned(), OnMaster: s.OnMaster, Revision: s.LatestRevision, Commits: s.Commits, Edited: s.Edited,
		Held: s.Held, Moved: s.Moved, Awaiting: s.Awaiting, NotAssessed: s.Assessment == AssessmentPending, NoFork: s.NoFork, Pushed: s.Pushed(), OthersPushed: s.SomeoneElsePushed(),
		Failing: s.Failing(), CI: s.CI, Ports: s.Scope.PortNames(), Wider: s.Wider, PastMerge: s.PastMerge}
	if s.Evidence != nil {
		if on, ok := OnValues(s.Evidence.Plan.Environments); ok {
			b.CheckOn = on
		}
	}
	return branchstate.Facts{Branch: b, Check: s.Check()}
}

// wider is the check a passing attended check didn't build, where the
// update gives a reviewer reason to ask for it (the docs dig's I47):
// minmax, after a check of this Mac's release alone, where it raised a
// floor, the oldest macOS the Portfile allows or a toolchain's; and, after
// any check of the default variants, a variant whose sources it changed,
// since every such build takes the defaults. So minmax comes first where
// an update does both, and its pass names the variant next. serve, bump,
// and ship --yes build minmax already; host stays the attended default.
// Nil where neither is asked, or the check built it.
func (e *Engine) wider(ctx context.Context, status BranchStatus, changed []string) *branchstate.Wider {
	plan := status.Evidence.Plan
	if status.Latest.State != model.RunPassed || status.Latest.Origin == model.OriginServe || plan.Variants != "" || plan.EachVariant {
		return nil
	}
	if plan.Profile == string(config.RuleHost) {
		if raised := e.floorsRaised(ctx, status); len(raised) > 0 {
			return &branchstate.Wider{Next: branchstate.Next{Verb: branchstate.VerbCheck, Args: []string{"--build", string(config.RuleMinMax)}},
				Why: fmt.Sprintf("the update raises the oldest macOS %s builds on, or its toolchain's", prose.And(raised))}
		}
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(status.Branch.Base)})
	if err != nil {
		return nil
	}
	// --variants builds one port, so Next names the first with a variant
	// whose sources changed, by --only where the branch changes more than
	// one (variantsTarget), and the rest in its words.
	var changedTargets []model.PlanTarget
	for _, target := range plan.Targets {
		if target.Role == model.Changed {
			changedTargets = append(changedTargets, target)
		}
	}
	var next *branchstate.Wider
	var others []string
	for _, directory := range status.Scope.Ports {
		file := path.Join(directory, "Portfile")
		var patches []string
		for _, changedFile := range changed {
			if name, ok := strings.CutPrefix(changedFile, directory+"/files/"); ok {
				patches = append(patches, name)
			}
		}
		before, _ := e.Repo.ReadFiles(ctx, trees[string(status.Branch.Base)], []string{file})
		after, _ := e.Repo.ReadFiles(ctx, status.Tree, []string{file})
		variants := portfile.SourceVariants(before[file], after[file], patches)
		if len(variants) == 0 {
			continue
		}
		signed := make([]string, len(variants))
		for i, variant := range variants {
			signed[i] = "+" + variant
		}
		port := path.Base(directory)
		if i := slices.IndexFunc(changedTargets, func(t model.PlanTarget) bool { return t.Directory == directory }); i >= 0 {
			port = changedTargets[i].Target.Name
		}
		if next != nil {
			others = append(others, port+"'s "+prose.And(signed))
			continue
		}
		args := []string{"--variants", strings.Join(signed, "")}
		if len(changedTargets) > 1 {
			args = append(args, "--only", port)
		}
		next = &branchstate.Wider{Next: branchstate.Next{Verb: branchstate.VerbCheck, Args: args},
			Why: fmt.Sprintf("the update changes the sources of %s's %s, which a build of the default variants doesn't take", port, prose.And(signed))}
	}
	if next != nil && len(others) > 0 {
		next.Why += ", nor " + prose.And(others)
	}
	return next
}

// floorsRaised are the ports whose floor the branch's update raised: a
// platforms Darwin floor or macosx_deployment_target in its change
// records (floorConcerns), or upstream's newer toolchain in its recorded
// assessments (assess.RaisesFloor).
func (e *Engine) floorsRaised(ctx context.Context, status BranchStatus) []string {
	var raised []string
	for _, floor := range floorConcerns(status.Changes) {
		if !slices.Contains(raised, floor.Port) {
			raised = append(raised, floor.Port)
		}
	}
	if assessments, err := e.revisionAssessments(ctx, status.Branch.ID, status.Branch.Base, model.ObjectID(status.Tree), false); err == nil {
		for _, a := range assessments {
			if slices.ContainsFunc(a.Comparison.Changes, assess.RaisesFloor) && !slices.Contains(raised, a.Port) {
				raised = append(raised, a.Port)
			}
		}
	}
	return raised
}

// Vocabulary words what branchstate's sentences name, as the engine words
// each (branchstate.Vocabulary).
type Vocabulary struct{}

func (Vocabulary) Environment(environment model.Environment) string {
	return DescribeEnvironment(environment)
}
func (Vocabulary) Revision(revision model.Revision) string   { return Describe(revision) }
func (Vocabulary) Ago(at time.Time) string                   { return Ago(at) }
func (Vocabulary) Remade(target TargetEvidence) string       { return RemadeWords(target) }
func (Vocabulary) Holds(concerns []model.Concern) []string   { return HoldLines(concerns) }
func (Vocabulary) Unmet(unmet model.Unmet) string            { return UnmetWords(unmet) }
func (Vocabulary) CIFailure(failure model.CIFailure) string  { return CIFailureWords(failure) }
func (Vocabulary) CIPorts(failures []model.CIFailure) string { return CIPortWords(failures) }

// Checking is the branch's one check queued or running (branchstate).
func (s BranchStatus) Checking() *model.Run { return s.Check().Checking() }

// CheckStanding judges the branch's checks (branchstate.Check.Standing).
func (s BranchStatus) CheckStanding() branchstate.Standing { return s.Check().Standing() }

// Pushed reports whether the pull request has the branch's head.
func (s BranchStatus) Pushed() bool {
	return s.Branch.PullRequest != nil && string(s.Branch.PullRequest.Pushed) == s.Head
}

// Status gathers every open branch's status, or the named branches'.
func (e *Engine) Status(ctx context.Context, states ...model.BranchState) ([]BranchStatus, error) {
	if len(states) == 0 {
		states = []model.BranchState{model.BranchOpen}
	}
	return e.statusWhere(ctx, states, nil)
}

// statusWhere is Status of the branches in states that keep, from their
// records alone, before any git read; nil keeps every one.
func (e *Engine) statusWhere(ctx context.Context, states []model.BranchState, keep func(model.Branch) bool) ([]BranchStatus, error) {
	renamed, err := e.followRenames(ctx, "")
	if err != nil {
		return nil, err
	}
	var branches []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branches, err = r.Branches(store.BranchFilter{States: states})
		return err
	}); err != nil {
		return nil, err
	}
	if keep != nil {
		branches = slices.DeleteFunc(branches, func(b model.Branch) bool { return !keep(b) })
	}
	var all []BranchStatus
	// The checkout's remotes are the same for every branch: read once,
	// where each branch ran five git processes for them, 3,415 of
	// status --all's 7,372 over 683 branches (the M1's rc1 measure).
	noFork := !e.hasForkRemote(ctx)
	reads, err := e.readBranches(ctx, branches)
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
		status, err := e.branchStatus(ctx, branch, noFork, reads)
		if err != nil {
			return nil, err
		}
		status.Renamed = renamed[branch.ID]
		all = append(all, status)
	}
	return all, nil
}

// BranchStatusOf is the status of the branch an ID names, its record read
// again, as after a check that moved it on, for what moves it on next:
// view read and judged a branch itself (Then's item 6).
func (e *Engine) BranchStatusOf(ctx context.Context, id model.BranchID) (BranchStatus, error) {
	branch, err := e.Branch(ctx, id)
	if err != nil {
		return BranchStatus{}, err
	}
	return e.BranchStatus(ctx, branch)
}

// BranchStatus gathers one branch's status.
func (e *Engine) BranchStatus(ctx context.Context, branch model.Branch) (BranchStatus, error) {
	return e.branchStatus(ctx, branch, !e.hasForkRemote(ctx), nil)
}

// branchStatus is BranchStatus, with whether the checkout has a fork
// remote read already.
func (e *Engine) branchStatus(ctx context.Context, branch model.Branch, noFork bool, reads *statusReads) (BranchStatus, error) {
	status := BranchStatus{Branch: branch, NoFork: noFork}
	if reads == nil {
		var err error
		if reads, err = e.readBranches(ctx, []model.Branch{branch}); err != nil {
			return status, err
		}
	}
	changed, worktree, err := e.readFiles(ctx, &status, reads)
	if err != nil || status.Missing {
		return status, err
	}
	if branch.State == model.BranchMerged {
		past, err := e.PastMerge(ctx, branch, status.Head)
		if err != nil {
			return status, err
		}
		status.PastMerge = len(past)
	}
	if branch.State == model.BranchOpen {
		if status.OnMaster, err = e.landedOnMaster(ctx, changed, status.Tree, reads); err != nil {
			return status, err
		}
		if err := e.readOpen(ctx, &status, worktree); err != nil {
			return status, err
		}
	}
	var checks []model.Run
	var edits []model.Edit
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if edits, err = r.Edits(branch.ID); err != nil {
			return err
		}
		if pr := branch.PullRequest; pr != nil && pr.Observed != nil && pr.Observed.Checks == model.ChecksFailing {
			if status.CI, err = r.CIFailures(branch.ID, pr.Observed.Head); err != nil {
				return err
			}
		}
		for _, edit := range edits {
			if edit.Release == nil {
				continue
			}
			found := PortRelease{Port: edit.Port, Release: *edit.Release}
			if i := slices.IndexFunc(status.Releases, func(r PortRelease) bool { return r.Port == edit.Port }); i >= 0 {
				status.Releases[i] = found
			} else {
				status.Releases = append(status.Releases, found)
			}
		}
		runs, err := r.Runs(store.RunFilter{Branch: branch.ID})
		if err != nil {
			return err
		}
		for _, run := range runs {
			switch {
			case run.State == model.RunQueued || run.State == model.RunRunning:
				status.Active = append(status.Active, run)
			case run.State.Finished() && run.BaselineOf == "" && status.Latest == nil:
				latest := run
				status.Latest = &latest
			}
		}
		slices.Reverse(status.Active)
		// A check of the files as they are now stands, whichever it was, as
		// submit credits it (EvidenceFor): a branch restored to files an
		// earlier check passed is that check's, not the newest one's.
		if checks, err = treeRuns(r, branch.ID, model.ObjectID(status.Tree)); err != nil {
			return err
		}
		if len(checks) > 0 {
			status.Latest, status.Current = &checks[0], true
		}
		if status.Latest == nil {
			return nil
		}
		revision, err := r.Revision(status.Latest.Revision)
		if err != nil {
			return err
		}
		status.LatestRevision = &revision
		if !status.Current {
			checks, err = treeRuns(r, branch.ID, revision.Source.Tree)
		}
		if err != nil || branch.State == model.BranchOpen {
			return err
		}
		// An ended branch's checks are kept only as its newest, without
		// what it built, once cleanup removed the rest (D6): its check is
		// said as it ended, from the run, with no evidence to read.
		executions, err := r.Executions(status.Latest.ID)
		if err == nil && len(executions) == 0 {
			status.Pruned = true
		}
		return err
	})
	if err != nil || status.Latest == nil || status.Pruned {
		return status, err
	}
	if !status.Current && worktree != nil {
		if err := e.compareIncluded(ctx, worktree, &status); err != nil {
			return status, err
		}
	}
	evidence, err := e.evidenceNow(ctx, *status.Latest, checks)
	status.Evidence = &evidence
	if status.Current {
		// Each lets go where a person's decision answers its commits, as
		// submit's do (D1).
		decided := map[string]model.Decision{}
		for _, decision := range status.Decided {
			decided[decision.Subject] = decision
		}
		status.Moved = undecided(append(preparedSources(status.Evidence, edits), assessedSources(status.Evidence, status.assessed)...), decided)
	}
	if err == nil && status.Current {
		status.Wider = e.wider(ctx, status, changed)
	}
	if err == nil && status.Current && branchstate.ReadsDecisions(evidence) {
		err = e.readDecisions(ctx, &status)
	}
	return status, err
}

// readOpen reads what status says of an open branch beside its files:
// what holds it, its hand edits, and what its last submission plan found
// that took the network. An ended branch, merged, closed, or archived,
// has nothing submitted from it, so they say nothing of it, and status
// --all skips them (Then's item 6).
func (e *Engine) readOpen(ctx context.Context, status *BranchStatus, worktree *git.Repository) error {
	// What holds a branch is read for every open branch, as status says it
	// for each: paused where serve prepared it, and for a person's own, what
	// submit shows (C2, where it was serve's alone).
	var err error
	if status.Held, status.assessed, status.Assessment, err = e.assessedHolds(ctx, status.Branch, model.ObjectID(status.Tree), status.Scope.Ports); err != nil {
		return err
	}
	// A hand edit says why it's the person's, as its working files have
	// it, written or committed (Then's item 3). The worktree is opened,
	// never checked out again: status only reads, and a worktree gone
	// stays gone, its committed files standing for it (openWorktree's
	// rule; rc1 checked removed ones out again).
	hands, err := e.handEditsIn(ctx, status.Branch.ID, string(status.Branch.Base), func() string {
		if worktree != nil {
			if _, final, err := worktree.WorkingTree(ctx); err == nil {
				return final
			}
		}
		return status.Tree
	})
	if err != nil {
		return err
	}
	for _, directory := range slices.Sorted(maps.Keys(hands)) {
		status.HandEdits = append(status.HandEdits, hands[directory])
	}
	// And what the last submission plan of these files found that took
	// the network, another pull request or a moved source, as it found
	// them (C2's rest), since status reads no network itself.
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		network, at, err := r.NetworkHolds(status.Branch.ID, model.ObjectID(status.Tree))
		status.Held, status.NetworkHeldAt = append(status.Held, holding(network, e.allowedFor)...), at
		return err
	}); err != nil {
		return err
	}
	// The person's decisions on its concerns: an acceptance lets its
	// concern go, said; one waiting on something blocks it.
	decided := e.decided(ctx, status.Branch.ID, status.Tree)
	status.Held = undecided(status.Held, decided)
	for _, subject := range slices.Sorted(maps.Keys(decided)) {
		status.Decided = append(status.Decided, decided[subject])
		if decided[subject].Answer == model.Waiting {
			status.Awaiting = append(status.Awaiting, decided[subject])
		}
	}
	return nil
}

// readDecisions reads what publication reads of a branch beside its
// failed check, as submit does: the failures a person accepted at its
// head, those a baseline fails alike, and the standing decisions, so
// status, serve's candidates, and ship --passing judge them as submit
// does (Codex's review of 8c007979, C8-3).
func (e *Engine) readDecisions(ctx context.Context, status *BranchStatus) error {
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		decisions, err := r.Decisions(status.Branch.ID, model.FindingFailure, status.Head)
		if err != nil {
			return err
		}
		for _, d := range decisions {
			status.Accepted = append(status.Accepted, d.Subject)
		}
		status.Decisions, err = r.StandingDecisions(model.FindingEnvironmentFailure)
		return err
	}); err != nil {
		return err
	}
	atBase, err := e.failsAtBase(ctx, status.Branch.ID, *status.Evidence)
	status.AtBase = atBase
	return err
}

// compareIncluded compares the files as they are now with a snapshot that
// took in untracked files with --include, as they are now: where the
// snapshot holds files untracked now, and the files with them are what it
// checked, it stands as current, and Included names them. Untracked files
// it didn't take in are left out, as a capture leaves them.
func (e *Engine) compareIncluded(ctx context.Context, worktree *git.Repository, status *BranchStatus) error {
	revision := status.LatestRevision
	if revision == nil || revision.Kind != model.RevisionSnapshot {
		return nil
	}
	untracked, err := worktree.Untracked(ctx)
	if err != nil {
		return err
	}
	var included []string
	for _, path := range untracked {
		if file, _, err := e.Repo.File(ctx, string(revision.Source.Tree), path); err == nil && file.Exists {
			included = append(included, path)
		}
	}
	if len(included) == 0 {
		return nil
	}
	tree, err := worktree.WithFiles(ctx, status.Tree, included)
	if err != nil {
		return nil
	}
	if model.ObjectID(tree) == revision.Source.Tree {
		status.Current, status.Included = true, included
	}
	return nil
}

// landedOnMaster is the master dockhand last fetched where, for each path
// a branch's files change, its file is the branch's: the branch's work is
// on master already. Status reads the master kept, never fetching one.
func (e *Engine) landedOnMaster(ctx context.Context, changed []string, tree string, reads *statusReads) (model.ObjectID, error) {
	master := reads.master
	if master == "" || len(changed) == 0 {
		return "", nil
	}
	differ, ok := reads.fromMaster[tree]
	var err error
	if !ok {
		differ, err = e.Repo.ChangedPaths(ctx, reads.trees[string(master)], tree)
	}
	if err != nil {
		return "", err
	}
	if slices.ContainsFunc(changed, func(path string) bool { return slices.Contains(differ, path) }) {
		return "", nil
	}
	return master, nil
}

// Runs lists runs, newest first.
func (e *Engine) Runs(ctx context.Context, filter store.RunFilter) ([]model.Run, error) {
	var runs []model.Run
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		runs, err = r.Runs(filter)
		return err
	})
	return runs, err
}

// LatestCheck is a branch's newest check, in any state, a baseline aside:
// the one logs, retry, wait, and watch's l land on, a branch having one
// check at a time (SQL-O1). A baseline is named by its own name. Status's
// verdict reads the newest finished one instead, until layer 1's
// branchstate takes it. ok is false where the branch has none.
func (e *Engine) LatestCheck(ctx context.Context, branch model.BranchID) (run model.Run, ok bool, err error) {
	runs, err := e.Runs(ctx, store.RunFilter{Branch: branch, NoBaselines: true, Limit: 1})
	if err != nil || len(runs) == 0 {
		return model.Run{}, false, err
	}
	return runs[0], true, nil
}

// checkName is a check's name, check-42 or 42.
var checkName = regexp.MustCompile(`^(check-)?[0-9]+$`)

// IsCheckName reports whether name is spelled as a check's, check-42 or 42.
func IsCheckName(name string) bool { return checkName.MatchString(name) }

// RunOrExecution is the check a name means, check-42, or the provider run
// it means, by its ID or its provider's own reference, with that run's
// check (the command package split's 7a row 11).
func (e *Engine) RunOrExecution(ctx context.Context, name string) (model.Run, model.ExecutionID, error) {
	run, err := e.RunNamed(ctx, name)
	if err == nil || IsCheckName(name) {
		return run, "", err
	}
	execution, runErr := e.ExecutionNamed(ctx, name)
	if runErr != nil {
		return model.Run{}, "", fmt.Errorf("%s is neither a check, such as check-42, nor a provider run, such as tart_7y62p4sigena6xlr or a workflow run's URL", name)
	}
	run, err = e.Run(ctx, execution.Run)
	return run, execution.ID, err
}

// RunNamed finds a run by the name people type, check-42, or its number.
func (e *Engine) RunNamed(ctx context.Context, name string) (model.Run, error) {
	var number int
	for _, prefix := range []string{"check-", ""} {
		if n, ok := parseNumber(name, prefix); ok {
			number = n
			break
		}
	}
	if number == 0 {
		return model.Run{}, errors.New(name + " is not a run's name, such as check-42")
	}
	var run model.Run
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		run, err = r.RunNumbered(number)
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		// One numbered below a run there is was there once: cleanup
		// removes an ended branch's checks but its newest (D6).
		newest, err := r.Runs(store.RunFilter{Limit: 1})
		if err == nil && len(newest) > 0 && newest[0].Number > number {
			return fmt.Errorf("check-%d is no longer recorded: cleanup removes what a branch's checks recorded once it has been merged, closed, or archived for cleanup.after, keeping its newest check", number)
		}
		return errors.New("there is no run " + name)
	})
	return run, err
}

// ExecutionNamed finds a provider run by its ID, tart_7y62p4sigena6xlr, or
// by its provider's own reference for it, such as a workflow run's URL:
// the latest, when a provider reused one.
func (e *Engine) ExecutionNamed(ctx context.Context, name string) (model.GuestExecution, error) {
	var found model.GuestExecution
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		execution, err := r.Execution(model.ExecutionID(name))
		if err == nil {
			found = execution
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		referred, err := r.ExecutionsReferred(name)
		if err != nil {
			return err
		}
		if len(referred) == 0 {
			return errors.New("there is no provider run " + name)
		}
		found = referred[len(referred)-1]
		return nil
	})
	return found, err
}

func parseNumber(text, prefix string) (int, bool) {
	if len(text) <= len(prefix) || text[:len(prefix)] != prefix {
		return 0, false
	}
	n := 0
	for _, c := range text[len(prefix):] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n > 0
}

// RunLogs are the logs a run's executions left, target by target.
type RunLogs struct {
	Run        model.Run
	Executions []ExecutionLogs
	// Progress is what a running run last said of its work, as "macOS 26
	// (Tahoe) arm64: building gnome-voice: configure".
	Progress string
}

// LiveLog is where a running run's driver is copying port's log as the
// guest writes it (buildenv.LogName), in the newest of its builds'
// directories, for logs --follow; empty where none is copied yet, as
// before the build reaches the port, or on a provider that copies none.
// port is a target's ID or its port's name, which a variant build's ID
// begins with.
func (e *Engine) LiveLog(ctx context.Context, id model.RunID, port string) (string, error) {
	var run model.Run
	var plan model.Plan
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if run, err = r.Run(id); err != nil {
			return err
		}
		plan, err = r.Plan(run.Plan)
		return err
	}); err != nil {
		return "", err
	}
	newest, at := "", time.Time{}
	for _, target := range plan.Targets {
		if string(target.ID) != port && target.Target.Name != port && target.Target.Subport != port {
			continue
		}
		found, err := filepath.Glob(filepath.Join(e.LogDirectory(), run.Name(), "*", buildenv.LogName(target.ID)))
		if err != nil {
			return "", err
		}
		for _, path := range found {
			if info, err := os.Stat(path); err == nil && info.ModTime().After(at) {
				newest, at = path, info.ModTime()
			}
		}
	}
	return newest, nil
}

// Building reports whether the run's progress says it's building port,
// with the step under way, as the Tart provider says it ("building jq (2
// of 5): fetch"; tart.buildingWords): logs of a running target said it
// "recorded no log", which read as a failure (the gnome-voice session's
// notes, 2026-10-20).
func (l RunLogs) Building(port string) (phase string, ok bool) {
	if l.Run.State != model.RunRunning {
		return "", false
	}
	_, rest, found := strings.Cut(l.Progress, "building "+port)
	if !found || rest != "" && rest[0] != ' ' && rest[0] != ':' && rest[0] != ';' {
		return "", false
	}
	if strings.HasPrefix(rest, " (") {
		if _, after, closed := strings.Cut(rest, ")"); closed {
			rest = after
		}
	}
	if after, stepped := strings.CutPrefix(rest, ": "); stepped {
		phase, _, _ = strings.Cut(after, ";")
	}
	return strings.TrimSpace(phase), true
}

// PortLog is one build's log of a port: the provider run it was in, and
// its result.
type PortLog struct {
	Execution model.GuestExecution
	Result    model.TargetResult
}

// ForPort are a check's logs of a port, the one to show first, and the
// others: where it failed, or its tests did, before where it passed, the
// latest of those. check-65's libuv-devel failed its tests on macOS 15 and
// passed on 26, and logs of the port showed 26's, the last (the hugo
// exercise). It's false where no build of the port left a log.
func (l RunLogs) ForPort(port string) (PortLog, []PortLog, bool) {
	var all []PortLog
	for _, execution := range l.Executions {
		for _, result := range execution.Results {
			if string(result.Target) == port && result.Log != "" {
				all = append(all, PortLog{Execution: execution.Execution, Result: result})
			}
		}
	}
	if len(all) == 0 {
		return PortLog{}, nil, false
	}
	rank := func(result model.TargetResult) int {
		switch {
		case result.Outcome == model.OutcomeFailed:
			return 2
		case result.Tests.Failed():
			return 1
		}
		return 0
	}
	chosen := 0
	for i, log := range all {
		if rank(log.Result) >= rank(all[chosen].Result) {
			chosen = i
		}
	}
	return all[chosen], append(slices.Clone(all[:chosen]), all[chosen+1:]...), true
}

// StepLines are where a step of a port's build is in its log (the steps
// its provider recorded, model.LogStep): the line it began on, and the
// line the next began on, its own next step's, or another target's in the
// same log, 0 where it ran to the log's end, as FailureLines bounds a
// failed step. A step that ran more than once is its last run. It's false
// where the build recorded no such step.
func (l RunLogs) StepLines(log PortLog, step model.Step) (from, to int, ok bool) {
	for _, recorded := range log.Result.Steps {
		if recorded.Name == step {
			from, ok = recorded.Line, true
		}
	}
	if !ok {
		return 0, 0, false
	}
	for _, execution := range l.Executions {
		if execution.Execution.ID != log.Execution.ID {
			continue
		}
		for _, result := range execution.Results {
			if result.Log != log.Result.Log {
				continue
			}
			for _, other := range result.Steps {
				if other.Line > from && (to == 0 || other.Line < to) {
					to = other.Line
				}
			}
		}
	}
	return from, to, true
}

// ExecutionLogs are one execution's results and where its logs are.
type ExecutionLogs struct {
	Execution model.GuestExecution
	Results   []model.TargetResult
	// Git are what the builds of its Git-fetched targets fetched, by
	// target, where one built.
	Git map[model.TargetID]GitFetch
}

// GitFetch is what a Git-fetched target's build fetched (batch 20): the
// source its plan expected, and the commit the build recorded fetching,
// empty where its provider didn't say (FetchedWords).
type GitFetch struct {
	Expected model.GitSource
	Fetched  model.ObjectID
}

// Logs gathers where a run's logs are, and what the builds of its
// Git-fetched targets fetched, which is evidence of what they built.
func (e *Engine) Logs(ctx context.Context, id model.RunID) (RunLogs, error) {
	logs := RunLogs{}
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		// The check's records in one read (the SQL study's O2).
		check, err := r.Check(id)
		if err != nil {
			return err
		}
		logs.Run = check.Run
		if logs.Run.State == model.RunRunning {
			event, found, err := r.LatestRunEvent(id, model.EventProgress, model.EventWaiting)
			if err != nil {
				return err
			}
			if found {
				logs.Progress = event.Message
			}
		}
		for _, execution := range check.Executions {
			results := check.Results[execution.ID]
			entry := ExecutionLogs{Execution: execution, Results: results}
			for _, result := range results {
				source, git := check.Plan.GitIn(execution.Environment, result.Target)
				if !git || result.Outcome != model.OutcomePassed && result.Outcome != model.OutcomeFailed {
					continue
				}
				fetch := GitFetch{Expected: source}
				if result.Inputs != "" {
					fetch.Fetched = check.Inputs[result.Inputs].Fetched
				}
				if entry.Git == nil {
					entry.Git = map[model.TargetID]GitFetch{}
				}
				entry.Git[result.Target] = fetch
			}
			logs.Executions = append(logs.Executions, entry)
		}
		return nil
	})
	return logs, err
}

// IncludedPorts are the ports the latest check built from files Git
// doesn't track, which check --include added: a new port's Portfile, which
// the branch's own files don't change yet, so ChangedPorts names none.
// Status said "Ports none yet" of a branch whose check had just built one
// (Codex's feedback, approved 2026-10-05). None where the branch's files
// change a port, or the latest check was of a commit.
func (s BranchStatus) IncludedPorts() []string {
	if ports, _ := s.ChangedPorts(); len(ports) > 0 || s.LatestRevision == nil || s.LatestRevision.Kind != model.RevisionSnapshot || s.Evidence == nil {
		return nil
	}
	var included []string
	for _, target := range s.Evidence.Plan.Targets {
		if target.Role == model.Changed && !slices.Contains(included, target.Target.Name) {
			included = append(included, target.Target.Name)
		}
	}
	return included
}

// Touches reports whether a branch is a port's: it changes the port, by
// its directory or its change record, or it was started for it
// (model.Branch.Ports), as status --port reads it.
func (s BranchStatus) Touches(port string) bool {
	changed, _ := s.ChangedPorts()
	return slices.Contains(s.Scope.PortNames(), port) || slices.Contains(changed, port) || slices.Contains(s.Branch.Ports, port)
}

// OnlyPort is the one port a branch is about, for a command run in it that
// names none: what its files change, but _resources, and the ports it was
// started for (model.Branch.Ports); false where that's none or several.
func (s BranchStatus) OnlyPort() (string, bool) {
	changed, _ := s.ChangedPorts()
	var ports []string
	for _, port := range append(changed, s.Branch.Ports...) {
		if port != macports.ResourcesDirectory && !slices.Contains(ports, port) {
			ports = append(ports, port)
		}
	}
	if len(ports) != 1 {
		return "", false
	}
	return ports[0], true
}

// ChangedPorts are what the branch's files change as they are now, as a
// person reads it: the ports its change records say (recordedPorts), then
// _resources, where it changed.
func (s BranchStatus) ChangedPorts() (ports []string, notes map[string]string) {
	ports, notes = recordedPorts(s.Scope.Ports, s.Changes)
	if s.Scope.Resources {
		ports = append(ports, macports.ResourcesDirectory)
	}
	return ports, notes
}
