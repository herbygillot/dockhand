package tart

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

func (p *Provider) Reconcile(ctx context.Context, id record.RequestID, _ verify.ReconcileOptions) (verify.Reconciliation, error) {
	o, err := p.begin(ctx, id)
	if err != nil {
		return verify.Reconciliation{}, err
	}
	defer o.close()
	v, err := o.read(ctx, id)
	if errors.Is(err, state.ErrNotFound) {
		v = record.ProviderExecution{ID: id, RepositoryID: p.Repository, State: record.ExecutionClosed, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
		err = o.put(ctx, v)
		return verify.Reconciliation{State: verify.RequestClosed}, err
	}
	if err != nil {
		return verify.Reconciliation{}, err
	}
	if v.State == record.ExecutionAdmitted || v.State == record.ExecutionReleased && len(v.Result) > 0 {
		return verify.Reconciliation{State: verify.RunFound, Submission: submission(v, verify.Admitted)}, nil
	}
	if v.State == record.ExecutionReserved {
		if _, err = o.restore(v); err != nil {
			return verify.Reconciliation{}, err
		}
		if result, ok, savedErr := o.saved(v); savedErr != nil {
			return verify.Reconciliation{State: verify.RunUnknown}, savedErr
		} else if ok {
			v.State = record.ExecutionAdmitted
			if _, err = o.finish(ctx, v, result); err != nil {
				return verify.Reconciliation{State: verify.RunUnknown}, err
			}
			return verify.Reconciliation{State: verify.RunFound, Submission: submission(v, verify.Admitted)}, nil
		}
		if err = o.machine.Stop(ctx, v.Resource); err != nil {
			return verify.Reconciliation{State: verify.RunUnknown}, err
		}
		v.State, v.Occupied = record.ExecutionClosed, false
		if err = o.put(ctx, v); err != nil {
			return verify.Reconciliation{State: verify.RunUnknown}, err
		}
	}
	return verify.Reconciliation{State: verify.RequestClosed, Submission: submission(v, verify.SubmissionUncertain)}, nil
}

func (p *Provider) Cancel(ctx context.Context, run record.ProviderRun) error {
	o, v, data, err := p.openRun(ctx, run)
	if err != nil {
		return err
	}
	defer o.close()
	if len(v.Result) > 0 {
		return nil
	}
	if result, ok, err := o.saved(v); err != nil {
		return err
	} else if ok {
		_, err = o.finish(ctx, v, result)
		return err
	}
	if status, err := o.machine.Inspect(ctx, v.Resource); err == nil && status.State == "finished" {
		_, err = o.observe(ctx, v, data)
		return err
	}
	environment, err := o.environment(ctx, data)
	if err != nil {
		return err
	}
	log := filepath.Join(o.directory(v), "build.log")
	result := verify.Observation{Run: run, State: record.AttemptCanceled, Environment: environment, Verdict: record.VerdictCanceled, Detail: "VM stopped by cancellation", ObservedAt: time.Now().UTC()}
	if err = o.machine.Logs(ctx, v.Resource, log); err == nil {
		result.Logs = []record.Artifact{{Name: "build.log", Location: log, MediaType: "text/plain"}}
	}
	_, err = o.finish(ctx, v, result)
	return err
}
func (p *Provider) Release(ctx context.Context, handle record.ResourceHandle) (verify.ReleaseResult, error) {
	if handle.Provider != verify.ProviderTart {
		return verify.ReleaseResult{}, state.ErrInvalid
	}
	o, err := p.begin(ctx, record.RequestID(handle.ID))
	if err != nil {
		return verify.ReleaseResult{}, err
	}
	defer o.close()
	v, err := o.read(ctx, record.RequestID(handle.ID))
	if err != nil {
		return verify.ReleaseResult{}, err
	}
	if v.State == record.ExecutionReleased {
		return verify.ReleaseResult{Confirmed: true}, nil
	}
	if v.State != record.ExecutionClosed && len(v.Result) == 0 {
		return verify.ReleaseResult{}, fmt.Errorf("tart: execution has no confirmed terminal result")
	}
	if v.Resource != "" {
		if _, err = o.restore(v); err != nil {
			return verify.ReleaseResult{}, err
		}
		if err = o.machine.Stop(ctx, v.Resource); err != nil {
			return verify.ReleaseResult{}, err
		}
		if err = o.machine.Delete(ctx, v.Resource); err != nil {
			return verify.ReleaseResult{}, err
		}
		if err = o.removeInput(v); err != nil {
			return verify.ReleaseResult{}, err
		}
	}
	v.State, v.Occupied = record.ExecutionReleased, false
	if err = o.put(ctx, v); err != nil {
		return verify.ReleaseResult{}, err
	}
	return verify.ReleaseResult{Confirmed: true}, nil
}
