package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"os"
	"path/filepath"
)

func (s *Service) observeContents(ctx context.Context, request Request, input *sourceInput, contents []byte, profile macports.ObservationRequest, selectedOnly bool) (_ macports.Observation, err error) {
	observer, ok := s.Ports.(macports.Observer)
	if !ok {
		return macports.Observation{}, fmt.Errorf("%w: declaration observation is unavailable", ErrUnsupported)
	}
	path := filepath.Join(input.files.Root, input.target.Portfile)
	original, err := os.ReadFile(path)
	if err != nil {
		return macports.Observation{}, err
	}
	if err = os.WriteFile(path, contents, 0600); err != nil {
		return macports.Observation{}, err
	}
	defer func() { err = errors.Join(err, os.WriteFile(path, original, 0600)) }()
	target := input.primary
	if selectedOnly {
		target = input.target
	}
	bound, err := macports.NewContext(request.Source, input.files.Root, target, input.before.Runtime.Platform)
	if err != nil {
		return macports.Observation{}, err
	}
	observed, err := observer.Observe(ctx, bound, profile)
	observed.Snapshot.Source = record.Source{}
	return observed, err
}
