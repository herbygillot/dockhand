package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
	"strings"
)

type carrier struct {
	candidate      portfile.Candidate
	prefix, suffix string
}

func (s *Service) versionCarriers(ctx context.Context, request Request, input *sourceInput) ([]carrier, error) {
	spec, err := portsource.Interpret(input.info)
	if err != nil {
		return nil, err
	}
	candidates, err := portfile.Candidates(input.data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	var carriers []carrier
	failures := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// A literal that already spells the full source value can be tested
		// directly with the real candidate, even if artificial values are invalid.
		if candidate.Value == spec.SourceVersion {
			carriers = append(carriers, carrier{candidate: candidate})
			continue
		}
		probe := candidate.Probe()
		contents, err := candidate.Replace(input.data, probe)
		if err != nil {
			continue
		}
		_, snapshot, _, err := s.evaluateEdit(ctx, request, input, contents)
		if err != nil {
			failures++
			continue
		}
		info, ok := snapshot.Ports[input.target.Name]
		if !ok || info.Version == input.info.Version {
			continue
		}
		observed, err := portsource.Interpret(info)
		if err != nil || !sameRepository(spec, observed) || observed.SourceVersion == spec.SourceVersion {
			continue
		}
		// Infer only literal substitution into the source reference. The final port
		// version is always evaluated, never inverted or predicted from this probe.
		for start := 0; start <= len(spec.SourceVersion)-len(candidate.Value); start++ {
			if !strings.HasPrefix(spec.SourceVersion[start:], candidate.Value) {
				continue
			}
			prefix, suffix := spec.SourceVersion[:start], spec.SourceVersion[start+len(candidate.Value):]
			if observed.SourceVersion == prefix+probe+suffix {
				carriers = append(carriers, carrier{candidate: candidate, prefix: prefix, suffix: suffix})
			}
		}
	}
	if len(carriers) == 0 {
		if failures > 0 {
			return nil, fmt.Errorf("%w: %w: no editable version input established (%d candidate evaluations were inconclusive)", ErrUnsupported, ErrProbeInconclusive, failures)
		}
		return nil, fmt.Errorf("%w: no editable version input established (%d candidate evaluations were inconclusive)", ErrUnsupported, failures)
	}
	return carriers, nil
}

func sameRepository(a, b portsource.Spec) bool {
	return a.Forge == b.Forge && a.Instance == b.Instance && a.Repository == b.Repository && a.Pattern == b.Pattern
}

func (s *Service) probeVersion(ctx context.Context, request Request, input *sourceInput, carriers []carrier, sourceVersion string) ([]byte, macports.Snapshot, error) {
	return s.evaluateVersion(ctx, request, input, carriers, sourceVersion, true)
}

func (s *Service) evaluateVersion(ctx context.Context, request Request, input *sourceInput, carriers []carrier, sourceVersion string, checkFidelity bool) ([]byte, macports.Snapshot, error) {
	spec, err := portsource.Interpret(input.info)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	var selected []byte
	var snapshot macports.Snapshot
	matches := 0
	var rejected error
	for _, carrier := range carriers {
		value, ok := strings.CutPrefix(sourceVersion, carrier.prefix)
		if !ok {
			continue
		}
		value, ok = strings.CutSuffix(value, carrier.suffix)
		if !ok || !portfile.Literal(value) || value == carrier.candidate.Value {
			continue
		}
		contents, err := carrier.candidate.Replace(input.data, value)
		if err != nil {
			continue
		}
		contents, err = portfile.ResetRevision(contents, input.info.Revision)
		if err != nil {
			return nil, snapshot, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
		_, after, root, err := s.evaluateEdit(ctx, request, input, contents)
		if err != nil {
			if ctx.Err() != nil {
				return nil, snapshot, ctx.Err()
			}
			rejected = fmt.Errorf("%w: %w: candidate evaluation was inconclusive: %v", ErrUnsupported, ErrProbeInconclusive, err)
			continue
		}
		next := after.Ports[input.target.Name]
		observed, err := portsource.Interpret(next)
		if err != nil || !sameRepository(spec, observed) || observed.SourceVersion != sourceVersion || next.Options["git.branch"] != spec.Pattern.Tag(sourceVersion) {
			rejected = fmt.Errorf("%w: candidate did not select the requested source tag", ErrFidelity)
			continue
		}
		if checkFidelity && next.Version == input.info.Version {
			rejected = fmt.Errorf("%w: candidate did not change the evaluated version", ErrUnsupported)
			continue
		}
		if checkFidelity {
			if err := checkFetchCredentials(next); err != nil {
				rejected = err
				continue
			}
		}
		fidelity := versionFidelity(input.before, after, input.target.Name, input.files.Root, root, record.Release{Version: next.Version, Tag: spec.Pattern.Tag(sourceVersion)}, next.Options["checksums"])
		if checkFidelity && len(fidelity.UnexpectedChanges) > 0 {
			rejected = fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
			continue
		}
		selected, snapshot = contents, after
		matches++
	}
	if matches == 0 {
		if rejected != nil {
			return nil, snapshot, rejected
		}
		return nil, snapshot, fmt.Errorf("%w: no version input can select %s", ErrUnsupported, sourceVersion)
	}
	if matches != 1 {
		return nil, snapshot, fmt.Errorf("%w: %d version inputs can select %s; edit the Portfile manually", ErrUnsupported, matches, sourceVersion)
	}
	return selected, snapshot, nil
}
