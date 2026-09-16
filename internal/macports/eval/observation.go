package eval

import (
	"context"
	_ "embed"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"strconv"
)

//go:embed observation.tcl
var observationScript string

func (e *Evaluator) Observe(ctx context.Context, source macports.Context, request macports.ObservationRequest) (macports.Observation, error) {
	return e.evaluate(ctx, source, &request, request.SelectedOnly)
}

func decodeObservation(value string) (macports.PortObservation, error) {
	var out macports.PortObservation
	fields, errs := syntax.ListValues(value)
	if len(errs) > 0 || len(fields) != 4 {
		return out, fmt.Errorf("macports: invalid observation")
	}
	events, errs := syntax.ListValues(fields[0])
	if len(errs) > 0 {
		return out, fmt.Errorf("macports: invalid declarations")
	}
	for _, event := range events {
		parts, errs := syntax.ListValues(event)
		if len(errs) > 0 || len(parts) != 2 {
			return out, fmt.Errorf("macports: invalid declaration")
		}
		args, errs := syntax.ListValues(parts[0])
		if len(errs) > 0 || len(args) < 2 {
			return out, fmt.Errorf("macports: invalid declaration command")
		}
		frames, errs := syntax.ListValues(parts[1])
		if len(errs) > 0 {
			return out, fmt.Errorf("macports: invalid source frames")
		}
		d := macports.Declaration{Command: args[0], Values: args[1:]}
		for _, frame := range frames {
			f, errs := syntax.ListValues(frame)
			if len(errs) > 0 || len(f) != 3 {
				return out, fmt.Errorf("macports: invalid source frame")
			}
			line, err := strconv.Atoi(f[1])
			if err != nil {
				return out, err
			}
			d.Frames = append(d.Frames, macports.SourceFrame{File: f[0], Line: line, Command: f[2]})
		}
		out.Declarations = append(out.Declarations, d)
	}
	files, errs := syntax.ListValues(fields[1])
	if len(errs) > 0 {
		return out, fmt.Errorf("macports: invalid distfiles")
	}
	for _, file := range files {
		f, errs := syntax.ListValues(file)
		if len(errs) > 0 || len(f) != 2 {
			return out, fmt.Errorf("macports: invalid distfile")
		}
		urls, errs := syntax.ListValues(f[1])
		if len(errs) > 0 {
			return out, fmt.Errorf("macports: invalid locations")
		}
		out.Distfiles = append(out.Distfiles, macports.Distfile{Name: f[0], URLs: urls})
	}
	out.Problems, errs = syntax.ListValues(fields[2])
	if len(errs) > 0 {
		return out, fmt.Errorf("macports: invalid observation problems")
	}
	out.ModeledHostAccess, _ = strconv.ParseBool(fields[3])
	return out, nil
}

var _ macports.Observer = (*Evaluator)(nil)
