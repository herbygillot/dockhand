package eval

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

//go:embed versions.tcl
var versionScript string

// SelectVersion applies a Tcl capture expression and compares matching versions
// with MacPorts vercmp. Indices identifies every candidate tied for newest.
func (e *Evaluator) SelectVersion(ctx context.Context, current, expression string, candidates []macports.VersionCandidate) (_ macports.VersionSelection, err error) {
	session, _, err := e.start(ctx, macports.Tree{})
	if err != nil {
		return macports.VersionSelection{}, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	if _, err = session.Call(ctx, "eval", versionScript); err != nil {
		return macports.VersionSelection{}, err
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
		return macports.VersionSelection{}, err
	}
	fields, errs := syntax.ListValues(reply)
	if len(errs) > 0 || len(fields) < 1 {
		return macports.VersionSelection{}, fmt.Errorf("macports: invalid version selection response")
	}
	comparison, err := strconv.Atoi(fields[0])
	if err != nil {
		return macports.VersionSelection{}, fmt.Errorf("macports: invalid version comparison")
	}
	// MacPorts vercmp may return a character difference, not just -1 or 1.
	result := macports.VersionSelection{Comparison: cmp.Compare(comparison, 0)}
	seen := map[int]bool{}
	for _, field := range fields[1:] {
		index, err := strconv.Atoi(field)
		if err != nil || index < 0 || index >= len(candidates) || seen[index] {
			return macports.VersionSelection{}, fmt.Errorf("macports: invalid version candidate index")
		}
		seen[index] = true
		result.Indices = append(result.Indices, index)
	}
	return result, nil
}
