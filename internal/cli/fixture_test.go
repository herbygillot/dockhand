package cli

import (
	"context"
	"github.com/herbygillot/dockhand/internal/app"
	"time"
)

func runFixture(ctx context.Context, args []string, streams Streams, config app.Config) error {
	return run(ctx, args, streams, config, func(ctx context.Context, config app.Config) (*app.Services, error) {
		services, err := app.Build(ctx, config)
		if err != nil {
			return nil, err
		}
		services.Workflow.RetryDelay = 20 * time.Millisecond
		services.Workflow.WaitInterval = 20 * time.Millisecond
		services.Workflow.ObserveInterval = 20 * time.Millisecond
		services.Processes.Interval = 20 * time.Millisecond
		return services, nil
	})
}
