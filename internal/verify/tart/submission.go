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
	"github.com/herbygillot/dockhand/internal/verify"
)

func (p *Provider) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	if err := validateRequest(request); err != nil {
		return verify.Submission{State: verify.Unsupported, Detail: err.Error()}, nil
	}
	ctx = progress.WithScope(ctx, string(request.AttemptID))
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
	defer o.close()
	previous, err := o.read(ctx, request.ID)
	if err == nil {
		if previous.State == record.ExecutionClosed || previous.State == record.ExecutionReleased && len(previous.Result) == 0 {
			return verify.Submission{}, ErrClosed
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
	} else if err != nil {
		return verify.Submission{}, err
	}
	if p.Repo == nil {
		return verify.Submission{}, fmt.Errorf("tart: source repository is required")
	}
	prepared, err := os.MkdirTemp(o.pool.Directory, ".preparing-")
	if err != nil {
		return verify.Submission{}, err
	}
	defer os.RemoveAll(prepared)
	archive, err := makeInput(ctx, p.Repo, request, o.config, prepared, p.HTTP)
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
	v := record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, AttemptID: request.AttemptID, Resource: "dockhand2-" + digest([]byte(o.pool.ID + "/" + string(request.ID)))[:24], Payload: raw, State: record.ExecutionReserved, Occupied: true, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	running, err := o.machine.Running(ctx)
	if err != nil {
		return verify.Submission{}, err
	}
	err = p.State.ProviderUpdate(ctx, o.pool.ID, func(ctx context.Context, tx state.ProviderTx) error {
		if err := capacityAvailable(ctx, tx, running, o.pool.Capacity); err != nil {
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
	progress.Report(ctx, "Tart capacity reserved for %s; cloning image %s", request.Spec.Target.Name, o.config.Image)
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
	progress.Report(ctx, "Starting verification VM and waiting for the guest agent")
	if err = o.machine.Start(ctx, v.Resource, directory); err != nil {
		return uncertain, err
	}
	if err = o.machine.Ready(ctx, v.Resource); err != nil {
		return uncertain, err
	}
	if !observed {
		progress.Report(ctx, "Inspecting guest verification prerequisites")
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
	progress.Report(ctx, "Transferring prepared source to the verification VM")
	if err = o.machine.Stage(ctx, v.Resource, archive); err != nil {
		return uncertain, err
	}
	// Once launch intent is admitted, recovery completes the same guest launch.
	v.State = record.ExecutionAdmitted
	if err = o.put(ctx, v); err != nil {
		return uncertain, err
	}
	progress.Report(ctx, "Launching verification")
	if err = o.machine.Launch(ctx, v.Resource); err != nil {
		return uncertain, err
	}
	progress.Report(ctx, "Verification launched")
	return submission(v, verify.Admitted), nil
}
