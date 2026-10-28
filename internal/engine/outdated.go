package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// OutdatedRequest chooses the ports to look at: the ports named, or those
// with one of the maintainers.
type OutdatedRequest struct {
	Ports       []string
	Maintainers []string
	// Categories and NotMaintainers narrow a selection by master's index,
	// as port echo combines them, beside Maintainers.
	Categories, NotMaintainers []string
	// Progress, when set, hears how many of the ports are looked up, as
	// the lookups finish.
	Progress func(done, total int)
	// Allow, when set, is asked before the first lookup whether the look
	// may go on, given how many it makes (outdated.Service.Allow).
	Allow func(ctx context.Context, lookups int) error
}

// OutdatedPort is one port as its upstream stands.
type OutdatedPort struct {
	Port    string
	Current string
	Newest  string
	// Outdated is true when upstream has a newer release.
	Outdated bool
	// Uncertain are the versions that compare newer than the port's own,
	// newest first, but that discovery set aside, since their tags'
	// commits predate the port's own tag's: whether the port is outdated
	// is a person's call, and Newest is the first of them. It is neither
	// current nor to be updated by itself.
	Uncertain []SetAside
	// Ahead is, for a port whose version compares newer than every one
	// upstream lists, its own tag not among them, the newest tag listed:
	// the repository may have moved, so it's neither current nor to be
	// updated by itself, and Newest is that tag's version.
	Ahead string
	// Problem says why the port could not be checked; RetryAt, when a
	// rate limit that kept it from being checked lifts, zero where none.
	Problem string
	RetryAt time.Time
	Release *model.Release
	// With is the subport whose check stands for this one, which shares
	// its Portfile's release; it moves with that one's update.
	With string
	// Moved is, for a port that tracks a branch, the branch and the newer
	// commit it names than the one the port pins: behind, with a version
	// a person names, so it's neither outdated nor planned.
	Moved *Head
	// OwnVersion is a port with no release to look for: it fetches
	// nothing, and no livecheck reads its version, as a _select port's.
	// Its version is MacPorts' own; it's covered, and never planned.
	OwnVersion bool
	// Unpublished says why a newer release isn't ready: its release has
	// none of the files the port fetches yet (upstream's askAssets). It's
	// said, as update would refuse it, and never planned.
	Unpublished string
}

// OutdatedClass is what a look found of a port, one of the classes below,
// which plain outdated counts and lists by (Summary).
type OutdatedClass string

const (
	// OutdatedOwnVersion has no release to look for (OwnVersion).
	OutdatedOwnVersion OutdatedClass = "own-version"
	// OutdatedMoved tracks a branch that names a newer commit (Moved).
	OutdatedMoved OutdatedClass = "moved"
	// OutdatedUnchecked couldn't be checked (Problem).
	OutdatedUnchecked OutdatedClass = "unchecked"
	// OutdatedUncertain may have a newer release, for a look (Uncertain).
	OutdatedUncertain OutdatedClass = "uncertain"
	// OutdatedAhead is ahead of every version upstream lists, for a look
	// (Ahead).
	OutdatedAhead OutdatedClass = "ahead"
	// OutdatedUnpublished has a newer release not ready yet (Unpublished).
	OutdatedUnpublished OutdatedClass = "unpublished"
	// OutdatedNewer has a newer release (Outdated).
	OutdatedNewer OutdatedClass = "newer"
	// OutdatedCurrent has none.
	OutdatedCurrent OutdatedClass = "current"
)

// Class is what the look found of the port, the first of its facts that
// holds, in the order the constants are: one with no release to look for
// is neither checked nor current, and one that couldn't be checked isn't
// said to be anything else.
func (p OutdatedPort) Class() OutdatedClass {
	switch {
	case p.OwnVersion:
		return OutdatedOwnVersion
	case p.Moved != nil:
		return OutdatedMoved
	case p.Problem != "":
		return OutdatedUnchecked
	case len(p.Uncertain) > 0:
		return OutdatedUncertain
	case p.Ahead != "":
		return OutdatedAhead
	case p.Outdated && p.Unpublished != "":
		return OutdatedUnpublished
	case p.Outdated:
		return OutdatedNewer
	}
	return OutdatedCurrent
}

// uncheckedReason is why a port couldn't be checked, as ports are grouped
// by: GitHub's rate limit for every port it kept, whenever each lifts, and
// otherwise its Reason.
func (p OutdatedPort) uncheckedReason() string {
	if !p.RetryAt.IsZero() {
		return "GitHub's rate limit"
	}
	return p.Reason()
}

// Reason is why the port couldn't be checked, in a line: its problem's
// first, the rest being detail, as how its releases were listed after
// GitHub's rate limit stopped the look (R3-38: the command cut it).
func (p OutdatedPort) Reason() string {
	reason, _, _ := strings.Cut(p.Problem, "\n")
	return reason
}

// Head is the branch a port tracks and the commit it names now.
type Head = upstream.Head

// SetAside is a version discovery set aside: it compares newer than the
// port's own, but its tag's commit predates the port's own tag's.
type SetAside = upstream.SetAside

// UncertainRelease is an update's refusal to choose a release where
// discovery set a newer one aside and found nothing newer beyond it: the
// update needs the version named.
type UncertainRelease = upstream.UncertainError

// AheadRelease is an update's refusal to choose a release where the port
// is ahead of every version upstream lists: the update needs the version
// named.
type AheadRelease = upstream.AheadError

// OutdatedReader finds ports' newest releases at a commit of master.
// MacPorts' evaluator and upstream discovery are the real one.
type OutdatedReader interface {
	Outdated(ctx context.Context, commit model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error)
}

// OutdatedReport is what outdated found, at a freshly fetched master.
type OutdatedReport struct {
	Master model.ObjectID
	Ports  []OutdatedPort
}

// Unchecked are the ports that couldn't be checked, as GitHub's rate limit
// leaves them, and UncheckedWords says how many of all, and why the first
// couldn't, as "2 of 4 ports couldn't be checked: GitHub's rate limit …":
// what's said ahead of the rest's result, which it can't stand for, since
// "none has a newer release" was said of four where two due ones went
// unchecked (the rc6 full stage, D-N4).
func (r OutdatedReport) Unchecked() []OutdatedPort {
	var unchecked []OutdatedPort
	for _, port := range r.Ports {
		if port.Problem != "" {
			unchecked = append(unchecked, port)
		}
	}
	return unchecked
}

// RateLimited are the ports a rate limit kept from being checked, and
// when the last of those limits lifts.
func (r OutdatedReport) RateLimited() ([]string, time.Time) {
	var ports []string
	var lifts time.Time
	for _, port := range r.Ports {
		if port.Problem != "" && !port.RetryAt.IsZero() {
			ports = append(ports, port.Port)
			if port.RetryAt.After(lifts) {
				lifts = port.RetryAt
			}
		}
	}
	return ports, lifts
}

// Where they couldn't for several reasons, the reasons are counted, the
// commonest first, two named and the rest counted together: the daily
// look's 27 unchecked were said with one port's reason, "requested version
// matches multiple releases…" (the rc10 full stage's B7).
func (r OutdatedReport) UncheckedWords() string {
	unchecked := r.Unchecked()
	if len(unchecked) == 0 {
		return ""
	}
	count := map[string]int{}
	var reasons []string
	for _, port := range unchecked {
		reason := port.uncheckedReason()
		if count[reason] == 0 {
			reasons = append(reasons, reason)
		}
		count[reason]++
	}
	head := fmt.Sprintf("%d of %s couldn't be checked: ", len(unchecked), prose.Plural(len(r.Ports), "port"))
	if len(reasons) == 1 {
		return head + unchecked[0].Reason()
	}
	// The commonest first; among equals, the order they were met.
	slices.SortStableFunc(reasons, func(a, b string) int { return count[b] - count[a] })
	var said []string
	rest := 0
	for i, reason := range reasons {
		if i < 2 {
			said = append(said, fmt.Sprintf("%d for %s", count[reason], reason))
			continue
		}
		rest += count[reason]
	}
	if rest > 0 {
		said = append(said, fmt.Sprintf("%d more (dockhand outdated --current says each)", rest))
	}
	return head + strings.Join(said, ", ")
}

// Summary counts the report's ports by what the look found of each
// (OutdatedPort.Class).
func (r OutdatedReport) Summary() map[OutdatedClass]int {
	summary := map[OutdatedClass]int{}
	for _, port := range r.Ports {
		summary[port.Class()]++
	}
	return summary
}

// Outdated reports which of the chosen ports have newer releases upstream
// (Design v3 §6.12), at master as fetched now.
func (e *Engine) Outdated(ctx context.Context, request OutdatedRequest) (OutdatedReport, error) {
	if len(request.Ports) == 0 && len(request.Maintainers)+len(request.Categories) == 0 {
		return OutdatedReport{}, fmt.Errorf("name ports, or ask for yours with --mine")
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return OutdatedReport{}, err
	}
	reader, err := e.outdatedReader()
	if err != nil {
		return OutdatedReport{}, err
	}
	ports, err := reader.Outdated(ctx, master, request)
	slices.SortFunc(ports, func(a, b OutdatedPort) int { return macports.ComparePortNames(a.Port, b.Port) })
	return OutdatedReport{Master: master, Ports: ports}, err
}

// outdatedReader is the engine's OutdatedReader: the one it was given, or
// MacPorts' evaluator with the upstream discovery compose gave it
// (Discovery).
func (e *Engine) outdatedReader() (OutdatedReader, error) {
	return assemble(e, &e.OutdatedReader, func() (OutdatedReader, error) {
		ports, err := e.selectionReader()
		if err != nil {
			return nil, err
		}
		if e.Discovery == nil {
			return nil, errors.New("no upstream discovery is set up")
		}
		return &surveyedPorts{service: &outdated.Service{Repo: e.Repo, Ports: ports, Upstream: e.Discovery, Index: ports.Index, Workspaces: &workspace.Registry{}}}, nil
	})
}

// surveyedPorts reads outdated ports with the outdated package's service.
type surveyedPorts struct {
	service *outdated.Service
}

func (s *surveyedPorts) Outdated(ctx context.Context, commit model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error) {
	service := *s.service
	service.Commit, service.Progress, service.Allow = string(commit), request.Progress, request.Allow
	result, err := service.Observe(ctx, outdated.Selection{Ports: request.Ports, Maintainers: request.Maintainers, Categories: request.Categories, NotMaintainers: request.NotMaintainers})
	var ports []OutdatedPort
	for _, port := range result.Ports {
		ports = append(ports, outdatedPort(port))
	}
	return ports, err
}

// outdatedPort is a port as upstream discovery assessed it.
func outdatedPort(port outdated.Port) OutdatedPort {
	entry := OutdatedPort{Port: port.Selector, Current: port.CurrentVersion, Newest: port.CandidateVersion, Release: port.Release, With: port.With}
	switch port.Assessment {
	case upstream.UpdateAvailable:
		entry.Outdated = true
		if port.Unpublished {
			entry.Unpublished = port.Detail
		}
	case upstream.Uncertain:
		entry.Uncertain = port.SetAside
	case upstream.Ahead:
		entry.Ahead = port.NewestTag
	case upstream.Moved:
		entry.Moved = port.Head
	case upstream.OwnVersion:
		entry.OwnVersion = true
	case upstream.Unknown:
		entry.Problem, entry.RetryAt = port.Detail, port.RetryAt
		if entry.Problem == "" {
			entry.Problem = "its newest release could not be found"
		}
	}
	return entry
}

// OutdatedPlan is how update --outdated splits the work before it starts
// anything: one branch per port, since unrelated ports go in separate
// pull requests.
type OutdatedPlan struct {
	Updates []PlannedUpdate
	// Skipped are the outdated ports it leaves alone, and why.
	Skipped []SkippedUpdate
}

// PlannedUpdate is one port's update and the branch it would start.
type PlannedUpdate struct {
	Port OutdatedPort
	// Name is the branch's name, without dockhand/.
	Name string
	// Also are the other ports of Port's Portfile the plan updates, in
	// the one branch, which Name names for Directory: tidy makes a commit
	// each.
	Also      []OutdatedPort
	Directory string
}

// Ports are the ports the planned branch updates, in order.
func (p PlannedUpdate) Ports() []OutdatedPort { return append([]OutdatedPort{p.Port}, p.Also...) }

// CrossesMajor says whether the update moves the port to a new major
// version, which what depends on it may need to follow; a preview says
// it before the update is made.
func (p PlannedUpdate) CrossesMajor() bool {
	return p.Port.Current != "" && version.CrossesMajor(p.Port.Current, p.Port.Newest)
}

// SkippedUpdate is a port left alone, and why.
type SkippedUpdate struct {
	Port   string
	Reason string
}

// PlanOutdated splits a report's outdated ports into branches, one for
// the ports of each Portfile: a port an open branch already changes, or whose newest release could not be
// found, is left alone. A port whose newest release is uncertain isn't
// outdated, and isn't planned: which release it takes is a person's call.
func (e *Engine) PlanOutdated(ctx context.Context, report OutdatedReport) (OutdatedPlan, error) {
	var plan OutdatedPlan
	for _, port := range report.Ports {
		if port.Problem != "" {
			plan.Skipped = append(plan.Skipped, SkippedUpdate{Port: port.Port, Reason: port.Problem})
			continue
		}
		if !port.Outdated {
			continue
		}
		if port.Unpublished != "" {
			plan.Skipped = append(plan.Skipped, SkippedUpdate{Port: port.Port, Reason: port.Unpublished})
			continue
		}
		if port.With != "" {
			plan.Skipped = append(plan.Skipped, SkippedUpdate{Port: port.Port, Reason: "moves with " + port.With + ", whose release it shares"})
			continue
		}
		open, err := e.BranchesChanging(ctx, port.Port)
		if err != nil {
			return plan, err
		}
		if len(open) > 0 {
			plan.Skipped = append(plan.Skipped, SkippedUpdate{Port: port.Port, Reason: "already in " + open[0].ShortName()})
			continue
		}
		// An update a maintainer closed unmerged isn't proposed again at
		// its version: the closed branch keeps its name, so the next look
		// planned it again under another, and serve's submit opened its
		// pull request again each day (the docs dig's I1). A newer release
		// lifts it.
		closed, ok, err := e.ClosedAt(ctx, port.Port, port.Newest)
		if err != nil {
			return plan, err
		}
		if ok {
			plan.Skipped = append(plan.Skipped, SkippedUpdate{Port: port.Port, Reason: ClosedWords(closed, port.Newest)})
			continue
		}
		// The ports of one Portfile go in one branch, named for its
		// directory, where a branch each would be pull requests editing
		// one file: field testing's four kubectl subports.
		directory, _ := e.PortDirectory(ctx, port.Port)
		if at := slices.IndexFunc(plan.Updates, func(u PlannedUpdate) bool { return directory != "" && u.Directory == directory }); at >= 0 {
			planned := &plan.Updates[at]
			if len(planned.Also) == 0 {
				if planned.Name, err = e.FreeName(ctx, path.Base(directory)); err != nil {
					return plan, err
				}
			}
			planned.Also = append(planned.Also, port)
			continue
		}
		name, err := e.NameFor(ctx, port.Port, port.Newest)
		if err != nil {
			return plan, err
		}
		plan.Updates = append(plan.Updates, PlannedUpdate{Port: port, Name: name, Directory: directory})
	}
	return plan, nil
}

// ClosedAt is the newest closed or archived branch that updated a port to
// a version and wasn't merged: an update a maintainer, or the person,
// closed, which an unattended look doesn't propose again at that
// version.
func (e *Engine) ClosedAt(ctx context.Context, port, version string) (model.Branch, bool, error) {
	var found model.Branch
	ok := false
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		branches, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchClosed, model.BranchArchived}})
		if err != nil {
			return err
		}
		for _, branch := range branches {
			// An archived branch may have been merged first: master
			// has that version, and nothing waits for a look.
			if pr := branch.PullRequest; pr != nil && pr.Observed != nil && pr.Observed.State == model.PullRequestMerged {
				continue
			}
			edits, err := r.Edits(branch.ID)
			if err != nil {
				return err
			}
			for _, edit := range edits {
				if edit.Kind == model.EditUpdate && strings.EqualFold(edit.Port, port) && edit.Release != nil && edit.Release.Version == version &&
					(!ok || branch.CreatedAt.After(found.CreatedAt)) {
					found, ok = branch, true
				}
			}
		}
		return nil
	})
	return found, ok, err
}

// ClosedWords say why an update closed unmerged isn't proposed again:
// "1.8.1's pull request #123 was closed unmerged, in jq-1.8.1; a newer
// release lifts this".
func ClosedWords(branch model.Branch, version string) string {
	if pr := branch.PullRequest; pr != nil && pr.Number > 0 {
		return fmt.Sprintf("%s's pull request #%d was closed unmerged, in %s; a newer release lifts this", version, pr.Number, branch.ShortName())
	}
	return fmt.Sprintf("%s's update was %s unmerged, in %s; a newer release lifts this", version, branch.State, branch.ShortName())
}

// PrepareOptions are how PrepareOutdated prepares each branch.
type PrepareOptions struct {
	// Origin is who asked: a person, or serve.
	Origin model.Origin
	// To is how far each goes: ShipCommit, where it's empty, or ShipCheck,
	// which queues a check of each prepared commit where Where says.
	To    ShipStage
	Where Where
	Tests model.TestPolicy
	// Answered is how the person answered for what a person would decide,
	// which serve sets to nobody: the caller says it, where prepareOne
	// read it from Origin (the architecture review of 5897a82, R3-36).
	Answered model.Answered
}

// PreparedUpdate is what preparing one planned update did.
type PreparedUpdate struct {
	Planned PlannedUpdate
	Branch  model.Branch
	Update  Update
	// Tidied is true when the update was committed as one commit.
	Tidied bool
	Run    *model.Run
	// Problem says what stopped it, after whatever it had done.
	Problem string
}

// PrepareOutdated updates each planned port from fresh master, with the
// upstream archives compared, in a branch started once there is an edit
// to make; commits the edit when the plan is unambiguous; and with To
// ShipCheck queues a check of that commit. A
// problem with one port is reported beside the others, and never stops
// them.
func (e *Engine) PrepareOutdated(ctx context.Context, plan OutdatedPlan, options PrepareOptions) []PreparedUpdate {
	var prepared []PreparedUpdate
	for _, planned := range plan.Updates {
		if ctx.Err() != nil {
			break
		}
		prepared = append(prepared, e.prepareOne(ctx, planned, options))
	}
	return prepared
}

func (e *Engine) prepareOne(ctx context.Context, planned PlannedUpdate, options PrepareOptions) PreparedUpdate {
	to := cmp.Or(options.To, ShipCommit)
	done := e.ship(ctx, shipTarget{Port: planned.Port.Port, Version: planned.Port.Newest, Release: planned.Port.Release, Name: planned.Name, Also: planned.Also},
		shipOptions{Origin: options.Origin, To: to, Where: options.Where, Tests: options.Tests, Answered: options.Answered})
	done.Planned = planned
	return done
}

func unresolvedWords(plan model.Plan) string {
	var words []string
	for _, unresolved := range plan.Unresolved {
		words = append(words, unresolved.Target.Name+": "+unresolved.Reason)
	}
	if len(words) == 0 {
		return "it builds nothing"
	}
	return strings.Join(words, "; ")
}

// GitHubAllowance is what's left of the hour's GitHub requests for the
// login the engine's runtime holds, for a look to weigh its cost first.
func (e *Engine) GitHubAllowance(ctx context.Context) (github.Allowance, error) {
	e.github()
	return e.GitHub.Allowance(ctx)
}
