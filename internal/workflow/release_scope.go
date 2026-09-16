package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

func (e *Engine) rebindReleaseScope(ctx context.Context, scope *record.ReleaseScope, source record.Source, platform record.Platform) (*record.ReleaseScope, error) {
	if !scope.Valid() {
		return nil, ErrInvalidRequest
	}
	if scope == nil {
		return nil, nil
	}
	_, snapshot, err := e.bindSnapshot(ctx, source, macports.Selection{Selector: scope.Input.Portfile, Variants: scope.Affected[0].Target.Variants}, platform, nil)
	if err != nil {
		return nil, err
	}
	return macports.RebindReleaseScope(scope, snapshot)
}
