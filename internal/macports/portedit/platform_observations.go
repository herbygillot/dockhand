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
			return nil, fmt.Errorf("%w: Darwin enumeration exceeds modeled range", ErrProbeInconclusive)
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
				observed, err := s.observeContents(ctx, request, input, contents, macports.ObservationRequest{Platform: profile, Declarations: true}, false)
				if err != nil {
					return nil, err
				}
				for _, port := range observed.Ports {
					if port.ModeledHostAccess {
						return nil, fmt.Errorf("%w: platform boundary depends on host state", ErrProbeInconclusive)
					}
					for _, fact := range port.Operands {
						if !slices.Contains(needs.operands, fact.Name) {
							continue
						}
						if !sourceBoundOperand(input.files.Root, fact.Frames) {
							return nil, fmt.Errorf("%w: platform operand %s has no captured source", ErrProbeInconclusive, fact.Name)
						}
						value, err := strconv.Atoi(fact.Value)
						if err != nil {
							return nil, fmt.Errorf("%w: platform operand %s is not an integer", ErrProbeInconclusive, fact.Name)
						}
						if previous, ok := values[fact.Name]; ok && previous != value {
							return nil, fmt.Errorf("%w: platform operand %s changes value (%d, %d)", ErrProbeInconclusive, fact.Name, previous, value)
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
					return nil, fmt.Errorf("%w: platform operand %s was not observed; branch coverage is incomplete", ErrProbeInconclusive, name)
				}
			}
			return expanded, nil
		}
		if len(expanded) > 40 {
			return nil, fmt.Errorf("%w: platform observation limit exceeded", ErrProbeInconclusive)
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
