package app

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/eval"
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
	ports := &eval.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
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
	} else if darwin, err := strconv.Atoi(platform.Version); err == nil {
		// A host newer than the release dockhand builds on says which release
		// it wants. Verification builds on the image's own release, so a newer
		// machine choosing silently would verify on an untested macOS.
		if release, err := macos.ReleaseForDarwin(darwin); err == nil && macos.NewerThanDefault(release) {
			def, _ := macos.ReleaseForDarwin(macos.DefaultDarwin)
			return SetupResult{}, fmt.Errorf("setup: this Mac runs %s, which dockhand does not prepare by default; it prepares %s and older. Pass --os %s to prepare it deliberately, or --os %s", release.Name, def.Name, release.Slug, def.Slug)
		}
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
