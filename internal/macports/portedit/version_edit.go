package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
)

type carrier struct {
	candidate                         portfile.Candidate
	prefix, suffix                    string
	sourceSeparator, literalSeparator string
	numeric                           *numericCarrier
}

func (s *Service) versionCarriers(ctx context.Context, request Request, input *sourceInput) (versionInputs, error) {
	spec, err := portsource.ForEditing(input.info)
	if err != nil {
		return nil, err
	}
	candidates, err := portfile.Candidates(input.data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	var carriers versionInputs
	failures := 0
	for _, candidate := range candidates {
		if !portfile.CandidateInSelection(input.data, candidate, input.target.Subport) {
			continue
		}
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
		evaluated, err := s.evaluateCandidate(ctx, input, contents)
		if err != nil {
			failures++
			continue
		}
		info, ok := evaluated.after.Ports[input.target.Name]
		if !ok || info.Version == input.info.Version {
			continue
		}
		observed, err := portsource.ForEditing(info)
		if err != nil || !sameRepository(spec, observed) || observed.SourceVersion == spec.SourceVersion {
			continue
		}
		if sourceSeparator, literalSeparator, ok := separatorMapping(candidate.Value, spec.SourceVersion, probe, observed.SourceVersion); ok {
			carriers = append(carriers, carrier{candidate: candidate, sourceSeparator: sourceSeparator, literalSeparator: literalSeparator})
		}
		if numeric := numericRelation(candidate, probe, spec.SourceVersion, observed.SourceVersion); numeric != nil {
			carriers = append(carriers, *numeric)
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

func (s *Service) probeVersion(ctx context.Context, request Request, input *sourceInput, carriers versionInputs, sourceVersion string) ([]byte, macports.Snapshot, error) {
	return s.evaluateVersion(ctx, s.Ports, request, input, carriers, sourceVersion, true)
}

// versionInputs is the set of proven relations between Portfile literals and
// the source version. It decides which literal edits could select a source
// version; evaluating those edits is the service's job.
type versionInputs []carrier

// versionEdit is one literal edit that may select the source version, with
// the relation that produced it.
type versionEdit struct {
	carrier  carrier
	value    string
	contents []byte
}

// edits proposes the distinct Portfile edits that could select sourceVersion.
// Different relations can predict the same edit; ambiguity is between distinct
// edits, not between ways of deriving one edit.
func (inputs versionInputs) edits(data []byte, sourceVersion string) []versionEdit {
	var result []versionEdit
	attempted := map[string]bool{}
	for _, carrier := range inputs {
		value, ok := strings.CutPrefix(sourceVersion, carrier.prefix)
		if !ok {
			continue
		}
		value, ok = strings.CutSuffix(value, carrier.suffix)
		if carrier.numeric != nil {
			mapped, valid := carrier.numeric.literal(value)
			if !valid {
				continue
			}
			value = mapped
		}
		if carrier.sourceSeparator != "" {
			value = strings.ReplaceAll(value, carrier.sourceSeparator, carrier.literalSeparator)
		}
		if !ok || !portfile.Literal(value) || value == carrier.candidate.Value {
			continue
		}
		contents, err := carrier.candidate.Replace(data, value)
		if err != nil || attempted[string(contents)] {
			continue
		}
		attempted[string(contents)] = true
		result = append(result, versionEdit{carrier: carrier, value: value, contents: contents})
	}
	return result
}

// evaluateVersion evaluates every distinct edit that could select sourceVersion
// and accepts exactly one that selects the requested source. With
// checkFidelity it also resets the revision, requires a changed version, and
// refuses unexpected sibling changes, as an actual bump must.
func (s *Service) evaluateVersion(ctx context.Context, reader snapshotEvaluator, request Request, input *sourceInput, inputs versionInputs, sourceVersion string, checkFidelity bool) ([]byte, macports.Snapshot, error) {
	spec, err := portsource.ForEditing(input.info)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	var selected []byte
	var snapshot macports.Snapshot
	matches := 0
	var rejected error
	for _, edit := range inputs.edits(input.data, sourceVersion) {
		contents := edit.contents
		if checkFidelity {
			contents, err = s.resetRevision(ctx, request, input, contents)
			if err != nil {
				return nil, snapshot, fmt.Errorf("%w: %w", ErrUnsupported, err)
			}
		}
		evaluated, err := s.evaluateContents(ctx, reader, input, contents, !checkFidelity)
		if err != nil {
			if ctx.Err() != nil {
				return nil, snapshot, ctx.Err()
			}
			rejected = fmt.Errorf("%w: %w: candidate evaluation was inconclusive: %v", ErrUnsupported, ErrProbeInconclusive, err)
			continue
		}
		after := evaluated.after
		next := after.Ports[input.target.Name]
		observed, err := portsource.ForEditing(next)
		if err != nil || !sameRepository(spec, observed) || observed.SourceVersion != sourceVersion || spec.Forge != "" && next.Options["git.branch"] != spec.Pattern.Tag(sourceVersion) {
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
		desired := record.Release{Version: next.Version, Forge: string(spec.Forge)}
		if spec.Forge != "" {
			desired.Tag = spec.Pattern.Tag(sourceVersion)
		}
		report := fidelity.ScopedVersion(request.SharedRelease, input.before, after, input.target.Name, input.files.root, desired, next.Options["checksums"])
		if checkFidelity && len(report.UnexpectedChanges) > 0 {
			rejected = fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
			continue
		}
		selected, snapshot = contents, after
		input.versionInput = record.ReleaseInput{Portfile: input.target.Portfile, Offset: edit.carrier.candidate.Span.Start, Before: edit.carrier.candidate.Value, After: edit.value}
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

var decimalComponent = regexp.MustCompile(`[0-9]+`)

type numericCarrier struct{ scale, offset *big.Rat }

func (n *numericCarrier) literal(value string) (string, bool) {
	y, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", false
	}
	x := new(big.Rat).Quo(new(big.Rat).Sub(y, n.offset), n.scale)
	if !x.IsInt() || x.Sign() < 0 {
		return "", false
	}
	return x.Num().String(), true
}
func numericRelation(candidate portfile.Candidate, probe, old, next string) *carrier {
	x, err := strconv.ParseInt(candidate.Value, 10, 64)
	if err != nil {
		return nil
	}
	xp, err := strconv.ParseInt(probe, 10, 64)
	if err != nil || xp == x {
		return nil
	}
	for _, span := range decimalComponent.FindAllStringIndex(old, -1) {
		prefix, suffix := old[:span[0]], old[span[1]:]
		value, ok := strings.CutPrefix(next, prefix)
		if !ok {
			continue
		}
		value, ok = strings.CutSuffix(value, suffix)
		if !ok {
			continue
		}
		y, ok := new(big.Rat).SetString(old[span[0]:span[1]])
		if !ok {
			continue
		}
		yp, ok := new(big.Rat).SetString(value)
		if !ok || y.Cmp(yp) == 0 {
			continue
		}
		scale := new(big.Rat).Quo(new(big.Rat).Sub(yp, y), new(big.Rat).Sub(new(big.Rat).SetInt64(xp), new(big.Rat).SetInt64(x)))
		offset := new(big.Rat).Sub(y, new(big.Rat).Mul(scale, new(big.Rat).SetInt64(x)))
		if scale.Cmp(big.NewRat(1, 1)) == 0 && offset.Sign() == 0 {
			continue
		}
		return &carrier{candidate: candidate, prefix: prefix, suffix: suffix, numeric: &numericCarrier{scale: scale, offset: offset}}
	}
	return nil
}
