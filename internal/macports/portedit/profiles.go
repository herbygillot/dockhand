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
	osRead        = regexp.MustCompile(`\$(?:\{os\.major\}|os\.major\b)`)
	archRead      = regexp.MustCompile(`\$(?:\{(?:(?:configure\.)?build_arch|os\.arch)\}|(?:(?:configure\.)?build_arch|os\.arch)\b)`)
	unmodeledRead = regexp.MustCompile(`\$(?:\{(?:os\.version|macosx_version|macos_version|macosx_deployment_target)\}|(?:os\.version|macosx_version|macos_version|macosx_deployment_target)\b)`)
)

const operandPattern = `(?:[0-9]+|\$\{[a-zA-Z_][a-zA-Z0-9_.]*\}|\$[a-zA-Z_][a-zA-Z0-9_.]*|\[option\s+[a-zA-Z_][a-zA-Z0-9_.]*\])`

var forwardComparison = regexp.MustCompile(`^\s*(?:>=|<=|>|<|==|!=|eq|ne)\s*(` + operandPattern + `)\s*`)
var reverseComparison = regexp.MustCompile(`(` + operandPattern + `)\s*(?:>=|<=|>|<|==|!=|eq|ne)\s*$`)

type platformNeeds struct {
	majors     map[int]bool
	operands   []string
	arch       bool
	exhaustive bool
}

func scanPlatformNeeds(src []byte) (platformNeeds, error) {
	n := platformNeeds{majors: map[int]bool{}}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return n, fmt.Errorf("%w: invalid context syntax", ErrUnsupported)
	}
	if err := unmodeledReads(src, script); err != nil {
		return n, err
	}
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		name, _ := cmd.Name(src)
		raw := cmd.Span.Text(src)
		n.arch = n.arch || (name == "supported_archs" && len(cmd.Words) > 1) || archRead.MatchString(raw)
		for _, read := range osRead.FindAllStringIndex(raw, -1) {
			suffix := raw[read[1]:]
			operand := ""
			if m := forwardComparison.FindStringSubmatch(suffix); m != nil && comparisonEnd(suffix[len(m[0]):]) {
				operand = m[1]
			} else if m := reverseComparison.FindStringSubmatchIndex(raw[:read[0]]); m != nil {
				prefix := strings.TrimSpace(raw[:m[2]])
				if prefix != "" && !strings.ContainsAny(prefix[len(prefix)-1:], "{([\"&|") {
					return n, fmt.Errorf("%w: unresolved inverted Darwin comparison", ErrProbeInconclusive)
				}
				operand = raw[m[2]:m[3]]
			} else {
				// Enumerate metadata contexts for aliases and formatting, but do not
				// mistake an unsupported comparison expression for a harmless read.
				rest := strings.TrimSpace(suffix)
				if strings.HasPrefix(rest, ">") || strings.HasPrefix(rest, "<") || strings.HasPrefix(rest, "==") || strings.HasPrefix(rest, "!=") {
					return n, fmt.Errorf("%w: unresolved Darwin comparison", ErrProbeInconclusive)
				}
				n.exhaustive = true
				continue
			}
			if v, err := strconv.Atoi(operand); err == nil {
				if err = addBoundary(n.majors, v); err != nil {
					return n, err
				}
				continue
			}
			operand = strings.TrimPrefix(operand, "$")
			operand = strings.Trim(operand, "{}")
			if strings.HasPrefix(operand, "[option") {
				operand = "option:" + strings.Fields(strings.Trim(operand, "[]"))[1]
			}
			if !slices.Contains(n.operands, operand) {
				n.operands = append(n.operands, operand)
			}
		}
	}
	slices.Sort(n.operands)
	return n, nil
}
func addBoundary(majors map[int]bool, n int) error {
	if n < 8 || n > 1000 {
		return fmt.Errorf("%w: unsupported Darwin boundary %d", ErrProbeInconclusive, n)
	}
	for _, v := range []int{n - 1, n, n + 1} {
		if v >= 8 {
			majors[v] = true
		}
	}
	return nil
}
func contextBoundaries(src []byte) (map[int]bool, bool, error) {
	n, err := scanPlatformNeeds(src)
	if err == nil && (len(n.operands) > 0 || n.exhaustive) {
		err = fmt.Errorf("%w: native platform observations required", ErrProbeInconclusive)
	}
	return n.majors, n.arch, err
}

func comparisonEnd(rest string) bool {
	return rest == "" || strings.ContainsAny(rest[:1], "})]\"") || strings.HasPrefix(rest, "&&") || strings.HasPrefix(rest, "||")
}

// observationProfiles includes the host, relevant architecture choices, and
// both sides of literal Darwin conditions. Unmodeled expressions are gaps.
func observationProfiles(src []byte, native record.Platform) ([]record.Platform, error) {
	majors, archDependent, err := contextBoundaries(src)
	if err != nil {
		return nil, err
	}
	return profilesForBoundaries(majors, archDependent, native)
}

func profilesForBoundaries(majors map[int]bool, archDependent bool, native record.Platform) ([]record.Platform, error) {
	result := []record.Platform{native}
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
