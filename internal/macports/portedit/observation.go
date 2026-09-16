package portedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"os"
	"path/filepath"
)

type observationKey struct {
	contents [sha256.Size]byte
	profile  macports.ObservationRequest
}

func (s *Service) observeContents(ctx context.Context, request Request, input *sourceInput, contents []byte, profile macports.ObservationRequest, selectedOnly bool) (_ macports.Observation, err error) {
	observer, ok := s.Ports.(macports.Observer)
	if !ok {
		return macports.Observation{}, fmt.Errorf("%w: declaration observation is unavailable", ErrUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return macports.Observation{}, err
	}
	profile.SelectedOnly = selectedOnly
	key := observationKey{contents: sha256.Sum256(contents), profile: profile}
	// Reuse only immutable baseline declarations in this source-bound request.
	// Candidate observations and final untraced evaluations always run afresh.
	baseline := profile.Declarations && bytes.Equal(contents, input.data)
	if baseline {
		if cached, ok := input.baselineObservations[key]; ok {
			return cached, nil
		}
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
	if err == nil && baseline {
		if input.baselineObservations == nil {
			input.baselineObservations = make(map[observationKey]macports.Observation)
		}
		input.baselineObservations[key] = observed
	}
	return observed, err
}
