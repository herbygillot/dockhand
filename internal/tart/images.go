package tart

import (
	"context"
	"encoding/json"
	"fmt"
)

type Image struct {
	Name    string
	Source  string
	Running bool
}

func (c Client) Images(ctx context.Context, options RunOptions) ([]Image, error) {
	output, err := c.Run(ctx, options, "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name, Source, State string
		Running             bool
	}
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("tart: invalid image listing: %w", err)
	}
	result := make([]Image, 0, len(rows))
	for _, row := range rows {
		result = append(result, Image{Name: row.Name, Source: row.Source, Running: row.Running || row.State == "running"})
	}
	return result, nil
}
