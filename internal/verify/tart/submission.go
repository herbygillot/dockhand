package tart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
)

func (p *Provider) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	if err := validateRequest(request); err != nil {
		return verify.Submission{State: verify.Unsupported, Detail: err.Error()}, nil
	}
	ctx = progress.WithScope(ctx, request.Spec.Target.Name)
	config := p.Config
	if len(request.Spec.Config.ProviderConfig) > 0 {
		config = Config{}
		if err := json.Unmarshal(request.Spec.Config.ProviderConfig, &config); err != nil {
			return verify.Submission{State: verify.Unsupported, Detail: "invalid Tart configuration"}, nil
		}
	}
	o, err := p.beginWith(ctx, request.ID, config)
	if err != nil {
		return verify.Submission{}, err
	}
	defer o.entry.Close()
	previous, err := o.entry.Read(ctx)
	if err == nil {
		if ledger.SubmissionClosed(previous) {
			return verify.Submission{}, fmt.Errorf("tart: %w", ledger.ErrClosed)
		}
		data, e := o.restore(previous)
		if e != nil {
			return verify.Submission{}, e
		}
		a, _ := json.Marshal(data.Request)
		b, _ := json.Marshal(request)
		if !bytes.Equal(a, b) {
			return verify.Submission{}, state.ErrConflict
		}
		if previous.State == record.ExecutionAdmitted || previous.State == record.ExecutionReleased {
			if previous.State == record.ExecutionAdmitted && len(previous.Result) == 0 {
				if e := o.removeInput(previous); e != nil {
					return submission(previous, verify.SubmissionUncertain), e
				}
				if e := o.machine.Launch(ctx, previous.Resource); e != nil {
					return submission(previous, verify.SubmissionUncertain), e
				}
			}
			return submission(previous, verify.Admitted), nil
		}
		return submission(previous, verify.SubmissionUncertain), nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return verify.Submission{}, err
	}
	if o.config.Image == "" || o.config.Platform != request.Spec.Config.Platform {
		return verify.Submission{State: verify.Unsupported, Detail: "no prepared image for the requested platform"}, nil
	}
	env, err := o.machine.Environment(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return verify.Submission{}, ctx.Err()
		}
		return verify.Submission{State: verify.Unsupported, Detail: err.Error()}, nil
	}
	if env.Digest != request.Spec.Config.EnvironmentDigest || env.Platform != request.Spec.Config.Platform {
		return verify.Submission{State: verify.Unsupported, Detail: "prepared image does not match the accepted build environment"}, nil
	}
	capabilities, observed, err := p.cachedImageCapabilities(ctx, env.Digest)
	if err != nil {
		return verify.Submission{}, err
	}
	if observed {
		if problem := capabilityProblem(capabilities, o.config, request.Spec.Config); problem != "" {
			return verify.Submission{State: verify.Unsupported, Detail: problem}, nil
		}
	}
	if err := o.checkCapacity(ctx); errors.Is(err, errCapacity) {
		return verify.Submission{State: verify.AtCapacity}, nil
	} else if errors.Is(err, tartvm.ErrListingBlocked) {
		return verify.Submission{State: verify.AtCapacity, Detail: listingBlocked}, nil
	} else if err != nil {
		return verify.Submission{}, err
	}
	if p.Repo == nil {
		return verify.Submission{}, fmt.Errorf("tart: source repository is required")
	}
	prepared, err := os.MkdirTemp(o.entry.Pool.Directory, ".preparing-")
	if err != nil {
		return verify.Submission{}, err
	}
	defer os.RemoveAll(prepared)
	archive, err := makeInput(ctx, p.Repo, p.Workspaces, request, o.config, p.IndexCache, prepared, p.HTTP)
	if err != nil {
		return verify.Submission{}, err
	}
	data := payload{Request: request, Config: o.config, Digest: buildDigest(request.Spec)}
	// Freeze diagnostic metadata with the immutable submission payload.
	data.ProviderVersion, _ = o.machine.Version(ctx)
	if err := ctx.Err(); err != nil {
		return verify.Submission{}, err
	}
	raw, _ := json.Marshal(data)
	v := record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, AttemptID: request.AttemptID, Resource: "dockhand2-" + record.Digest([]byte(o.entry.Pool.ID + "/" + string(request.ID)))[:24], Payload: raw, State: record.ExecutionReserved, Occupied: true, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	running, err := o.machine.Running(ctx)
	if errors.Is(err, tartvm.ErrListingBlocked) {
		return verify.Submission{State: verify.AtCapacity, Detail: listingBlocked}, nil
	}
	if err != nil {
		return verify.Submission{}, err
	}
	err = o.entry.Update(ctx, func(ctx context.Context, tx state.ProviderTx) error {
		if err := capacityAvailable(ctx, tx, running, o.entry.Pool.Capacity); err != nil {
			return err
		}
		return tx.PutExecution(ctx, v)
	})
	if errors.Is(err, errCapacity) {
		return verify.Submission{State: verify.AtCapacity}, nil
	}
	if err != nil {
		return verify.Submission{}, err
	}
	uncertain := submission(v, verify.SubmissionUncertain)
	progress.VerboseReport(ctx, "Tart capacity reserved for %s; cloning image %s", request.Spec.Target.Name, o.config.Image)
	if err = o.machine.Clone(ctx, o.config.Image, v.Resource); err != nil {
		return uncertain, err
	}
	// A stopped image can still be edited by another program while cloning.
	after, err := o.machine.Environment(ctx)
	if err != nil {
		return uncertain, err
	}
	if after != env {
		return uncertain, fmt.Errorf("tart: base image changed during clone")
	}
	directory := o.directory(v)
	if err = os.MkdirAll(directory, 0700); err != nil {
		return uncertain, err
	}
	inputPath := filepath.Join(directory, "input.tar")
	if err = os.Rename(archive, inputPath); err != nil {
		return uncertain, err
	}
	archive = inputPath
	progress.VerboseReport(ctx, "Starting verification VM and waiting for the guest agent")
	if err = o.machine.Start(ctx, v.Resource, directory); err != nil {
		return uncertain, err
	}
	if err = o.machine.Ready(ctx, v.Resource, directory); errors.Is(err, errVMLimit) {
		return o.withdraw(ctx, v, err)
	} else if err != nil {
		return uncertain, err
	}
	if !observed {
		progress.DebugReport(ctx, "Inspecting guest verification prerequisites")
		inspection, inspectErr := o.machine.InspectCapabilities(ctx, v.Resource, o.config.GuestPrefix)
		if inspectErr != nil {
			return uncertain, inspectErr
		}
		capabilities = newImageCapabilities(env.Digest, inspection)
		if err = p.State.PutImageCapabilities(ctx, capabilities); err != nil {
			return uncertain, err
		}
		if problem := capabilityProblem(capabilities, o.config, request.Spec.Config); problem != "" {
			v.State = record.ExecutionAdmitted
			result := verify.Observation{
				Run: submission(v, verify.Admitted).Run, State: record.AttemptFinished, Environment: environmentEvidence(capabilities),
				Verdict: record.VerdictBlocked, Detail: problem, ObservedAt: time.Now().UTC(),
				Failure: &record.Failure{Kind: record.InfrastructureFailure, Package: request.Spec.Target.Name, Phase: "setup", Attribution: record.AttributionUnknown, Detail: problem},
			}
			if _, err = o.finish(ctx, v, result); err != nil {
				return uncertain, err
			}
			return submission(v, verify.Admitted), nil
		}
	}
	progress.DebugReport(ctx, "Transferring prepared source to the verification VM")
	if err = o.machine.Stage(ctx, v.Resource, archive); err != nil {
		return uncertain, err
	}
	// Once launch intent is admitted, recovery completes the same guest launch.
	v.State = record.ExecutionAdmitted
	if err = o.entry.Put(ctx, v); err != nil {
		return uncertain, err
	}
	if err = o.removeInput(v); err != nil {
		return uncertain, err
	}
	progress.DebugReport(ctx, "Launching verification")
	if err = o.machine.Launch(ctx, v.Resource); err != nil {
		return uncertain, err
	}
	progress.VerboseReport(ctx, "Verification launched")
	return submission(v, verify.Admitted), nil
}

// withdraw undoes a reservation whose clone the Mac would not start at its
// limit of two macOS VMs: the clone is stopped and deleted, and the request
// released with no result, which closes it to submission with its clone
// already gone, so reconciliation reports it closed with nothing left and
// the workflow queues a new submission, waiting as it waits for capacity.
func (o *operation) withdraw(ctx context.Context, v record.ProviderExecution, cause error) (verify.Submission, error) {
	uncertain := submission(v, verify.SubmissionUncertain)
	if err := o.machine.Stop(ctx, v.Resource); err != nil {
		return uncertain, errors.Join(cause, err)
	}
	if err := o.machine.Delete(ctx, v.Resource); err != nil {
		return uncertain, errors.Join(cause, err)
	}
	_ = os.RemoveAll(o.directory(v))
	v.State, v.Occupied = record.ExecutionReleased, false
	if err := o.entry.Put(ctx, v); err != nil {
		return uncertain, errors.Join(cause, err)
	}
	return verify.Submission{State: verify.SubmissionUncertain, Detail: "Waiting: " + cause.Error()}, nil
}
