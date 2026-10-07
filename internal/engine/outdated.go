package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// OutdatedRequest chooses the ports to look at: the ports named, or those
// with one of the maintainers.
type OutdatedRequest struct {
	Ports       []string
	Maintainers []string
	// Progress, when set, hears how many of the ports are looked up, as
	// the lookups finish.
	Progress func(done, total int)
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

func (r OutdatedReport) UncheckedWords() string {
	unchecked := r.Unchecked()
	if len(unchecked) == 0 {
		return ""
	}
	why, _, _ := strings.Cut(unchecked[0].Problem, "\n")
	return fmt.Sprintf("%d of %s couldn't be checked: %s", len(unchecked), prose.Plural(len(r.Ports), "port"), why)
}

// Outdated reports which of the chosen ports have newer releases upstream
// (Design v3 §6.12), at master as fetched now.
func (e *Engine) Outdated(ctx context.Context, request OutdatedRequest) (OutdatedReport, error) {
	if len(request.Ports) == 0 && len(request.Maintainers) == 0 {
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
	slices.SortFunc(ports, func(a, b OutdatedPort) int { return strings.Compare(a.Port, b.Port) })
	return OutdatedReport{Master: master, Ports: ports}, err
}

// outdatedReader is the engine's OutdatedReader: the one it was given, or
// MacPorts' evaluator with upstream discovery.
func (e *Engine) outdatedReader() (OutdatedReader, error) {
	return assemble(e, &e.OutdatedReader, func() (OutdatedReader, error) {
		ports, err := e.selectionReader()
		if err != nil {
			return nil, err
		}
		return &surveyedPorts{e: e, service: &outdated.Service{Repo: e.Repo, Ports: ports, Upstream: e.discovery(ports), Index: ports.Index, Workspaces: &workspace.Registry{}}}, nil
	})
}

// surveyedPorts reads outdated ports with the outdated package's service.
type surveyedPorts struct {
	e       *Engine
	service *outdated.Service
}

func (s *surveyedPorts) Outdated(ctx context.Context, commit model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error) {
	service := *s.service
	service.Commit, service.Progress = string(commit), request.Progress
	result, err := service.Observe(ctx, outdated.Selection{Ports: request.Ports, Maintainers: request.Maintainers})
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
	case upstream.Uncertain:
		entry.Uncertain = port.SetAside
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
}

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

// PlanOutdated splits a report's outdated ports into branches: a port an
// open branch already changes, or whose newest release could not be
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
		name, err := e.NameFor(ctx, port.Port, port.Newest)
		if err != nil {
			return plan, err
		}
		plan.Updates = append(plan.Updates, PlannedUpdate{Port: port, Name: name})
	}
	return plan, nil
}

// PrepareOptions are how PrepareOutdated prepares each branch.
type PrepareOptions struct {
	// Origin is who asked: a person, or serve.
	Origin model.Origin
	// Check queues a check of each prepared commit, in Environments.
	Check        bool
	Environments []model.Environment
	Tests        model.TestPolicy
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
// to make; commits the edit when the plan is unambiguous; and with Check
// queues a check of that commit. A
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
	done := PreparedUpdate{Planned: planned}
	var err error
	done.Update, err = e.Update(ctx, UpdateRequest{Start: &StartRequest{Name: planned.Name, Origin: options.Origin}, Action: model.EditUpdate, Port: planned.Port.Port, Version: planned.Port.Newest,
		Release: planned.Port.Release, CompareUpstream: true, NoHandBranch: true})
	done.Branch = done.Update.Branch
	if err != nil {
		done.Problem = err.Error()
		if !done.Update.Started {
			done.Problem += "; no branch was started"
		}
		return done
	}
	if done.Update.Current {
		done.Problem = "nothing to change: it is already at " + done.Update.After.String()
		return done
	}
	branch := done.Branch
	tidy, err := e.PlanTidy(ctx, TidyRequest{Branch: branch})
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	if !tidy.Unambiguous() {
		done.Problem = "the edit needs a look before it is committed: dockhand tidy --branch " + branch.ShortName()
		return done
	}
	if _, err := e.ApplyTidy(ctx, tidy); err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Tidied = true
	if !options.Check {
		return done
	}
	capture, err := e.Capture(ctx, CaptureRequest{Branch: branch, Mode: CaptureHead})
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	checkPlan, err := e.PlanCheck(ctx, PlanRequest{Revision: capture.Revision, Environments: options.Environments, Tests: options.Tests})
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	if !checkPlan.Runnable() {
		done.Problem = "its check can't be planned: " + unresolvedWords(checkPlan)
		return done
	}
	run, err := e.Enqueue(ctx, branch, checkPlan, options.Origin)
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Run = &run
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
