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
// contents are one overlay of the workspace and each profile gets its own
// interpreter, so the observations run concurrently on an immutable
// projection; results keep the profiles' order. Baseline observations come from and go
// to the request's cache like single observations do.
func (s *Service) observeProfiles(ctx context.Context, input *sourceInput, contents []byte, profiles []record.Platform, declarations, selectedOnly bool) ([]macports.Observation, error) {
	observer := s.Ports
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
	projection, err := input.projection(ctx, contents)
	if err != nil {
		return nil, err
	}
	bound, err := input.contextIn(projection, input.before.Runtime.Platform, selectedOnly)
	if err != nil {
		return nil, err
	}
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(observationConcurrency)
	for i := range profiles {
		if baseline {
			if _, ok := input.baselineObservations[keys[i]]; ok {
				continue
			}
		}
		group.Go(func() error {
			observed, err := observer.Observe(gctx, bound, requests[i])
			if err != nil {
				return fmt.Errorf("%+v: %w", profiles[i], err)
			}
			observed.Snapshot.Source = record.Source{}
			results[i] = observed
			return nil
		})
	}
	if err := group.Wait(); err != nil {
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
	observer := s.Ports
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
	projection, err := input.projection(ctx, contents)
	if err != nil {
		return macports.Observation{}, err
	}
	bound, err := input.contextIn(projection, input.before.Runtime.Platform, selectedOnly)
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
