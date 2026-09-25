package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// OutdatedRequest chooses the ports to look at: the ports named, or those
// with one of the maintainers.
type OutdatedRequest struct {
	Ports       []string
	Maintainers []string
}

// OutdatedPort is one port as its upstream stands.
type OutdatedPort struct {
	Port    string
	Current string
	Newest  string
	// Outdated is true when upstream has a newer release.
	Outdated bool
	// Problem says why the port could not be checked.
	Problem string
	Release *record.Release
}

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
	if e.OutdatedReader != nil {
		return e.OutdatedReader, nil
	}
	ports, err := e.selectionReader()
	if err != nil {
		return nil, err
	}
	e.OutdatedReader = &surveyedPorts{e: e, service: &outdated.Service{Repo: e.Repo, Ports: ports, Upstream: e.discovery(ports), Index: ports.Index, Workspaces: &workspace.Registry{}}}
	return e.OutdatedReader, nil
}

// surveyedPorts reads outdated ports with v2's survey.
type surveyedPorts struct {
	e       *Engine
	service *outdated.Service
}

func (s *surveyedPorts) Outdated(ctx context.Context, commit model.ObjectID, request OutdatedRequest) ([]OutdatedPort, error) {
	service := *s.service
	service.Commit = string(commit)
	result, err := service.Observe(ctx, outdated.Selection{Ports: request.Ports, Maintainers: request.Maintainers})
	var ports []OutdatedPort
	for _, port := range result.Ports {
		entry := OutdatedPort{Port: port.Selector, Current: port.CurrentVersion, Newest: port.CandidateVersion, Release: port.Release}
		switch port.Assessment {
		case upstream.UpdateAvailable:
			entry.Outdated = true
		case upstream.Unknown:
			entry.Problem = port.Detail
			if entry.Problem == "" {
				entry.Problem = "its newest release could not be found"
			}
		}
		ports = append(ports, entry)
	}
	return ports, err
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

// SkippedUpdate is a port left alone, and why.
type SkippedUpdate struct {
	Port   string
	Reason string
}

// PlanOutdated splits a report's outdated ports into branches: a port an
// open branch already changes, or whose newest release could not be
// found, is left alone.
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
		open, err := e.BranchesChanging(ctx, port.Port)
		if err != nil {
			return plan, err
		}
		if len(open) > 0 {
			plan.Skipped = append(plan.Skipped, SkippedUpdate{Port: port.Port, Reason: "already in " + open[0].ShortName()})
			continue
		}
		name, err := e.FreeName(ctx, port.Port)
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

// PrepareOutdated starts each planned branch from fresh master, updates
// its port with the upstream archives compared, commits the edit when the
// plan is unambiguous, and with Check queues a check of that commit. A
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
	branch, err := e.Start(ctx, StartRequest{Name: planned.Name, Origin: options.Origin})
	if err != nil {
		done.Problem = err.Error()
		return done
	}
	done.Branch = branch
	if done.Update, err = e.Update(ctx, UpdateRequest{Branch: branch, Action: record.Bump, Port: planned.Port.Port, Version: planned.Port.Newest, CompareUpstream: true}); err != nil {
		done.Problem = err.Error()
		return done
	}
	if done.Update.Current {
		done.Problem = "nothing to change: it is already at " + done.Update.After.String()
		return done
	}
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

// UpstreamFindings are what comparing the upstream archives found for a
// branch's updates, from its recorded edits.
func (e *Engine) UpstreamFindings(ctx context.Context, branch model.Branch) ([]model.UpstreamChange, error) {
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return nil, err
	}
	var changes []model.UpstreamChange
	for _, edit := range edits {
		if edit.Upstream != nil {
			changes = append(changes, edit.Upstream.Changes...)
		}
	}
	return changes, nil
}
