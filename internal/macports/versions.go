package macports

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

type VersionCandidate struct{ Version, MatchText, CaptureVersion string }
type VersionSelection struct {
	Indices    []int
	Comparison int
}

//go:embed versions.tcl
var versionScript string

// SelectVersion applies a Tcl capture expression and compares matching versions
// with MacPorts vercmp. Indices identifies every candidate tied for newest.
func (e *Evaluator) SelectVersion(ctx context.Context, current, expression string, candidates []VersionCandidate) (_ VersionSelection, err error) {
	session, _, err := e.start(ctx, Tree{})
	if err != nil {
		return VersionSelection{}, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	if _, err = session.Call(ctx, "eval", versionScript); err != nil {
		return VersionSelection{}, err
	}
	args := []string{current, expression}
	for _, candidate := range candidates {
		capture := candidate.CaptureVersion
		if capture == "" {
			capture = candidate.Version
		}
		args = append(args, candidate.Version, capture, candidate.MatchText)
	}
	reply, err := session.Call(ctx, "select-version", args...)
	if err != nil {
		return VersionSelection{}, err
	}
	fields, errs := syntax.ListValues(reply)
	if len(errs) > 0 || len(fields) < 1 {
		return VersionSelection{}, fmt.Errorf("macports: invalid version selection response")
	}
	comparison, err := strconv.Atoi(fields[0])
	if err != nil {
		return VersionSelection{}, fmt.Errorf("macports: invalid version comparison")
	}
	// MacPorts vercmp may return a character difference, not just -1 or 1.
	result := VersionSelection{Comparison: cmp.Compare(comparison, 0)}
	seen := map[int]bool{}
	for _, field := range fields[1:] {
		index, err := strconv.Atoi(field)
		if err != nil || index < 0 || index >= len(candidates) || seen[index] {
			return VersionSelection{}, fmt.Errorf("macports: invalid version candidate index")
		}
		seen[index] = true
		result.Indices = append(result.Indices, index)
	}
	return result, nil
}
