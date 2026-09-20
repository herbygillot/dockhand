package portedit

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// contextProfiles closes over boundaries seen in baseline and candidate metadata.
// The source scan also covers declarations in branches that have not executed.
func (s *Service) contextProfiles(ctx context.Context, request Request, input *sourceInput, candidate []byte) ([]record.Platform, error) {
	needs, err := scanPlatformNeeds(input.data)
	if err != nil {
		return nil, err
	}
	next, err := scanPlatformNeeds(candidate)
	if err != nil {
		return nil, err
	}
	for major := range next.majors {
		needs.majors[major] = true
	}
	needs.arch = needs.arch || next.arch
	needs.exhaustive = needs.exhaustive || next.exhaustive
	for _, name := range next.operands {
		if !slices.Contains(needs.operands, name) {
			needs.operands = append(needs.operands, name)
		}
	}
	slices.Sort(needs.operands)
	input.platformOperands = needs.operands
	if needs.exhaustive {
		current, err := strconv.Atoi(input.before.Platform.Version)
		if err != nil || current < 8 || current > 30 {
			return nil, fmt.Errorf("%w: Darwin enumeration exceeds modeled range", errProbeInconclusive)
		}
		for n := 8; n <= current; n++ {
			needs.majors[n] = true
		}
	}
	profiles, err := profilesForBoundaries(needs.majors, needs.arch, input.before.Platform)
	if err != nil || len(needs.operands) == 0 {
		return profiles, err
	}
	values := map[string]int{}
	visited := map[record.Platform]bool{}
	for {
		for _, profile := range profiles {
			if visited[profile] {
				continue
			}
			visited[profile] = true
			for _, contents := range [][]byte{input.data, candidate} {
				observed, err := s.observeContents(ctx, input, contents, macports.ObservationRequest{Platform: profile, Declarations: true}, false)
				if err != nil {
					return nil, err
				}
				for _, port := range observed.Ports {
					if port, inconclusive := tolerateExplainedProbes(ctx, port, contents, input.files.root); inconclusive {
						return nil, fmt.Errorf("%w: platform boundary depends on host state%s", errProbeInconclusive, hostInputs(port))
					}
					for _, fact := range port.Operands {
						if !slices.Contains(needs.operands, fact.Name) {
							continue
						}
						if !sourceBoundOperand(input.files.root, fact.Frames) {
							return nil, fmt.Errorf("%w: platform operand %s has no captured source", errProbeInconclusive, fact.Name)
						}
						value, err := strconv.Atoi(fact.Value)
						if err != nil {
							return nil, fmt.Errorf("%w: platform operand %s is not an integer", errProbeInconclusive, fact.Name)
						}
						if previous, ok := values[fact.Name]; ok && previous != value {
							return nil, fmt.Errorf("%w: platform operand %s changes value (%d, %d)", errProbeInconclusive, fact.Name, previous, value)
						}
						values[fact.Name] = value
						if err := addBoundary(needs.majors, value); err != nil {
							return nil, err
						}
					}
				}
			}
		}
		expanded, err := profilesForBoundaries(needs.majors, needs.arch, input.before.Platform)
		if err != nil {
			return nil, err
		}
		if len(expanded) == len(profiles) {
			for _, name := range needs.operands {
				if _, ok := values[name]; !ok {
					return nil, fmt.Errorf("%w: platform operand %s was not observed; branch coverage is incomplete", errProbeInconclusive, name)
				}
			}
			return expanded, nil
		}
		if len(expanded) > 40 {
			return nil, fmt.Errorf("%w: platform observation limit exceeded", errProbeInconclusive)
		}
		profiles = expanded
	}
}

func sourceBoundOperand(root string, frames []macports.SourceFrame) bool {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for _, frame := range frames {
		if frame.File == "" {
			continue
		}
		file := frame.File
		if resolved, err := filepath.EvalSymlinks(file); err == nil {
			file = resolved
		}
		rel, err := filepath.Rel(root, file)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// hostInputs names the recorded host accesses with their Portfile lines so a
// refusal says which read an alternate profile cannot reproduce.
func hostInputs(port macports.PortObservation) string {
	var details []string
	add := func(detail string) {
		if !slices.Contains(details, detail) {
			details = append(details, detail)
		}
	}
	for _, declaration := range port.Declarations {
		if declaration.Command != "dockhand.host-access" || len(declaration.Values) == 0 {
			continue
		}
		detail := strings.TrimPrefix(declaration.Values[0], "modeled context depends on ")
		for _, frame := range declaration.Frames {
			if strings.HasSuffix(frame.File, "/Portfile") {
				detail += fmt.Sprintf(" (Portfile:%d)", frame.Line)
				break
			}
		}
		add(detail)
	}
	for _, problem := range port.Problems {
		add(strings.TrimPrefix(problem, "modeled context depends on "))
	}
	if len(details) == 0 {
		return ""
	}
	return ": " + strings.Join(details, "; ")
}
