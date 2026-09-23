package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

var _ verify.ArtifactPruner = (*Provider)(nil)

func (p *Provider) PruneArtifacts(ctx context.Context, handle record.ResourceHandle) error {
	if handle.Provider != verify.ProviderTart {
		return state.ErrInvalid
	}
	o, err := p.begin(ctx, record.RequestID(handle.ID))
	if err != nil {
		return err
	}
	defer o.entry.Close()
	v, err := o.entry.Read(ctx)
	if err != nil {
		return err
	}
	if v.State != record.ExecutionReleased || v.Occupied {
		return fmt.Errorf("tart: artifact cleanup requires confirmed resource release")
	}
	if v.Resource == "" {
		return nil
	}
	expected := "dockhand2-" + record.Digest([]byte(o.entry.Pool.ID + "/" + string(v.ID)))[:24]
	if v.Resource != expected {
		return fmt.Errorf("tart: unexpected resource directory")
	}
	if _, err = o.restore(v); err != nil {
		return err
	}
	if len(v.Result) > 0 {
		var observation verify.Observation
		if err = json.Unmarshal(v.Result, &observation); err != nil {
			return err
		}
		if len(observation.Artifacts) != 0 {
			return fmt.Errorf("tart: execution has build outputs that must be retained")
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(o.entry.Pool.Directory)
	if err != nil {
		return err
	}
	defer root.Close()
	// Root-relative removal cannot follow an artifact symlink outside the pool.
	// The per-submission lock lives outside this directory and is never removed.
	if err = root.RemoveAll(v.Resource); err != nil {
		return err
	}
	parent, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}

// removeInput discards the host transfer copy under the submission lock. The
// guest owns its staged source after admission; retries never restage it.
func (o *operation) removeInput(v record.ProviderExecution) error {
	if v.Resource != "dockhand2-"+record.Digest([]byte(o.entry.Pool.ID + "/" + string(v.ID)))[:24] {
		return fmt.Errorf("tart: unexpected resource directory")
	}
	root, err := os.OpenRoot(o.entry.Pool.Directory)
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(v.Resource + "/input.tar")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
