package eval

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

//go:embed versions.tcl
var versionScript string

// SelectVersion applies a Tcl capture expression and compares matching versions
// with MacPorts vercmp. Indices identifies every candidate tied for newest.
func (e *Evaluator) SelectVersion(ctx context.Context, current, expression string, candidates []macports.VersionCandidate) (_ macports.VersionSelection, err error) {
	versions, err := e.versions(ctx)
	if err != nil {
		return macports.VersionSelection{}, err
	}
	defer func() { err = errors.Join(err, versions.Close()) }()
	return versions.SelectVersion(ctx, current, expression, candidates)
}

// ExtractVersions collects all distinct first captures using native Tcl regex
// semantics and Base's line-oriented regex livecheck behavior. With
// multiline it is Base's regexm instead: one match against the whole page,
// so the result holds at most one version.
func (e *Evaluator) ExtractVersions(ctx context.Context, expression, page string, multiline bool) (_ []string, err error) {
	versions, err := e.versions(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, versions.Close()) }()
	return versions.ExtractVersions(ctx, expression, page, multiline)
}

// VersionSession is one interpreter for a port's version comparisons and
// extractions, rather than one for each (macports.VersionSessions).
func (e *Evaluator) VersionSession(ctx context.Context) (macports.VersionSession, error) {
	return e.versions(ctx)
}

// versionSession is an interpreter with the version script loaded.
type versionSession struct {
	session *rpc.Session
}

func (e *Evaluator) versions(ctx context.Context) (*versionSession, error) {
	session, _, err := e.start(ctx, macports.Tree{})
	if err != nil {
		return nil, err
	}
	if _, err = session.Call(ctx, "eval", versionScript); err != nil {
		return nil, errors.Join(err, session.Close())
	}
	return &versionSession{session: session}, nil
}

func (v *versionSession) Close() error { return v.session.Close() }

func (v *versionSession) SelectVersion(ctx context.Context, current, expression string, candidates []macports.VersionCandidate) (macports.VersionSelection, error) {
	args := []string{current, expression}
	for _, candidate := range candidates {
		capture := candidate.CaptureVersion
		if capture == "" {
			capture = candidate.Version
		}
		args = append(args, candidate.Version, capture, candidate.MatchText)
	}
	reply, err := v.session.Call(ctx, "select-version", args...)
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

func (v *versionSession) ExtractVersions(ctx context.Context, expression, page string, multiline bool) ([]string, error) {
	mode := "line"
	if multiline {
		mode = "page"
	}
	reply, err := v.session.Call(ctx, "extract-versions", expression, page, mode)
	if err != nil {
		return nil, err
	}
	values, errs := syntax.ListValues(reply)
	if len(errs) > 0 {
		return nil, fmt.Errorf("macports: invalid livecheck capture response")
	}
	return values, nil
}
