package eval

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
)

type machineReadKey struct{}

//go:embed machine_read.tcl
var machineReadOverrides string

func machineReadScript(ctx context.Context) string {
	if enabled, _ := ctx.Value(machineReadKey{}).(bool); enabled {
		return machineReadOverrides
	}
	return ""
}

// MachineReadExperiments never publishes a second observation. The operation
// owns a fresh interpreter, with no ledger, bootstrap, or progress subscribers.
func (e *Evaluator) MachineReadExperiments(ctx context.Context, source macports.Context, request macports.ObservationRequest, before macports.Observation, candidates map[string][]int, acted []string) map[string]macports.MachineReadExperiment {
	private := &Evaluator{Executable: e.Executable, Env: e.Env, Model: e.Model, Adapter: e.Adapter, Installation: e.Installation, Installed: e.Installed}
	return performMachineReadExperiment(ctx, before, candidates, acted, func(ctx context.Context) (macports.Observation, error) {
		return private.Observe(context.WithValue(ctx, machineReadKey{}, true), source, request)
	})
}

func performMachineReadExperiment(ctx context.Context, before macports.Observation, candidates map[string][]int, acted []string, operation func(context.Context) (macports.Observation, error)) map[string]macports.MachineReadExperiment {
	after, err := operation(progress.WithoutReports(ctx))
	result := make(map[string]macports.MachineReadExperiment, len(candidates))
	for name, ids := range candidates {
		summary := macports.MachineReadExperiment{Outcome: "not perturbed"}
		if err != nil {
			summary.Outcome, summary.Field, summary.ErrorCode = "differs", "evaluation", "check-inconclusive"
			if errors.Is(err, macports.ErrStartup) || errors.Is(err, rpc.ErrBrokenSession) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				summary.Outcome, summary.ErrorCode = "failed", "evaluation-unavailable"
			}
		} else if second, ok := after.Ports[name]; !ok {
			summary.Outcome, summary.ErrorCode = "failed", "experiment-incomplete"
		} else {
			for _, id := range ids {
				if id < 0 || id >= len(before.Ports[name].Declarations) {
					continue
				}
				original := before.Ports[name].Declarations[id]
				if len(original.Values) < 4 || original.Values[1] != "closed" {
					continue
				}
				if slices.ContainsFunc(second.Declarations, func(d macports.Declaration) bool {
					return d.Command == "dockhand.host-access" && len(d.Values) >= 4 && d.Values[1] == "standin" && d.Values[3] == original.Values[3]
				}) {
					summary.Varied = append(summary.Varied, id)
				}
			}
			if len(summary.Varied) > 0 {
				summary.Field, summary.BuildOnlyMoved = compareMachinePlans(before.Snapshot.Ports[name], after.Snapshot.Ports[name], before.Ports[name], second, acted)
				summary.Outcome = "same"
				if summary.Field != "" {
					summary.Outcome = "differs"
				} else if guard := machinePhaseGuard(before.Ports[name]); guard != "" {
					summary.Outcome = "unexplained"
					summary.ErrorCode = "checksum-phase-unread"
				} else if guard := machinePhaseGuard(second); guard != "" {
					summary.Outcome = "unexplained"
					summary.ErrorCode = "checksum-phase-unread"
				} else if len(summary.BuildOnlyMoved) > 0 {
					summary.Outcome = "same, build-only moved"
				}
			}
		}
		result[name] = summary
	}
	return result
}
