package observe

import (
	"context"
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
	candidates := map[string][]int{}
	for name, port := range observed.Ports {
		base := newBaseCode(port.BaseLibrary)
		for id, d := range port.Declarations {
			if d.Command != "dockhand.host-access" {
				continue
			}
			_, build := recognizeBuildDefault(d, base)
			_, lookup := recognizeBaseLookup(d, base)
			if build || lookup {
				candidates[name] = append(candidates[name], id)
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
