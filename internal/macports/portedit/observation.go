package portedit

import (
	"runtime"

	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"golang.org/x/sync/errgroup"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

type observationKey struct {
	contents [sha256.Size]byte
	profile  string
}

// observeProfiles observes one set of contents in every profile at once. The
// contents are written to the workspace once and each profile gets its own
// interpreter, so the observations run concurrently on a read-only file;
// results keep the profiles' order. Baseline observations come from and go
// to the request's cache like single observations do.
func (s *Service) observeProfiles(ctx context.Context, input *sourceInput, contents []byte, profiles []record.Platform, declarations, selectedOnly bool) ([]macports.Observation, error) {
	observer, ok := s.Ports.(macports.Observer)
	if !ok {
		return nil, fmt.Errorf("%w: declaration observation is unavailable", ErrUnsupported)
	}
	results := make([]macports.Observation, len(profiles))
	requests := make([]macports.ObservationRequest, len(profiles))
	keys := make([]observationKey, len(profiles))
	baseline := declarations && bytes.Equal(contents, input.data)
	pending := false
	for i, profile := range profiles {
		requests[i] = macports.ObservationRequest{Platform: profile, Declarations: declarations, SelectedOnly: selectedOnly}
		if declarations {
			requests[i].Operands = input.platformOperands
		}
		encoded, _ := json.Marshal(requests[i])
		keys[i] = observationKey{contents: sha256.Sum256(contents), profile: string(encoded)}
		if baseline {
			if cached, ok := input.baselineObservations[keys[i]]; ok {
				results[i] = cached
				continue
			}
		}
		pending = true
	}
	if !pending {
		return results, nil
	}
	err := input.files.withContents(input.target.Portfile, contents, func() error {
		bound, err := input.context(input.before.Runtime.Platform, selectedOnly)
		if err != nil {
			return err
		}
		group, ctx := errgroup.WithContext(ctx)
		group.SetLimit(observationConcurrency)
		for i := range profiles {
			if baseline {
				if _, ok := input.baselineObservations[keys[i]]; ok {
					continue
				}
			}
			group.Go(func() error {
				observed, err := observer.Observe(ctx, bound, requests[i])
				if err != nil {
					return fmt.Errorf("%+v: %w", profiles[i], err)
				}
				observed.Snapshot.Source = record.Source{}
				results[i] = observed
				return nil
			})
		}
		return group.Wait()
	})
	if err != nil {
		return nil, err
	}
	if baseline {
		if input.baselineObservations == nil {
			input.baselineObservations = make(map[observationKey]macports.Observation)
		}
		for i := range profiles {
			input.baselineObservations[keys[i]] = results[i]
		}
	}
	return results, nil
}

// observationConcurrency bounds the interpreters one multi-profile
// observation starts at once. Each is a MacPorts process; the bound keeps a
// wide profile set from swamping the host while still overlapping the
// process startup and Portfile evaluation that dominate each one.
var observationConcurrency = min(8, max(2, runtime.NumCPU()))

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
