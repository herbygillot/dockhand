package tart

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

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
	if err := os.MkdirAll(c.ArtifactDirectory, 0700); err != nil {
		return nil, err
	}
	pool := poolOf(c)
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
	lock, err := filelock.Acquire(ctx, filelock.Path(filepath.Join(pool.Directory, "locks"), string(id)), filelock.Exclusive)
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
		result.Resources = []record.ResourceHandle{{Provider: verify.ProviderTart, ID: string(v.ID)}}
	}
	if status == verify.Admitted {
		result.Run = record.ProviderRun{Provider: verify.ProviderTart, RequestID: v.ID, RunID: v.Resource}
	}
	return result
}

func (p *Provider) openRun(ctx context.Context, run record.ProviderRun) (*operation, record.ProviderExecution, payload, error) {
	if run.Provider != verify.ProviderTart {
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
func buildDigest(spec record.BuildSpec) string { raw, _ := json.Marshal(spec); return digest(raw) }
func digest(data []byte) string                { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// poolOf is the execution pool a resolved configuration shares: every driver
// on the same Tart home coordinates through it.
func poolOf(c Config) record.ProviderPool {
	return record.ProviderPool{ID: "tart_" + digest([]byte(c.Home)), Scope: "tart:" + c.Home, Directory: c.ArtifactDirectory, Capacity: c.Capacity}
}

// Pool resolves the configuration and names its execution pool, with the
// capacity the configuration carries, or the default for a pool not yet
// recorded.
func Pool(config Config) (record.ProviderPool, error) {
	c, err := settings(config)
	if err != nil {
		return record.ProviderPool{}, err
	}
	return poolOf(c), nil
}

type payload struct {
	ProviderVersion string `json:",omitempty"`
	Request         verify.Request
	Config          Config
	Digest          string
}

type operation struct {
	provider *Provider
	config   Config
	pool     record.ProviderPool
	lock     *os.File
	machine  machine
}
