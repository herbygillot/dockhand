package observe

import (
	"errors"
	"runtime"

	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"golang.org/x/sync/errgroup"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

type key struct {
	contents [sha256.Size]byte
	profile  string
}

// unanswered is the call a modelled evaluation ended on, where err says it
// ended on one the model has no answer for.
func unanswered(err error) (string, bool) {
	var unanswered *macports.UnansweredError
	if errors.As(err, &unanswered) {
		return unanswered.Call, true
	}
	return "", false
}

// unansweredWords say a call the model has no answer for.
func unansweredWords(call string) string {
	return fmt.Sprintf("modeled context has no answer for `%s`, which the Portfile needs", call)
}

// Left says why a context modelled on another Mac is left as the Portfile
// has it, where it is: its evaluation ended on a call the model has no
// answer for (Observation.Unanswered), or its reading depends on host
// state (Tolerate). It is the one rule an update's plan, a checksum
// refresh, its fallback, an assessment and a revision reset leave a
// context by (Codex's review of #110 and #125, findings 5 and 6). This
// Mac's own context is never left: it's read as it stands (#125).
func Left(ctx context.Context, observed macports.Observation, port string, contents []byte) (string, bool) {
	if !observed.Modeled {
		return "", false
	}
	if observed.Unanswered != "" {
		return unansweredWords(observed.Unanswered), true
	}
	if read, inconclusive := Tolerate(ctx, observed.Ports[port], contents, observed.Snapshot.Root); inconclusive {
		return "its reading depends on host state" + HostInputs(read), true
	}
	return "", false
}

// Observe observes one set of contents in every profile at once. The
// contents are one overlay of the workspace and each profile gets its own
// interpreter, so the observations run concurrently on an immutable
// projection; results keep the profiles' order. Baseline observations come from and go
// to the session's cache like single observations do.
func (s *Session) Observe(ctx context.Context, contents []byte, profiles []model.Platform, declarations, selectedOnly bool) ([]macports.Observation, error) {
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
			// A modelled context whose evaluation ended on a call the model
			// has no answer for is one nothing else of was read, for its
			// readers to leave (Left), not a failure of the rest (Codex's
			// review of #110 and #125, finding 6).
			if call, ok := unanswered(err); ok {
				observed, err = macports.Observation{Modeled: true, Unanswered: call}, nil
			}
			if err != nil {
				return fmt.Errorf("%+v: %w", profiles[i], err)
			}
			observed.Snapshot.Source = model.Source{}
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
	// A closed answer that ended the evaluation is a context the model
	// has no answer for, named by the call: inconclusive, not a failure
	// (#110), and the observation says it, for a reader to leave the
	// context (Left).
	if call, ok := unanswered(err); ok {
		observed = macports.Observation{Modeled: true, Unanswered: call}
		err = ErrInconclusive.Because("%s", unansweredWords(call))
	}
	observed.Snapshot.Source = model.Source{}
	if err == nil && baseline {
		if s.cache == nil {
			s.cache = make(map[key]macports.Observation)
		}
		s.cache[k] = observed
	}
	return observed, err
}
