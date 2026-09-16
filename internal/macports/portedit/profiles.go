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

var osBoundary = regexp.MustCompile(`\$\{?os\.major\}?\s*(?:>=|<=|>|<|==|!=|eq|ne)\s*([0-9]+)`)

// observationProfiles includes the host, relevant architecture choices, and
// both sides of literal Darwin conditions. Unmodeled expressions are gaps.
func observationProfiles(src []byte, native record.Platform) ([]record.Platform, error) {
	result := []record.Platform{native}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: invalid context syntax", ErrUnsupported)
	}
	majors := map[int]bool{}
	archDependent := false
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		name, _ := cmd.Name(src)
		if name == "supported_archs" && len(cmd.Words) > 2 {
			archDependent = true
		}
		if name != "if" && name != "switch" && name != "platform" {
			continue
		}
		// Only expression words are inspected; bodies are traversed separately.
		for _, word := range cmd.Words[1:] {
			raw := word.Span.Text(src)
			if strings.Contains(raw, "\n") || strings.Contains(raw, ";") {
				continue
			}
			if strings.Contains(raw, "build_arch") || strings.Contains(raw, "os.arch") {
				archDependent = true
			}
			if !strings.Contains(raw, "os.major") {
				continue
			}
			matches := osBoundary.FindAllStringSubmatch(raw, -1)
			if len(matches) == 0 {
				return nil, fmt.Errorf("%w: OS condition needs an explicit modeled boundary", ErrProbeInconclusive)
			}
			for _, match := range matches {
				n, _ := strconv.Atoi(match[1])
				for _, v := range []int{n - 1, n, n + 1} {
					if v >= 8 {
						majors[v] = true
					}
				}
			}
		}
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
