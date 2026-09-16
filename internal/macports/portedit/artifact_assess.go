package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"path/filepath"
)

func (s *Service) assessArchives(ctx context.Context, request Request, input *sourceInput) (coverage []ContextCoverage, fetchErr, checksumErr error) {
	if _, ok := s.Ports.(macports.Observer); !ok {
		sources, err := downloadSources(input.info, filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile)))
		if err != nil {
			return coverage, err, nil
		}
		return coverage, nil, checkChecksumSources(input.data, input.info, sources)
	}
	profiles, err := observationProfiles(input.data, input.before.Platform)
	if err != nil {
		return coverage, err, nil
	}
	declared, covered := map[string]bool{}, map[string]bool{}
	for _, profile := range profiles {
		observed, err := s.observeContents(ctx, request, input, input.data, macports.ObservationRequest{Platform: profile, Declarations: true}, false)
		if err != nil {
			return coverage, err, nil
		}
		info := observed.Snapshot.Ports[input.target.Name]
		coverage = append(coverage, ContextCoverage{Platform: profile, Modeled: observed.Modeled, Fetch: info.Fetch})
		if err := checkArchivePolicy(info, filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile))); err != nil {
			return coverage, err, nil
		}
		metadata := observed.Ports[input.target.Name]
		if metadata.ModeledHostAccess {
			return coverage, fmt.Errorf("%w: %v", ErrProbeInconclusive, metadata.Problems), nil
		}
		if len(metadata.Problems) > 0 {
			return coverage, fmt.Errorf("%w: %v", ErrUnsupported, metadata.Problems), nil
		}
		if len(metadata.Distfiles) == 0 {
			return coverage, fmt.Errorf("%w: no source archives; select a release subport if this is a metaport", ErrUnsupported), nil
		}
		binding, err := distfiles.Bind(input.data, filepath.Join(input.files.Root, input.target.Portfile), info, metadata)
		if err != nil {
			return coverage, nil, err
		}
		for _, group := range binding.Groups {
			declared[group.ID()] = true
		}
		for _, artifact := range binding.Artifacts {
			covered[artifact.Group.ID()] = true
		}
	}
	for id := range declared {
		if !covered[id] {
			return coverage, nil, fmt.Errorf("%w: checksum declaration %s is not covered by the observed contexts", ErrProbeInconclusive, id)
		}
	}
	return coverage, nil, nil
}
