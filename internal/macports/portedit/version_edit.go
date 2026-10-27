package portedit

import (
	"context"
	"maps"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/textedit"
)

type carrier struct {
	candidate                         portfile.Candidate
	prefix, suffix                    string
	sourceSeparator, literalSeparator string
	numeric                           *numericCarrier
}

func (s *Service) versionCarriers(ctx context.Context, request Request, input *sourceInput) (versionInputs, error) {
	spec, err := portsource.Interpret(input.info, portsource.Edit)
	if err != nil {
		return nil, err
	}
	candidates, err := portfile.Candidates(input.data)
	if err != nil {
		return nil, ErrUnsupported.Because("%w", err)
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
		// A literal that moves the source alone carries a part of it, as
		// openjdk's build number does its tag's, which an edit composed of
		// several moves with the version's (composed).
		info, ok := evaluated.after.Ports[input.target.Name]
		if !ok {
			continue
		}
		observed, err := portsource.Interpret(info, portsource.Edit)
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
			return nil, ErrUnsupported.Because("%w: no editable version input established (%d candidate evaluations were inconclusive)", errProbeInconclusive, failures)
		}
		return nil, ErrUnsupported.Because("no editable version input established (%d candidate evaluations were inconclusive)", failures)
	}
	return carriers, nil
}

func sameRepository(a, b portsource.Spec) bool {
	return a.Forge == b.Forge && a.Instance == b.Instance && a.Repository == b.Repository && a.Pattern == b.Pattern
}

func (s *Service) probeVersion(ctx context.Context, request Request, input *sourceInput, carriers versionInputs, sourceVersion string) ([]byte, macports.Snapshot, error) {
	reader := input.native(ctx, s.Ports)
	// A port that fetches GitHub's tarball moves to its archive with the
	// version, since the checksums are made anew anyway (the person's "we
	// should have Dockhand do those", 2026-10-27). Where the move can't be
	// made, or the update doesn't evaluate with it, the update is made as
	// it was, and the assessment's notice says the tarball.
	input.tarballMoved = false
	if input.info.Options["github.tarball_from"] == "tarball" && !input.info.GitFetched() {
		contents, snapshot, err := s.evaluateVersion(ctx, reader, request, input, carriers, sourceVersion, true, true)
		if err == nil {
			input.tarballMoved = true
			return contents, snapshot, nil
		}
		if ctx.Err() != nil {
			return nil, snapshot, ctx.Err()
		}
	}
	return s.evaluateVersion(ctx, reader, request, input, carriers, sourceVersion, true, false)
}

// versionInputs is the set of proven relations between Portfile literals and
// the source version. It decides which literal edits could select a source
// version; evaluating those edits is the service's job.
type versionInputs []carrier

// versionEdit is one literal edit that may select the source version, with
// the relation that produced it; also are the other literals an edit of
// several moves with it (composed).
type versionEdit struct {
	carrier  carrier
	value    string
	contents []byte
	also     []macports.ReleaseInput
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
	if len(result) == 0 {
		result = inputs.composed(data, sourceVersion)
	}
	return result
}

// composed are the edits that select sourceVersion by moving several
// literals together, where no one literal does: openjdk21's tag is
// jdk-${openjdk_version}+${build}, so jdk-21.0.13+5 moves both the version's
// literal and the build number's, and each alone was no edit (the composed
// versions note, 2026-10-02). Each is a plain substitution into its own
// place in the source version as it is; the text between them stays, and
// is what the new version is split at, where it splits one way only. Two
// or three move together; an edit is evaluated as any, and one alone must
// select the source.
func (inputs versionInputs) composed(data []byte, sourceVersion string) []versionEdit {
	type placed struct {
		carrier    carrier
		start, end int
	}
	var plain []placed
	old := ""
	for _, c := range inputs {
		if c.numeric != nil || c.sourceSeparator != "" {
			continue
		}
		whole := c.prefix + c.candidate.Value + c.suffix
		if old == "" {
			old = whole
		} else if whole != old {
			continue
		}
		plain = append(plain, placed{carrier: c, start: len(c.prefix), end: len(c.prefix) + len(c.candidate.Value)})
	}
	if len(plain) < 2 || len(plain) > 8 {
		return nil
	}
	var result []versionEdit
	attempted := map[string]bool{}
	try := func(set []placed) {
		slices.SortFunc(set, func(a, b placed) int { return a.start - b.start })
		var fixed []string
		at := 0
		for i, p := range set {
			if p.start < at || i > 0 && p.carrier.candidate.Span == set[i-1].carrier.candidate.Span {
				return
			}
			fixed = append(fixed, old[at:p.start])
			at = p.end
		}
		fixed = append(fixed, old[at:])
		for _, between := range fixed[1 : len(fixed)-1] {
			if between == "" {
				return
			}
		}
		values, ok := splitOnce(sourceVersion, fixed)
		if !ok {
			return
		}
		var changes []textedit.Edit
		var also []macports.ReleaseInput
		for i, p := range set {
			if !portfile.Literal(values[i]) {
				return
			}
			if values[i] == p.carrier.candidate.Value {
				continue
			}
			changes = append(changes, textedit.Edit{Span: p.carrier.candidate.Span, New: []byte(values[i])})
			also = append(also, macports.ReleaseInput{Offset: p.carrier.candidate.Span.Start, Before: p.carrier.candidate.Value, After: values[i]})
		}
		if len(changes) < 2 {
			return
		}
		contents, err := textedit.Apply(data, changes)
		if err != nil || attempted[string(contents)] {
			return
		}
		attempted[string(contents)] = true
		first := set[slices.IndexFunc(set, func(p placed) bool { return p.carrier.candidate.Span.Start == also[0].Offset })]
		result = append(result, versionEdit{carrier: first.carrier, value: also[0].After, contents: contents, also: also[1:]})
	}
	for i := range plain {
		for j := i + 1; j < len(plain); j++ {
			try([]placed{plain[i], plain[j]})
			for k := j + 1; k < len(plain); k++ {
				try([]placed{plain[i], plain[j], plain[k]})
			}
		}
	}
	return result
}

// splitOnce splits value at the fixed text between its parts, fixed[0]
// leading it and fixed[len-1] ending it, into len(fixed)-1 parts, where it
// splits one way only.
func splitOnce(value string, fixed []string) ([]string, bool) {
	rest, ok := strings.CutPrefix(value, fixed[0])
	if !ok {
		return nil, false
	}
	if rest, ok = strings.CutSuffix(rest, fixed[len(fixed)-1]); !ok {
		return nil, false
	}
	var found [][]string
	var walk func(rest string, between []string, parts []string)
	walk = func(rest string, between []string, parts []string) {
		if len(found) > 1 {
			return
		}
		if len(between) == 0 {
			if rest != "" {
				found = append(found, append(slices.Clone(parts), rest))
			}
			return
		}
		for at := 1; at+len(between[0]) <= len(rest); at++ {
			if strings.HasPrefix(rest[at:], between[0]) {
				walk(rest[at+len(between[0]):], between[1:], append(slices.Clone(parts), rest[:at]))
			}
		}
	}
	walk(rest, fixed[1:len(fixed)-1], nil)
	if len(found) != 1 {
		return nil, false
	}
	return found[0], true
}

// evaluateVersion evaluates every distinct edit that could select sourceVersion
// and accepts exactly one that selects the requested source. With
// checkFidelity it also resets the revision, requires a changed version, and
// refuses unexpected sibling changes, as an actual bump must. With
// moveTarball each edit also moves github.tarball_from tarball to archive
// (portfile.ArchiveFromTarball), and is taken only where it evaluates so.
func (s *Service) evaluateVersion(ctx context.Context, reader snapshotEvaluator, request Request, input *sourceInput, inputs versionInputs, sourceVersion string, checkFidelity, moveTarball bool) ([]byte, macports.Snapshot, error) {
	spec, err := portsource.Interpret(input.info, portsource.Edit)
	if err != nil {
		return nil, macports.Snapshot{}, err
	}
	var selected []byte
	var snapshot macports.Snapshot
	matches := 0
	var rejected error
	for _, edit := range inputs.edits(input.data, sourceVersion) {
		contents := edit.contents
		// A commit pin's version is a literal of its own, which moves
		// with the commit to the version its release gives.
		if request.Release != nil && request.Release.Pinned {
			if contents, err = portfile.SetVersionLiteral(contents, input.info.Version, request.Release.Version); err != nil {
				rejected = err
				continue
			}
		}
		if checkFidelity {
			// A version vercmp orders lower is taken by `port upgrade`
			// only where the epoch rises (the docs dig's I3).
			if lowers(request) {
				if contents, err = portfile.RaiseEpoch(contents, input.target.Subport, input.info.Epoch); err != nil {
					return nil, snapshot, ErrUnsupported.Because("the version is lower, so the epoch must rise: %w", err)
				}
			}
			contents, err = s.resetRevision(ctx, request, input, contents)
			if err != nil {
				return nil, snapshot, ErrUnsupported.Because("%w", err)
			}
		}
		if moveTarball {
			moved, ok := portfile.ArchiveFromTarball(contents, input.target.Subport)
			if !ok {
				return nil, snapshot, ErrUnsupported.Because("github.tarball_from tarball isn't set by one literal command")
			}
			contents = moved
		}
		evaluated, err := s.evaluateContents(ctx, reader, input, contents, !checkFidelity)
		if err != nil {
			if ctx.Err() != nil {
				return nil, snapshot, ctx.Err()
			}
			rejected = ErrUnsupported.Because("%w: candidate evaluation was inconclusive: %v", errProbeInconclusive, err)
			continue
		}
		after := evaluated.after
		next := after.Ports[input.target.Name]
		observed, err := portsource.Interpret(next, portsource.Edit)
		if err != nil || !sameRepository(spec, observed) || observed.SourceVersion != sourceVersion || spec.Forge != "" && next.Options["git.branch"] != spec.Pattern.Tag(sourceVersion) {
			rejected = ErrFidelity.Because("candidate did not select the requested source tag")
			continue
		}
		if moveTarball && next.Options["github.tarball_from"] != "archive" {
			rejected = ErrUnsupported.Because("candidate did not evaluate github.tarball_from archive")
			continue
		}
		if checkFidelity && next.Version == input.info.Version {
			rejected = ErrUnsupported.Because("candidate did not change the evaluated version")
			continue
		}
		if checkFidelity && next.Epoch != input.info.Epoch+epochRise(request) {
			rejected = ErrFidelity.Because("candidate evaluates to epoch %d, where the update writes %d", next.Epoch, input.info.Epoch+epochRise(request))
			continue
		}
		if checkFidelity {
			if err := distfetch.CheckFetchCredentials(next); err != nil {
				rejected = err
				continue
			}
		}
		desired := model.Release{Version: next.Version, Forge: string(spec.Forge)}
		if spec.Forge != "" {
			desired.Tag = spec.Pattern.Tag(sourceVersion)
		}
		// A probe compares the selected port with itself; an actual bump
		// compares the family, so a sibling that moved is an unexpected change.
		baseline := input.before
		if checkFidelity {
			if baseline, err = input.familySnapshot(ctx, s.Ports); err != nil {
				return nil, snapshot, err
			}
		}
		report := fidelity.ScopedVersion(request.SharedRelease, baseline, after, input.target.Name, desired, next.Options["checksums"])
		if checkFidelity && len(report.UnexpectedChanges) > 0 {
			rejected = report.Err()
			continue
		}
		selected, snapshot = contents, after
		input.versionInput = macports.ReleaseInput{Portfile: input.target.Portfile, Offset: edit.carrier.candidate.Span.Start, Before: edit.carrier.candidate.Value, After: edit.value}
		for _, also := range edit.also {
			also.Portfile = input.target.Portfile
			input.versionInput.Also = append(input.versionInput.Also, also)
		}
		matches++
	}
	if matches == 0 {
		if rejected != nil {
			return nil, snapshot, rejected
		}
		return nil, snapshot, ErrUnsupported.Because("no version input can select %s", sourceVersion)
	}
	if matches != 1 {
		return nil, snapshot, ErrUnsupported.Because("%d version inputs can select %s; edit the Portfile manually", matches, sourceVersion)
	}
	if checkFidelity {
		// Archive names the checksums list literally move with the
		// version, as go-1.26's three do (portfile.RenameChecksumFiles).
		renamed, err := portfile.RenameChecksumFiles(selected, input.info.Version, snapshot.Ports[input.target.Name].Version)
		if err != nil {
			return nil, snapshot, ErrUnsupported.Because("%w", err)
		}
		return s.followObsolete(ctx, reader, input, renamed, snapshot)
	}
	return selected, snapshot, nil
}

// followObsolete carries the new version into obsolete siblings that are
// replaced by the target and spell its old version as their own literal,
// such as a main port whose versioned subports have moved past it. A
// follower is edited only when exactly one literal in its selection holds
// the old version, its revision is already 0, and the edit moves nothing but
// the follower's version; otherwise it is left for a person.
func (s *Service) followObsolete(ctx context.Context, reader snapshotEvaluator, input *sourceInput, contents []byte, after macports.Snapshot) ([]byte, macports.Snapshot, error) {
	target := input.target.Name
	oldVersion, newVersion := input.info.Version, after.Ports[target].Version
	if oldVersion == "" || newVersion == "" || oldVersion == newVersion {
		return contents, after, nil
	}
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return contents, after, err
	}
	for _, name := range slices.Sorted(maps.Keys(family.Ports)) {
		old := family.Ports[name]
		if name == target || old.Options["replaced_by"] != target || old.Version != oldVersion || old.Revision != 0 || after.Ports[name].Version != oldVersion {
			continue
		}
		candidates, err := portfile.Candidates(contents)
		if err != nil {
			return contents, after, nil
		}
		var chosen *portfile.Candidate
		for i := range candidates {
			candidate := candidates[i]
			if candidate.Value != oldVersion || !portfile.CandidateInSelection(contents, candidate, name) {
				continue
			}
			if chosen != nil {
				chosen = nil
				break
			}
			chosen = &candidate
		}
		if chosen == nil {
			continue
		}
		next, err := chosen.Replace(contents, newVersion)
		if err != nil {
			continue
		}
		evaluated, err := s.evaluateContents(ctx, reader, input, next, false)
		if err != nil {
			if ctx.Err() != nil {
				return nil, after, ctx.Err()
			}
			continue
		}
		moved := evaluated.after.Ports[name]
		if moved.Version != newVersion || moved.Revision != 0 {
			continue
		}
		clean := true
		for other, port := range after.Ports {
			if other == name {
				continue
			}
			if !samePort(port, after.Root, evaluated.after.Ports[other], evaluated.after.Root) {
				clean = false
				break
			}
		}
		if !clean {
			continue
		}
		contents, after = next, evaluated.after
	}
	return contents, after, nil
}

// lowers reports whether the update moves to a version vercmp orders
// lower than the current one (model.ReleaseSelection.Lowers).
func lowers(request Request) bool { return request.Release != nil && request.Release.Lowers }

// epochRise is how much the update raises the epoch: one for a lower
// version, else none.
func epochRise(request Request) int {
	if lowers(request) {
		return 1
	}
	return 0
}

// samePort compares two evaluations of a port from different projections,
// each normalized by its own root, since paths such as filespath name the
// projection they were evaluated in.
func samePort(a macports.PortInfo, aRoot string, b macports.PortInfo, bRoot string) bool {
	a, b = fidelity.ComparablePort(a, aRoot), fidelity.ComparablePort(b, bRoot)
	return a.Version == b.Version && a.Revision == b.Revision && a.Epoch == b.Epoch && maps.Equal(a.Options, b.Options)
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
