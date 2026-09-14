package tart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

var ErrClosed = errors.New("tart: submission is permanently closed")
var errCapacity = errors.New("tart: pool is at capacity")

const ProviderName = "tart"

type Config struct {
	Executable        string
	Image             string
	ArtifactDirectory string
	Home              string
	Capacity          int
	Platform          record.Platform
	GuestPrefix       string
}

type Environment struct {
	Digest   string
	Platform record.Platform
}
type Provider struct {
	Config     Config
	State      state.ProviderStore
	Repository record.RepositoryID
	Repo       *git.Repository
	backend    machine
	images     imageCache
}

type payload struct {
	Request verify.Request
	Config  Config
	Digest  string
}

type operation struct {
	provider *Provider
	config   Config
	pool     record.ProviderPool
	lock     *os.File
	machine  machine
}

type machine interface {
	Environment(context.Context) (Environment, error)
	Running(context.Context) ([]string, error)
	Clone(context.Context, string, string) error
	Start(context.Context, string, string) error
	Ready(context.Context, string) error
	Stage(context.Context, string, string) error
	Launch(context.Context, string) error
	Inspect(context.Context, string) (guestResult, error)
	Logs(context.Context, string, string) error
	Stop(context.Context, string) error
	Delete(context.Context, string) error
}

func (p *Provider) settings() (Config, error) { return settings(p.Config) }

func settings(c Config) (Config, error) {
	if c.Executable == "" {
		c.Executable = "tart"
	}
	if c.Capacity == 0 {
		c.Capacity = 2
	}
	if c.GuestPrefix == "" {
		c.GuestPrefix = "/opt/local"
	}
	if c.Home == "" {
		c.Home = os.Getenv("TART_HOME")
	}
	if c.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return c, err
		}
		c.Home = filepath.Join(home, ".tart")
	}
	var err error
	c.Home, err = filepath.Abs(c.Home)
	if err != nil {
		return c, err
	}
	c.Home, err = filepath.EvalSymlinks(c.Home)
	if err != nil {
		return c, err
	}
	if c.ArtifactDirectory == "" || !filepath.IsAbs(c.GuestPrefix) || c.Capacity < 1 || strings.ContainsAny(c.GuestPrefix, "\x00\r\n") {
		return c, fmt.Errorf("tart: artifact directory, positive capacity, and an absolute guest prefix are required")
	}
	c.ArtifactDirectory, err = filepath.Abs(c.ArtifactDirectory)
	if err != nil {
		return c, err
	}
	if err = os.MkdirAll(c.ArtifactDirectory, 0700); err != nil {
		return c, err
	}
	c.ArtifactDirectory, err = filepath.EvalSymlinks(c.ArtifactDirectory)
	return c, err
}
func (p *Provider) machineFor(c Config, guard *os.File) machine {
	if p.backend != nil {
		return p.backend
	}
	return &native{config: c, guard: guard, images: &p.images, cache: p.State}
}
func (p *Provider) DescribeEnvironment(ctx context.Context) (Environment, error) {
	c, err := p.settings()
	if err != nil {
		return Environment{}, err
	}
	return p.machineFor(c, nil).Environment(ctx)
}
func (p *Provider) Capabilities(ctx context.Context) (verify.Capabilities, error) {
	c, err := p.settings()
	if err != nil {
		return verify.Capabilities{}, err
	}

	if _, err = p.machineFor(c, nil).Running(ctx); err != nil {
		return verify.Capabilities{}, err
	}
	var platforms []record.Platform
	if c.Platform != (record.Platform{}) {
		platforms = []record.Platform{c.Platform}
	}
	return verify.Capabilities{Name: ProviderName, Platforms: platforms, Isolated: true, Capacity: c.Capacity}, nil
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func requestID(id record.RequestID) bool {
	return id != "" && !strings.ContainsAny(string(id), "\x00\r\n\t ")
}

func (p *Provider) begin(ctx context.Context, id record.RequestID) (*operation, error) {
	return p.beginWith(ctx, id, p.Config)
}

func (p *Provider) beginWith(ctx context.Context, id record.RequestID, config Config) (*operation, error) {
	if p.State == nil || p.Repository == "" || !requestID(id) {
		return nil, state.ErrInvalid
	}
	c, err := settings(config)
	if err != nil {
		return nil, err
	}
	pool := record.ProviderPool{ID: "tart_" + digest([]byte(c.Home)), Scope: "tart:" + c.Home, Directory: c.ArtifactDirectory, Capacity: c.Capacity}
	if config.Capacity == 0 {
		existing, e := p.State.ProviderPool(ctx, pool.ID)
		if e == nil {
			pool.Capacity, c.Capacity = existing.Capacity, existing.Capacity
		} else if !errors.Is(e, state.ErrNotFound) {
			return nil, e
		}
	}
	pool, err = p.State.RegisterProviderPool(ctx, pool)
	if err != nil {
		return nil, err
	}
	lock, err := acquire(ctx, filepath.Join(pool.Directory, "locks", digest([]byte(string(id)))+".lock"))
	if err != nil {
		return nil, err
	}
	return &operation{provider: p, config: c, pool: pool, lock: lock, machine: p.machineFor(c, lock)}, nil
}
func (o *operation) close() { _ = o.lock.Close() }
func (o *operation) read(ctx context.Context, id record.RequestID) (record.ProviderExecution, error) {
	var v record.ProviderExecution
	err := o.provider.State.ProviderView(ctx, o.pool.ID, func(ctx context.Context, r state.ProviderReader) error {
		var err error
		v, err = r.Execution(ctx, id)
		return err
	})
	if err == nil && v.RepositoryID != o.provider.Repository {
		return record.ProviderExecution{}, state.ErrConflict
	}
	return v, err
}
func (o *operation) put(ctx context.Context, v record.ProviderExecution) error {
	return o.provider.State.ProviderUpdate(ctx, o.pool.ID, func(ctx context.Context, tx state.ProviderTx) error { return tx.PutExecution(ctx, v) })
}
func (o *operation) restore(v record.ProviderExecution) (payload, error) {
	var data payload
	if err := json.Unmarshal(v.Payload, &data); err != nil {
		return data, err
	}
	if data.Config.Home != o.config.Home || data.Config.ArtifactDirectory != o.config.ArtifactDirectory {
		return data, state.ErrConflict
	}
	o.config = data.Config
	o.machine = o.provider.machineFor(data.Config, o.lock)
	return data, nil
}
func (o *operation) directory(v record.ProviderExecution) string {
	return filepath.Join(o.pool.Directory, v.Resource)
}
func submission(v record.ProviderExecution, status verify.SubmissionState) verify.Submission {
	result := verify.Submission{State: status}
	if v.Resource != "" {
		result.Resources = []record.ResourceHandle{{Provider: ProviderName, ID: string(v.ID)}}
	}
	if status == verify.Admitted {
		result.Run = record.ProviderRun{Provider: ProviderName, RequestID: v.ID, RunID: v.Resource}
	}
	return result
}

func (p *Provider) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	if err := validateRequest(request); err != nil {
		return verify.Submission{State: verify.Unsupported, Detail: err.Error()}, nil
	}
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
	raw, _ := json.Marshal(payload{Request: request, Config: o.config, Digest: buildDigest(request.Spec)})
	v := record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, AttemptID: request.AttemptID, Resource: "dockhand2-" + digest([]byte(o.pool.ID + "/" + string(request.ID)))[:24], Payload: raw, State: record.ExecutionReserved, Occupied: true, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	running, err := o.machine.Running(ctx)
	if err != nil {
		return verify.Submission{}, err
	}
	err = p.State.ProviderUpdate(ctx, o.pool.ID, func(ctx context.Context, tx state.ProviderTx) error {
		occupied, err := tx.Occupied(ctx)
		if err != nil {
			return err
		}
		names := make(map[string]bool)
		for _, name := range running {
			names[name] = true
		}
		for _, run := range occupied {
			names[run.Resource] = true
		}
		if len(names) >= o.pool.Capacity {
			return errCapacity
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
	if err = o.machine.Start(ctx, v.Resource, directory); err != nil {
		return uncertain, err
	}
	if err = o.machine.Ready(ctx, v.Resource); err != nil {
		return uncertain, err
	}
	if p.Repo == nil {
		return uncertain, fmt.Errorf("tart: source repository is required")
	}
	archive, err := makeInput(ctx, p.Repo, request, o.config, directory)
	if err != nil {
		return uncertain, err
	}
	if err = o.machine.Stage(ctx, v.Resource, archive); err != nil {
		return uncertain, err
	}
	// Once launch intent is admitted, recovery completes the same guest launch.
	v.State = record.ExecutionAdmitted
	if err = o.put(ctx, v); err != nil {
		return uncertain, err
	}
	if err = o.machine.Launch(ctx, v.Resource); err != nil {
		return uncertain, err
	}
	return submission(v, verify.Admitted), nil
}

func (p *Provider) Reconcile(ctx context.Context, id record.RequestID) (verify.Reconciliation, error) {
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

func (p *Provider) Observe(ctx context.Context, run record.ProviderRun) (verify.Observation, error) {
	o, v, data, err := p.openRun(ctx, run)
	if err != nil {
		return verify.Observation{}, err
	}
	defer o.close()
	return o.observe(ctx, v, data)
}

func (o *operation) observe(ctx context.Context, v record.ProviderExecution, data payload) (verify.Observation, error) {
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
	status, err := o.machine.Inspect(ctx, v.Resource)
	if err != nil {
		return verify.Observation{}, err
	}
	if status.State == "starting" {
		return verify.Observation{Run: run, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}, nil
	}
	if status.State == "not-started" {
		if err = o.machine.Launch(ctx, v.Resource); err != nil {
			return verify.Observation{}, err
		}
		return verify.Observation{Run: run, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}, nil
	}
	if status.State == "stopped" || status.State == "runner-exited" {
		result := verify.Observation{Run: run, State: record.AttemptFinished, Verdict: record.VerdictErrored, Detail: "VM or guest runner stopped before a terminal result could be collected", ObservedAt: time.Now().UTC()}
		log := filepath.Join(o.directory(v), "build.log")
		if err := o.machine.Logs(ctx, v.Resource, log); err == nil {
			result.Logs = []record.Artifact{{Name: "build.log", Location: log, MediaType: "text/plain"}}
		}
		return o.finish(ctx, v, result)
	}
	if status.ID != string(v.ID) || status.Digest != data.Digest || status.Protocol != 1 {
		return verify.Observation{}, fmt.Errorf("tart: guest result identifies different inputs")
	}
	result := verify.Observation{Run: run, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}
	if status.State == "running" {
		return result, nil
	}
	if status.State != "finished" {
		return verify.Observation{}, fmt.Errorf("tart: invalid guest lifecycle %q", status.State)
	}
	result.State = record.AttemptFinished
	result.Verdict = status.Verdict
	result.Steps = status.Steps
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
	if err = atomicFile(path, data, 0600); err != nil {
		return verify.Observation{}, err
	}
	if err = o.machine.Stop(ctx, v.Resource); err != nil {
		return verify.Observation{}, err
	}
	v.Result = data
	v.Occupied = false
	if err = o.put(ctx, v); err != nil {
		return verify.Observation{}, err
	}
	return result, nil
}
func (p *Provider) openRun(ctx context.Context, run record.ProviderRun) (*operation, record.ProviderExecution, payload, error) {
	if run.Provider != ProviderName {
		return nil, record.ProviderExecution{}, payload{}, state.ErrInvalid
	}
	o, err := p.begin(ctx, run.RequestID)
	if err != nil {
		return nil, record.ProviderExecution{}, payload{}, err
	}
	v, err := o.read(ctx, run.RequestID)
	if err == nil && (v.Resource != run.RunID || (v.State != record.ExecutionAdmitted && v.State != record.ExecutionReleased)) {
		err = state.ErrConflict
	}
	var data payload
	if err == nil {
		data, err = o.restore(v)
	}
	if err != nil {
		o.close()
		return nil, v, data, err
	}
	return o, v, data, nil
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
	log := filepath.Join(o.directory(v), "build.log")
	result := verify.Observation{Run: run, State: record.AttemptCanceled, Verdict: record.VerdictCanceled, Detail: "VM stopped by cancellation", ObservedAt: time.Now().UTC()}
	if err = o.machine.Logs(ctx, v.Resource, log); err == nil {
		result.Logs = []record.Artifact{{Name: "build.log", Location: log, MediaType: "text/plain"}}
	}
	_, err = o.finish(ctx, v, result)
	return err
}
func (p *Provider) Release(ctx context.Context, handle record.ResourceHandle) (verify.ReleaseResult, error) {
	if handle.Provider != ProviderName {
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
		if err = os.Remove(filepath.Join(o.directory(v), "input.tar")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return verify.ReleaseResult{}, err
		}
	}
	v.State, v.Occupied = record.ExecutionReleased, false
	if err = o.put(ctx, v); err != nil {
		return verify.ReleaseResult{}, err
	}
	return verify.ReleaseResult{Confirmed: true}, nil
}
func buildDigest(spec record.BuildSpec) string { raw, _ := json.Marshal(spec); return digest(raw) }
func validateRequest(r verify.Request) error {
	if !requestID(r.ID) || r.AttemptID == "" || r.Spec.Config.Provider != ProviderName || len(r.Spec.Inputs) != 0 {
		return fmt.Errorf("tart: one concrete verification target without artifact inputs is required")
	}
	if err := verify.ValidateConfig(r.Spec.Config); err != nil {
		return err
	}
	if (r.Spec.Source.Commit != "" && !git.ValidObjectID(string(r.Spec.Source.Commit))) || !git.ValidObjectID(string(r.Spec.Source.Tree)) {
		return fmt.Errorf("tart: immutable source tree and an optional valid commit are required")
	}
	if r.Spec.Config.VerifierDigest != "" && r.Spec.Config.VerifierDigest != verifierDigest() {
		return fmt.Errorf("tart: verifier implementation changed; submit a new verification request")
	}
	target := r.Spec.Target
	if !safeToken(target.Name) || (target.Subport != "" && !safeToken(target.Subport)) || !validPortfile(target.Portfile) {
		return fmt.Errorf("tart: invalid target")
	}
	for variant := range target.Variants {
		if !safeToken(variant) || strings.HasPrefix(variant, "+") || strings.HasPrefix(variant, "-") {
			return fmt.Errorf("tart: invalid variant")
		}
	}
	return nil
}

var _ verify.Provider = (*Provider)(nil)

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
	if result.Run != (record.ProviderRun{Provider: ProviderName, RequestID: v.ID, RunID: v.Resource}) {
		return result, false, state.ErrConflict
	}
	_, err = verify.Judge(result)
	return result, err == nil, err
}

func (p *Provider) BuildConfig(ctx context.Context, platform record.Platform, tests record.TestPolicy, fromSource bool) (record.BuildConfig, error) {
	c, err := p.settings()
	if err != nil {
		return record.BuildConfig{}, err
	}
	if c.Image == "" {
		c.Image, err = DefaultImageName(platform)
		if err != nil {
			return record.BuildConfig{}, err
		}
	}
	c.Platform = platform
	if p.State != nil {
		pool, e := p.State.ProviderPool(ctx, "tart_"+digest([]byte(c.Home)))
		if e == nil && (pool.Directory != c.ArtifactDirectory || p.Config.Capacity != 0 && pool.Capacity != p.Config.Capacity) {
			return record.BuildConfig{}, fmt.Errorf("tart: configuration differs from the existing pool")
		}
		if e != nil && !errors.Is(e, state.ErrNotFound) {
			return record.BuildConfig{}, e
		}
	}
	environment, err := p.machineFor(c, nil).Environment(ctx)
	if err != nil {
		if p.Config.Image == "" {
			return record.BuildConfig{}, fmt.Errorf("tart: default image %s is unavailable; run dockhand setup or select --image: %w", c.Image, err)
		}
		return record.BuildConfig{}, err
	}
	// Capacity is pool policy; zero permits an existing pool's recorded limit.
	c.Capacity = p.Config.Capacity
	raw, err := json.Marshal(c)
	if err != nil {
		return record.BuildConfig{}, err
	}
	config := record.BuildConfig{Provider: ProviderName, Platform: platform, EnvironmentDigest: environment.Digest, VerifierDigest: verifierDigest(), ProviderConfig: raw, Tests: tests, FromSource: fromSource}
	return config, verify.ValidateConfig(config)
}
