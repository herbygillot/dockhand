package observe

import (
	"context"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
)

// readObservation is shared by One and Observe, before either caches a result.
// Both evaluations have the identical contents, binding, and request.
func (s *Session) readObservation(ctx context.Context, bound macports.Context, request macports.ObservationRequest) (macports.Observation, error) {
	observed, err := s.Ports.Observe(ctx, bound, request)
	if err != nil || !observed.Modeled {
		return observed, err
	}
	experimenter, ok := s.Ports.(macports.MachineReadExperimenter)
	if !ok {
		return observed, nil
	}
	candidates := map[string][]macports.MachineReadCandidate{}
	for name, port := range observed.Ports {
		base := newBaseCode(port.BaseLibrary)
		for id, d := range port.Declarations {
			if d.Command != "dockhand.host-access" {
				continue
			}
			defaults, build := recognizeBuildDefault(d, base)
			lookupDefaults, lookup := recognizeBaseLookup(d, base)
			if build || lookup {
				for _, option := range lookupDefaults {
					if !slices.Contains(defaults, option) {
						defaults = append(defaults, option)
					}
				}
				candidates[name] = append(candidates[name], macports.MachineReadCandidate{Read: id, Defaults: defaults})
			}
		}
	}
	if len(candidates) == 0 {
		return observed, nil
	}
	var acted []string
	for _, name := range macports.ReadOptions {
		if ActedOption(name) {
			acted = append(acted, name)
		}
	}
	for name, summary := range experimenter.MachineReadExperiments(ctx, bound, request, observed, candidates, acted) {
		port := observed.Ports[name]
		port.MachineReadExperiment = &summary
		observed.Ports[name] = port
	}
	return observed, nil
}

func experimentExplains(summary *macports.MachineReadExperiment, id int) bool {
	return summary != nil && (summary.Outcome == "same" || summary.Outcome == "same, build-only moved") && slices.Contains(summary.Varied, id) && !slices.Contains(summary.Unexplained, id)
}
