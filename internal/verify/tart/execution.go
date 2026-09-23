package tart

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
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
	entry, err := p.executions().Open(ctx, pool, id)
	if err != nil {
		return nil, err
	}
	return &operation{provider: p, config: c, entry: entry, machine: p.machineFor(c, entry.Lock)}, nil
}

// executions is the provider's ledger: its execution rows for the repository.
func (p *Provider) executions() ledger.Ledger {
	return ledger.Ledger{Store: p.State, Repository: p.Repository}
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
	o.machine = o.provider.machineFor(data.Config, o.entry.Lock)
	return data, nil
}
func (o *operation) directory(v record.ProviderExecution) string {
	return filepath.Join(o.entry.Pool.Directory, v.Resource)
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
	v, err := o.entry.Read(ctx)
	if err == nil && (v.Resource != run.RunID || (v.State != record.ExecutionAdmitted && v.State != record.ExecutionReleased)) {
		err = state.ErrConflict
	}
	var data payload
	if err == nil {
		data, err = o.restore(v)
	}
	if err != nil {
		o.entry.Close()
		return nil, v, data, err
	}
	return o, v, data, nil
}
func buildDigest(spec record.BuildSpec) string {
	raw, _ := json.Marshal(spec)
	return record.Digest(raw)
}

// poolOf is the execution pool a resolved configuration shares: every driver
// on the same Tart home coordinates through it.
func poolOf(c Config) record.ProviderPool {
	return record.ProviderPool{ID: "tart_" + record.Digest([]byte(c.Home)), Scope: "tart:" + c.Home, Directory: c.ArtifactDirectory, Capacity: c.Capacity}
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
	entry    *ledger.Entry
	machine  machine
}
