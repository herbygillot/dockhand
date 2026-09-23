package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

// AvailablePlatforms is the --os value that names every release with a
// prepared local image.
const AvailablePlatforms = "available"

// buildPlatforms are the platforms a verification was asked to build on, by
// setup's release names or major versions, or AvailablePlatforms for every
// release a local image serves. Each keeps the evaluated platform's
// operating system and architecture, as setup does, and a release named
// twice is built once, where it was first named.
func (s *Services) buildPlatforms(ctx context.Context, evaluated record.Platform, names []string) ([]record.Platform, error) {
	var releases []macos.Release
	for _, name := range names {
		if name != AvailablePlatforms {
			release, err := macos.ParseRelease(name)
			if err != nil {
				return nil, err
			}
			releases = append(releases, release)
			continue
		}
		if s.local == nil {
			return nil, fmt.Errorf("--os %s requires Tart", AvailablePlatforms)
		}
		prepared, err := s.local.PreparedReleases(ctx)
		if err != nil {
			return nil, fmt.Errorf("--os %s: listing Tart images: %w", AvailablePlatforms, err)
		}
		if len(prepared) == 0 {
			return nil, fmt.Errorf("--os %s: no Tart image is prepared; run dockhand setup --os <release> first", AvailablePlatforms)
		}
		releases = append(releases, prepared...)
	}
	var platforms []record.Platform
	for _, release := range releases {
		platform := record.Platform{OS: evaluated.OS, Version: strconv.Itoa(release.Darwin), Architecture: evaluated.Architecture}
		if !slices.Contains(platforms, platform) {
			platforms = append(platforms, platform)
		}
	}
	return platforms, nil
}
