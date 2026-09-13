package github

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
)

const (
	catalogPages         = 20
	catalogPageSize      = 100
	catalogResponseLimit = 8 << 20
	catalogTimeout       = 90 * time.Second
)

func collectPages[T any](ctx context.Context, c *Client, repository, kind string) ([]T, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	var all []T
	for page := 1; page <= catalogPages; page++ {
		var rows []T
		resource := fmt.Sprintf("repos/%s/%s?per_page=%d&page=%d", repository, kind, catalogPageSize, page)
		if err := c.getJSON(ctx, resource, &rows, catalogResponseLimit); err != nil {
			return nil, err
		}
		if rows == nil || len(rows) > catalogPageSize {
			return nil, fmt.Errorf("github: invalid %s page", kind)
		}
		all = append(all, rows...)
		if len(rows) < catalogPageSize {
			return all, nil
		}
	}
	return nil, fmt.Errorf("%w: %s exceeds %d pages", forge.ErrIncomplete, kind, catalogPages)
}
