package app

import (
	"context"
	"io"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart/provision"
)

type SetupOptions struct {
	Check           bool
	Rebuild         bool
	Image           string
	Source          string
	MacPortsVersion string
	Xcode           string
}

type SetupResult = provision.Result

func Setup(ctx context.Context, config Config, options SetupOptions, progress io.Writer) (SetupResult, error) {
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	platform, err := ports.NativePlatform(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	image := options.Image
	if image == "" {
		image = config.Tart.Image
	}
	service := provision.Provisioner{
		Config: provision.Config{
			Executable:      config.Tart.Executable,
			Home:            config.Tart.Home,
			Image:           image,
			Source:          options.Source,
			MacPortsVersion: options.MacPortsVersion,
			GuestPrefix:     config.Tart.GuestPrefix,
			Platform:        platform,
			Xcode:           options.Xcode,
		},
		Progress: progress,
	}
	return service.Run(ctx, provision.Options{Check: options.Check, Rebuild: options.Rebuild})
}
