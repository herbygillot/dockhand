package portedit

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	osRead       = regexp.MustCompile(`\$(?:\{os\.major\}|os\.major\b)`)
	osComparison = regexp.MustCompile(`^\s*(?:>=|<=|>|<|==|!=|eq|ne)\s*([0-9]+)\s*`)
	archRead     = regexp.MustCompile(`\$(?:\{(?:build_arch|os\.arch)\}|(?:build_arch|os\.arch)\b)`)
)

// Scan every command, including expressions assigned to variables. This is not
// Tcl data-flow analysis: a platform read without a supported comparison is an
// explicit gap, even when other reads in the same command have known bounds.
func contextBoundaries(src []byte) (map[int]bool, bool, error) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false, fmt.Errorf("%w: invalid context syntax", ErrUnsupported)
	}
	majors := map[int]bool{}
	archDependent := false
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		name, _ := cmd.Name(src)
		if name == "supported_archs" && len(cmd.Words) > 1 {
			archDependent = true
		}
		raw := cmd.Span.Text(src)
		archDependent = archDependent || archRead.MatchString(raw)
		for _, read := range osRead.FindAllStringIndex(raw, -1) {
			suffix := raw[read[1]:]
			comparison := osComparison.FindStringSubmatchIndex(suffix)
			if comparison == nil || !comparisonEnd(suffix[comparison[1]:]) {
				return nil, false, fmt.Errorf("%w: OS read needs an explicit modeled boundary", ErrProbeInconclusive)
			}
			n, err := strconv.Atoi(suffix[comparison[2]:comparison[3]])
			if err != nil || n < 8 || n > 1000 {
				return nil, false, fmt.Errorf("%w: unsupported Darwin boundary", ErrProbeInconclusive)
			}
			for _, v := range []int{n - 1, n, n + 1} {
				if v >= 8 {
					majors[v] = true
				}
			}
		}
	}
	return majors, archDependent, nil
}

func comparisonEnd(rest string) bool {
	return rest == "" || strings.ContainsAny(rest[:1], "})]\"") || strings.HasPrefix(rest, "&&") || strings.HasPrefix(rest, "||")
}

// observationProfiles includes the host, relevant architecture choices, and
// both sides of literal Darwin conditions. Unmodeled expressions are gaps.
func observationProfiles(src []byte, native record.Platform) ([]record.Platform, error) {
	result := []record.Platform{native}
	majors, archDependent, err := contextBoundaries(src)
	if err != nil {
		return nil, err
	}
	if native.OS != "darwin" && (archDependent || len(majors) > 0) {
		return nil, fmt.Errorf("%w: alternate platforms require Darwin modeling", ErrProbeInconclusive)
	}
	current, _ := strconv.Atoi(native.Version)
	appendProfile := func(major int, arch string) {
		p := record.Platform{OS: native.OS, Version: strconv.Itoa(major), Architecture: arch}
		if !slices.Contains(result, p) {
			result = append(result, p)
		}
	}
	if archDependent {
		for _, arch := range []string{"arm64", "x86_64"} {
			if current >= 20 || arch != "arm64" {
				appendProfile(current, arch)
			}
		}
	}
	var versions []int
	for major := range majors {
		if major <= current {
			versions = append(versions, major)
		}
	}
	slices.Sort(versions)
	for _, major := range versions {
		appendProfile(major, "x86_64")
		if archDependent && major >= 20 {
			appendProfile(major, "arm64")
		}
	}
	return result, nil
}
