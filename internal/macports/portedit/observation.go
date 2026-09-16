package portedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

type observationKey struct {
	contents [sha256.Size]byte
	profile  string
}

func (s *Service) observeContents(ctx context.Context, input *sourceInput, contents []byte, profile macports.ObservationRequest, selectedOnly bool) (macports.Observation, error) {
	observer, ok := s.Ports.(macports.Observer)
	if !ok {
		return macports.Observation{}, fmt.Errorf("%w: declaration observation is unavailable", ErrUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return macports.Observation{}, err
	}
	profile.SelectedOnly = selectedOnly
	if profile.Declarations {
		profile.Operands = input.platformOperands
	}
	encoded, _ := json.Marshal(profile)
	key := observationKey{contents: sha256.Sum256(contents), profile: string(encoded)}
	// Reuse only immutable baseline declarations in this source-bound request.
	// Candidate observations and final untraced evaluations always run afresh.
	baseline := profile.Declarations && bytes.Equal(contents, input.data)
	if baseline {
		if cached, ok := input.baselineObservations[key]; ok {
			return cached, nil
		}
	}
	var observed macports.Observation
	err := input.files.withContents(input.target.Portfile, contents, func() error {
		bound, err := input.context(input.before.Runtime.Platform, selectedOnly)
		if err != nil {
			return err
		}
		observed, err = observer.Observe(ctx, bound, profile)
		observed.Snapshot.Source = record.Source{}
		return err
	})
	if err == nil && baseline {
		if input.baselineObservations == nil {
			input.baselineObservations = make(map[observationKey]macports.Observation)
		}
		input.baselineObservations[key] = observed
	}
	return observed, err
}
