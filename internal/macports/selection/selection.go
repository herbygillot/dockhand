package selection

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
)

// Reader adds snapshot-relative indexed names to the native evaluator. Index
// must stage or open an index belonging to the supplied tree, never the checkout.
type Reader struct {
	*eval.Evaluator
	Index func(context.Context, macports.Tree) (*portindex.Index, error)
}

// FromEntry retains the exact indexed name and its owning Portfile.
func FromEntry(entry portindex.Entry, variants map[string]bool) macports.Selection {
	selected := macports.Selection{Selector: path.Join(entry.Portdir, "Portfile"), Variants: variants}
	if entry.Name != path.Base(entry.Portdir) {
		selected.Subport = entry.Name
	}
	return selected
}

func (r *Reader) Resolve(ctx context.Context, tree macports.Tree, selected macports.Selection) ([]record.Target, error) {
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	if strings.Contains(selected.Selector, "/") {
		return r.Evaluator.Resolve(ctx, tree, selected)
	}
	if r.Index == nil {
		return nil, fmt.Errorf("%w: source-bound port index is required", macports.ErrTarget)
	}
	index, err := r.Index(ctx, tree)
	if err != nil {
		return nil, err
	}
	entry, err := index.Lookup(selected.Selector)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot resolve %s in selected source: %w", macports.ErrTarget, selected.Selector, err)
	}
	if selected.Subport != "" && selected.Subport != entry.Name {
		return nil, fmt.Errorf("%w: conflicting port and subport names", macports.ErrTarget)
	}
	if !macports.ValidName(entry.Name) || !fs.ValidPath(entry.Portdir) || strings.Count(entry.Portdir, "/") != 1 || strings.Contains(entry.Portdir, "\\") {
		return nil, fmt.Errorf("%w: invalid indexed target %s", macports.ErrTarget, entry.Name)
	}
	bound := FromEntry(entry, selected.Variants)
	if err := bound.Validate(); err != nil {
		return nil, err
	}
	targets, err := r.Evaluator.Resolve(ctx, tree, bound)
	if err != nil {
		return nil, err
	}
	if len(targets) != 1 || targets[0].Name != entry.Name {
		return nil, fmt.Errorf("%w: index names %s but its Portfile evaluates a different target", macports.ErrTarget, entry.Name)
	}
	return targets, nil
}
