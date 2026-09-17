package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

func (p *Provider) Observe(ctx context.Context, run record.ProviderRun) (verify.Observation, error) {
	o, v, data, err := p.openRun(ctx, run)
	if err != nil {
		return verify.Observation{}, err
	}
	defer o.close()
	return o.observe(ctx, v, data)
}

func (o *operation) observe(ctx context.Context, v record.ProviderExecution, data payload) (verify.Observation, error) {
	if v.State == record.ExecutionAdmitted {
		if err := o.removeInput(v); err != nil {
			return verify.Observation{}, err
		}
	}
	run := submission(v, verify.Admitted).Run
	if len(v.Result) > 0 {
		var result verify.Observation
		err := json.Unmarshal(v.Result, &result)
		return result, err
	}
	if result, ok, err := o.saved(v); err != nil {
		return verify.Observation{}, err
	} else if ok {
		return o.finish(ctx, v, result)
	}
	environment, err := o.environment(ctx, data)
	if err != nil {
		return verify.Observation{}, err
	}
	status, err := o.machine.Inspect(ctx, v.Resource)
	if err != nil {
		return verify.Observation{}, err
	}
	if status.State == "starting" {
		return verify.Observation{Run: run, State: record.AttemptRunning, Environment: environment, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}, nil
	}
	if status.State == "not-started" {
		if err = o.machine.Launch(ctx, v.Resource); err != nil {
			return verify.Observation{}, err
		}
		return verify.Observation{Run: run, State: record.AttemptRunning, Environment: environment, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}, nil
	}
	if status.State == "stopped" || status.State == "runner-exited" {
		result := verify.Observation{Run: run, State: record.AttemptFinished, Environment: environment, Verdict: record.VerdictErrored, Detail: "VM or guest runner stopped before a terminal result could be collected", ObservedAt: time.Now().UTC()}
		log := filepath.Join(o.directory(v), "build.log")
		if err := o.machine.Logs(ctx, v.Resource, log); err == nil {
			result.Logs = []record.Artifact{{Name: "build.log", Location: log, MediaType: "text/plain"}}
		}
		return o.finish(ctx, v, result)
	}
	if status.ID != string(v.ID) || status.Digest != data.Digest || status.Protocol != 1 {
		return verify.Observation{}, fmt.Errorf("tart: guest result identifies different inputs")
	}
	result := verify.Observation{Run: run, State: record.AttemptRunning, Environment: environment, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}
	if status.State == "running" {
		return result, nil
	}
	if status.State != "finished" {
		return verify.Observation{}, fmt.Errorf("tart: invalid guest lifecycle %q", status.State)
	}
	result.State = record.AttemptFinished
	result.Verdict = status.Verdict
	result.Steps = status.Steps
	result.TestOmission = status.TestOmission
	result.Environment.Guest = status.Environment
	result.Failure = status.Failure
	result.Detail = status.Detail
	if _, err = verify.Judge(result); err != nil {
		return verify.Observation{}, err
	}
	log := filepath.Join(o.directory(v), "build.log")
	if err = o.machine.Logs(ctx, v.Resource, log); err != nil {
		return verify.Observation{}, err
	}
	result.Logs = []record.Artifact{{Name: "build.log", Location: log, MediaType: "text/plain"}}
	return o.finish(ctx, v, result)
}
func (o *operation) finish(ctx context.Context, v record.ProviderExecution, result verify.Observation) (verify.Observation, error) {
	// Preserve evidence before powering off; a retry can finish stopping the VM.
	data, err := json.Marshal(result)
	if err != nil {
		return verify.Observation{}, err
	}
	path := filepath.Join(o.directory(v), "result.json")
	if err = atomicfile.Write(path, data, 0600); err != nil {
		return verify.Observation{}, err
	}
	if err = o.machine.Stop(ctx, v.Resource); err != nil {
		return verify.Observation{}, err
	}
	if err = o.removeInput(v); err != nil {
		return verify.Observation{}, err
	}
	v.Result = data
	v.Occupied = false
	if err = o.put(ctx, v); err != nil {
		return verify.Observation{}, err
	}
	return result, nil
}

func (o *operation) environment(ctx context.Context, data payload) (*record.EnvironmentEvidence, error) {
	capabilities, found, err := o.provider.cachedImageCapabilities(ctx, data.Request.Spec.Config.EnvironmentDigest)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("tart: admitted image has no capability observation")
	}
	if problem := capabilityProblem(capabilities, data.Config, data.Request.Spec.Config); problem != "" {
		return nil, fmt.Errorf("tart: admitted image capability conflict: %s", problem)
	}
	observed := environmentEvidence(capabilities)
	observed.Image, observed.ProviderVersion = data.Config.Image, data.ProviderVersion
	return observed, nil
}
func (o *operation) saved(v record.ProviderExecution) (verify.Observation, bool, error) {
	raw, err := os.ReadFile(filepath.Join(o.directory(v), "result.json"))
	if errors.Is(err, os.ErrNotExist) {
		return verify.Observation{}, false, nil
	}
	if err != nil {
		return verify.Observation{}, false, err
	}
	var result verify.Observation
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, false, err
	}
	if result.Run != (record.ProviderRun{Provider: verify.ProviderTart, RequestID: v.ID, RunID: v.Resource}) {
		return result, false, state.ErrConflict
	}
	_, err = verify.Judge(result)
	return result, err == nil, err
}
