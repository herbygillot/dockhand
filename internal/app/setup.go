package app

import (
	"context"
	"io"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/tart/provision"
)

type SetupOptions struct {
	OS              string
	Check           bool
	Rebuild         bool
	Image           string
	Source          string
	MacPortsVersion string
	Xcode           string
}

type SetupResult struct {
	HostMacPorts macports.Runtime `json:"host_macports"`
	provision.Result
	OptionalTools []dependency.Availability `json:"optional_tools"`
}

func Setup(ctx context.Context, config Config, options SetupOptions, progress io.Writer) (SetupResult, error) {
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	runtime, err := ports.Inspect(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	platform := runtime.Platform
	if options.OS != "" {
		release, err := macos.ParseRelease(options.OS)
		if err != nil {
			return SetupResult{}, err
		}
		platform.Version = strconv.Itoa(release.Darwin)
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
	result, err := service.Run(ctx, provision.Options{Check: options.Check, Rebuild: options.Rebuild})
	return SetupResult{HostMacPorts: runtime, Result: result, OptionalTools: config.DependencyTools.Probe()}, err
}
