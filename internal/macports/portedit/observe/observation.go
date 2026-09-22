package observe

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

type key struct {
	contents [sha256.Size]byte
	profile  string
}

// Observe observes one set of contents in every profile at once. The
// contents are one overlay of the workspace and each profile gets its own
// interpreter, so the observations run concurrently on an immutable
// projection; results keep the profiles' order. Baseline observations come from and go
// to the session's cache like single observations do.
func (s *Session) Observe(ctx context.Context, contents []byte, profiles []record.Platform, declarations, selectedOnly bool) ([]macports.Observation, error) {
	observer := s.Ports
	results := make([]macports.Observation, len(profiles))
	requests := make([]macports.ObservationRequest, len(profiles))
	keys := make([]key, len(profiles))
	baseline := declarations && bytes.Equal(contents, s.Baseline)
	pending := false
	for i, profile := range profiles {
		requests[i] = macports.ObservationRequest{Platform: profile, Declarations: declarations, SelectedOnly: selectedOnly}
		if declarations {
			requests[i].Operands = s.operands
		}
		encoded, _ := json.Marshal(requests[i])
		keys[i] = key{contents: sha256.Sum256(contents), profile: string(encoded)}
		if baseline {
			if cached, ok := s.cache[keys[i]]; ok {
				results[i] = cached
				continue
			}
		}
		pending = true
	}
	if !pending {
		return results, nil
	}
	projection, err := s.Project(ctx, contents)
	if err != nil {
		return nil, err
	}
	bound, err := s.bind(projection, selectedOnly)
	if err != nil {
		return nil, err
	}
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(observationConcurrency)
	for i := range profiles {
		if baseline {
			if _, ok := s.cache[keys[i]]; ok {
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
		if s.cache == nil {
			s.cache = make(map[key]macports.Observation)
		}
		for i := range profiles {
			s.cache[keys[i]] = results[i]
		}
	}
	return results, nil
}

// observationConcurrency bounds the interpreters one multi-profile
// observation starts at once. Each is a MacPorts process; the bound keeps a
// wide profile set from swamping the host while still overlapping the
// process startup and Portfile evaluation that dominate each one.
var observationConcurrency = min(8, max(2, runtime.NumCPU()))

// One observes contents in one profile. Only immutable baseline
// declarations are reused from the cache; candidate observations and
// final untraced evaluations always run afresh.
func (s *Session) One(ctx context.Context, contents []byte, profile macports.ObservationRequest, selectedOnly bool) (macports.Observation, error) {
	observer := s.Ports
	if err := ctx.Err(); err != nil {
		return macports.Observation{}, err
	}
	profile.SelectedOnly = selectedOnly
	if profile.Declarations {
		profile.Operands = s.operands
	}
	encoded, _ := json.Marshal(profile)
	k := key{contents: sha256.Sum256(contents), profile: string(encoded)}
	baseline := profile.Declarations && bytes.Equal(contents, s.Baseline)
	if baseline {
		if cached, ok := s.cache[k]; ok {
			return cached, nil
		}
	}
	projection, err := s.Project(ctx, contents)
	if err != nil {
		return macports.Observation{}, err
	}
	bound, err := s.bind(projection, selectedOnly)
	if err != nil {
		return macports.Observation{}, err
	}
	observed, err := observer.Observe(ctx, bound, profile)
	observed.Snapshot.Source = record.Source{}
	if err == nil && baseline {
		if s.cache == nil {
			s.cache = make(map[key]macports.Observation)
		}
		s.cache[k] = observed
	}
	return observed, err
}
