package engine

import (
	"context"
	"slices"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
)

// StatusRequest is what status shows: the branches named, else every
// branch in States touching Port where it names one; and with Overview,
// serve's state, the check queue, dockhand's processes, and what changed
// in the providers, which a branch's own status leaves out.
type StatusRequest struct {
	Branches []model.Branch
	States   []model.BranchState
	Port     string
	Overview bool
}

// StatusReport is everything status shows, gathered before anything is
// rendered, its text and its JSON written from it alone (Then's item 6,
// C8-6): each branch's status judged once, a stopped check marked, and,
// for the branches named, which status shows in detail, what their latest
// checks' builds link and don't declare, which evaluates their Portfiles;
// and the overview's parts, where asked.
type StatusReport struct {
	Statuses []BranchStatus
	// Links are what each branch's latest check's builds link and don't
	// declare, by the check; a check whose links couldn't be read has
	// none.
	Links map[model.RunID][]LinkFinding
	// Installed are what each such check's builds installed and configure
	// found against the port's earlier build (InstallChanges), by the
	// check, for the branches named alone.
	Installed map[model.RunID][]InstallFinding
	Serve     ServeState
	Queue     []QueuedCheck
	Processes []Process
	Changes   []buildenv.Change
	// Look is serve's last daily look under the selection configured now,
	// where it found any port to say (LastOutdatedLook); nil otherwise.
	Look *OutdatedLook
}

// StatusReport gathers what the request shows, as session observes.
func (e *Engine) StatusReport(ctx context.Context, session *coord.Session, request StatusRequest) (StatusReport, error) {
	var report StatusReport
	if len(request.Branches) > 0 {
		for _, branch := range request.Branches {
			status, err := e.BranchStatus(ctx, branch)
			if err != nil {
				return report, err
			}
			report.Statuses = append(report.Statuses, status)
		}
	} else {
		statuses, err := e.Status(ctx, request.States...)
		if err != nil {
			return report, err
		}
		if request.Port != "" {
			statuses = slices.DeleteFunc(statuses, func(s BranchStatus) bool { return !s.Touches(request.Port) })
		}
		report.Statuses = statuses
	}
	if err := e.JudgeStopped(ctx, session, report.Statuses); err != nil {
		return report, err
	}
	report.Links, report.Installed = map[model.RunID][]LinkFinding{}, map[model.RunID][]InstallFinding{}
	for _, status := range report.Statuses {
		if len(request.Branches) > 0 && status.Latest != nil && !status.Cleaned() {
			if links, err := e.UndeclaredLinks(ctx, *status.Latest); err == nil {
				report.Links[status.Latest.ID] = links
			}
			if installed, err := e.InstallChanges(ctx, *status.Latest, false); err == nil {
				report.Installed[status.Latest.ID] = installed
			}
		}
	}
	if !request.Overview {
		return report, nil
	}
	report.Changes = e.Changes(ctx)
	if found, ok := e.LastOutdatedLook(e.Serves); ok && (len(found.Outdated) > 0 || len(found.Uncertain) > 0) {
		report.Look = &found
	}
	var err error
	if report.Serve, err = e.ServeState(ctx, session); err != nil {
		return report, err
	}
	if report.Queue, err = e.Queue(ctx, session); err != nil {
		return report, err
	}
	report.Processes, err = e.Processes(ctx, session)
	return report, err
}
