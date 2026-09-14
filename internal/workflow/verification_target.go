package workflow

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/git/changeset"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

// inferVerificationTarget selects recorded intent only when the full source
// change remains within its port. This does not infer dependency coverage.
func (e *Engine) inferVerificationTarget(ctx context.Context, source record.Source, change record.Change, selection macports.Selection) (record.Target, error) {
	if change.ID == "" {
		return record.Target{}, fmt.Errorf("%w: cannot infer a port without a tracked contribution; specify a port explicitly", ErrInvalidRequest)
	}
	if len(change.Targets) != 1 {
		var names []string
		for _, target := range change.Targets {
			names = append(names, target.Name+" ("+target.Portfile+")")
		}
		slices.Sort(names)
		return record.Target{}, fmt.Errorf("%w: tracked contribution has %d targets [%s]; specify one port explicitly", ErrInvalidRequest, len(change.Targets), strings.Join(names, ", "))
	}
	if source.Base == "" {
		return record.Target{}, fmt.Errorf("%w: tracked contribution has no recorded base for scope checks; specify a port explicitly", ErrInvalidRequest)
	}
	target := change.Targets[0]
	delta, err := changeset.Between(ctx, e.Repo, source.Base, source.Tree)
	if err != nil {
		return record.Target{}, fmt.Errorf("cannot inspect the tracked contribution's scope; specify a port explicitly: %w", err)
	}
	directory := path.Dir(target.Portfile) + "/"
	for _, name := range delta.Paths {
		if !strings.HasPrefix(name, directory) {
			return record.Target{}, fmt.Errorf("%w: %q is outside tracked port %s relative to its recorded base; specify a port explicitly", ErrInvalidRequest, name, target.Portfile)
		}
	}
	target.Variants = maps.Clone(target.Variants)
	if len(selection.Variants) > 0 {
		if target.Variants == nil {
			target.Variants = make(map[string]bool)
		}
		maps.Copy(target.Variants, selection.Variants)
	}
	if selection.Subport != "" {
		target.Subport, target.Name = selection.Subport, selection.Subport
	}
	return target, nil
}
