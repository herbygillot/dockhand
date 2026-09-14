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
	if handle.Provider != ProviderName {
		return state.ErrInvalid
	}
	o, err := p.begin(ctx, record.RequestID(handle.ID))
	if err != nil {
		return err
	}
	defer o.close()
	v, err := o.read(ctx, record.RequestID(handle.ID))
	if err != nil {
		return err
	}
	if v.State != record.ExecutionReleased || v.Occupied {
		return fmt.Errorf("tart: artifact cleanup requires confirmed resource release")
	}
	if v.Resource == "" {
		return nil
	}
	expected := "dockhand2-" + digest([]byte(o.pool.ID + "/" + string(v.ID)))[:24]
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
	root, err := os.OpenRoot(o.pool.Directory)
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
