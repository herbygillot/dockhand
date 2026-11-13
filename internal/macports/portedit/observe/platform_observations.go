package observe

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Profiles closes over the platform boundaries seen in the baseline and
// candidate metadata, observing operands until no boundary is new. The
// source scan also covers declarations in branches that have not executed.
// The operands it finds are what later declaration observations request.
func (s *Session) Profiles(ctx context.Context, candidate []byte) ([]model.Platform, error) {
	needs, err := scanPlatformNeeds(s.Baseline)
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
	s.operands = needs.operands
	if needs.exhaustive {
		current, err := strconv.Atoi(s.Native.Version)
		if err != nil || current < 8 || current > 30 {
			return nil, ErrInconclusive.Because("Darwin enumeration exceeds modeled range")
		}
		for n := 8; n <= current; n++ {
			needs.majors[n] = true
		}
	}
	profiles, err := profilesForBoundaries(needs.majors, needs.arch, s.Native)
	if err != nil || len(needs.operands) == 0 {
		return profiles, err
	}
	values := map[string]int{}
	visited := map[model.Platform]bool{}
	for {
		for _, profile := range profiles {
			if visited[profile] {
				continue
			}
			visited[profile] = true
			for _, contents := range [][]byte{s.Baseline, candidate} {
				observed, err := s.One(ctx, contents, macports.ObservationRequest{Platform: profile, Declarations: true}, false)
				// A modelled context whose evaluation ended on a call the
				// model has no answer for has no operand to read: it's left
				// and named where the plan reads it (Left), as there, and
				// the boundary is found from the contexts that read (the
				// outside reader's point 7 on #110).
				if err != nil && observed.Modeled && observed.Unanswered != "" {
					continue
				}
				if err != nil {
					return nil, err
				}
				for name, port := range observed.Ports {
					if port, inconclusive := Tolerate(ctx, port, contents, observed.Snapshot.Root); inconclusive {
						return nil, ErrInconclusive.Because("platform boundary depends on host state%s", HostInputs(port))
					}
					for _, fact := range port.Operands {
						if !slices.Contains(needs.operands, fact.Name) {
							continue
						}
						if !sourceBoundOperand(observed.Snapshot.Root, fact.Frames) {
							where := ""
							for _, frame := range fact.Frames {
								if frame.File != "" {
									where = fmt.Sprintf(" (read at %s:%d)", filepath.Base(frame.File), frame.Line)
									break
								}
							}
							return nil, ErrInconclusive.Because("Dockhand couldn't verify the source of %s%s while checking %s for different macOS versions, so it couldn't prepare the update. No update was written.", strings.TrimPrefix(fact.Name, "option:"), where, name)
						}
						value, err := strconv.Atoi(fact.Value)
						if err != nil {
							return nil, ErrInconclusive.Because("platform operand %s is not an integer", fact.Name)
						}
						if previous, ok := values[fact.Name]; ok && previous != value {
							return nil, ErrInconclusive.Because("platform operand %s changes value (%d, %d)", fact.Name, previous, value)
						}
						values[fact.Name] = value
						if err := addBoundary(needs.majors, value); err != nil {
							return nil, err
						}
					}
				}
			}
		}
		expanded, err := profilesForBoundaries(needs.majors, needs.arch, s.Native)
		if err != nil {
			return nil, err
		}
		if len(expanded) == len(profiles) {
			for _, name := range needs.operands {
				if _, ok := values[name]; !ok {
					return nil, ErrInconclusive.Because("platform operand %s was not observed; branch coverage is incomplete", name)
				}
			}
			return expanded, nil
		}
		if len(expanded) > 40 {
			return nil, ErrInconclusive.Because("platform observation limit exceeded")
		}
		profiles = expanded
	}
}

// sourceBoundOperand reports whether a value was read from a file of the
// observation's own workspace: under its root, or under the resources the
// root names, which a candidate overlay links to the base workspace's
// rather than copying (workspace.Overlay): bazel's update was refused
// because legacysupport's own callbacks read the option there, while the
// same files copied into the overlay passed (#170). A file anywhere else
// still isn't evidence.
func sourceBoundOperand(root string, frames []macports.SourceFrame) bool {
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		roots[0] = resolved
	}
	if resources, err := filepath.EvalSymlinks(filepath.Join(roots[0], macports.ResourcesDirectory)); err == nil {
		roots = append(roots, resources)
	}
	for _, frame := range frames {
		if frame.File == "" {
			continue
		}
		file := frame.File
		if resolved, err := filepath.EvalSymlinks(file); err == nil {
			file = resolved
		}
		for _, root := range roots {
			rel, err := filepath.Rel(root, file)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				return true
			}
		}
	}
	return false
}

// hostInputs names the recorded host accesses with their Portfile lines so a
// refusal says which read an alternate profile cannot reproduce.
// HostInputs words the host state an observation depended on, for a
// message that says what made a modeled context inconclusive.
func HostInputs(port macports.PortObservation) string {
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
