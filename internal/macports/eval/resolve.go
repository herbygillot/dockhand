package eval

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// Resolve accepts a snapshot-relative port directory/Portfile or a unique
// directory name. A subport is selected explicitly within that Portfile.
func (e *Evaluator) Resolve(ctx context.Context, tree macports.Tree, selection macports.Selection) (_ []record.Target, err error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	selector := selection.Selector
	var candidates []string
	switch strings.Count(selector, "/") {
	case 2:
		if path.Base(selector) != "Portfile" {
			return nil, fmt.Errorf("%w: expected category/port/Portfile", macports.ErrTarget)
		}
		candidates = append(candidates, selector)
	case 1:
		candidates = append(candidates, selector+"/Portfile")
	case 0:
		categories, err := os.ReadDir(tree.Root())
		if err != nil {
			return nil, err
		}
		for _, category := range categories {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !category.IsDir() || strings.HasPrefix(category.Name(), ".") || strings.HasPrefix(category.Name(), "_") {
				continue
			}
			dirs, err := os.ReadDir(filepath.Join(tree.Root(), category.Name()))
			if err != nil {
				return nil, err
			}
			for _, dir := range dirs {
				if !dir.IsDir() || !strings.EqualFold(dir.Name(), selector) {
					continue
				}
				portfile := path.Join(category.Name(), dir.Name(), "Portfile")
				if _, err := os.Stat(filepath.Join(tree.Root(), portfile)); err == nil {
					candidates = append(candidates, portfile)
				} else if !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
			}
		}
	default:
		return nil, fmt.Errorf("%w: expected category/port or a directory name", macports.ErrTarget)
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("%w: %q matched %d port directories; specify category/port", macports.ErrTarget, selector, len(candidates))
	}
	provisional := record.Target{Name: path.Base(path.Dir(candidates[0])), Portfile: candidates[0], Subport: selection.Subport, Variants: maps.Clone(selection.Variants)}
	bound, err := tree.Select(provisional)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", macports.ErrTarget, err)
	}
	session, _, err := e.start(ctx, tree)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	info, _, err := evaluateOne(ctx, session, bound, selection.Subport)
	if err != nil {
		return nil, err
	}
	if selection.Subport != "" && info.Name != selection.Subport {
		return nil, fmt.Errorf("%w: requested %s, evaluated %s", macports.ErrTarget, selection.Subport, info.Name)
	}
	provisional.Name = info.Name
	return []record.Target{provisional}, nil
}
