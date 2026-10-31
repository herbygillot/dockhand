package eval

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

//go:embed observation.tcl
var observationScript string

func (e *Evaluator) Observe(ctx context.Context, source macports.Context, request macports.ObservationRequest) (macports.Observation, error) {
	return e.evaluate(ctx, source, &request, request.SelectedOnly)
}

func decodeObservation(value string) (macports.PortObservation, error) {
	var out macports.PortObservation
	fields, errs := syntax.ListValues(value)
	if len(errs) > 0 || len(fields) != 11 {
		return out, fmt.Errorf("macports: invalid observation")
	}
	// The source frames, each once; the events name them by index.
	table, errs := syntax.ListValues(fields[7])
	if len(errs) > 0 {
		return out, fmt.Errorf("macports: invalid source frames")
	}
	sourceFrames := make([]macports.SourceFrame, len(table))
	for i, frame := range table {
		f, errs := syntax.ListValues(frame)
		if len(errs) > 0 || len(f) != 3 {
			return out, fmt.Errorf("macports: invalid source frame")
		}
		line, err := strconv.Atoi(f[1])
		if err != nil {
			return out, err
		}
		sourceFrames[i] = macports.SourceFrame{File: f[0], Line: line, Command: f[2]}
	}
	events, errs := syntax.ListValues(fields[0])
	if len(errs) > 0 {
		return out, fmt.Errorf("macports: invalid declarations")
	}
	operands, errs := syntax.ListValues(fields[4])
	if len(errs) > 0 {
		return out, fmt.Errorf("macports: invalid operand observations")
	}
	events = append(events, operands...)
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
			index, err := strconv.Atoi(frame)
			if err != nil || index < 0 || index >= len(sourceFrames) {
				return out, fmt.Errorf("macports: invalid source frame")
			}
			d.Frames = append(d.Frames, sourceFrames[index])
		}
		if d.Command == "dockhand.operand" && len(d.Values) == 2 {
			out.Operands = append(out.Operands, macports.OperandObservation{Name: d.Values[0], Value: d.Values[1], Frames: d.Frames})
		} else {
			out.Declarations = append(out.Declarations, d)
		}
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
	out.HostAccess, _ = strconv.ParseBool(fields[5])
	ledger, errs := syntax.ListValues(fields[6])
	if len(errs) > 0 || len(ledger)%2 != 0 {
		return out, fmt.Errorf("macports: invalid ledger")
	}
	for i := 0; i < len(ledger); i += 2 {
		key, errs := syntax.ListValues(ledger[i])
		count, err := strconv.Atoi(ledger[i+1])
		if len(errs) > 0 || len(key) != 4 || err != nil {
			return out, fmt.Errorf("macports: invalid ledger entry")
		}
		out.Ledger = append(out.Ledger, macports.LedgerEntry{Command: key[0], Subcommand: key[1], Source: key[2], Subject: key[3], Count: count})
	}
	if err := decodeRecord(&out, fields[8], fields[9], fields[10]); err != nil {
		return out, err
	}
	return out, nil
}

// decodeRecorded reads what a recording native evaluation read and ran,
// its reads and execs as decodeRecord takes them.
func decodeRecorded(value string) (macports.PortObservation, error) {
	var out macports.PortObservation
	fields, errs := syntax.ListValues(value)
	if len(errs) > 0 || len(fields) != 2 {
		return out, fmt.Errorf("macports: invalid record")
	}
	return out, decodeRecord(&out, fields[0], fields[1], "")
}

// decodeRecord reads what a recording evaluation read, ran, and declared:
// reads and execs are dictionaries of counts, keyed by a read's command,
// subcommand, kind, source, and path, and by a command line and its
// source; hooks a list of procedure, origin, and body.
func decodeRecord(out *macports.PortObservation, reads, execs, hooks string) error {
	counted := func(value string, width int, each func(key []string, count int)) error {
		entries, errs := syntax.ListValues(value)
		if len(errs) > 0 || len(entries)%2 != 0 {
			return fmt.Errorf("macports: invalid record")
		}
		for i := 0; i < len(entries); i += 2 {
			key, errs := syntax.ListValues(entries[i])
			count, err := strconv.Atoi(entries[i+1])
			if len(errs) > 0 || len(key) != width || err != nil {
				return fmt.Errorf("macports: invalid record entry")
			}
			each(key, count)
		}
		return nil
	}
	if err := counted(reads, 5, func(key []string, count int) {
		out.Reads = append(out.Reads, macports.PathRead{Command: key[0], Subcommand: key[1], Kind: key[2], Source: key[3], Path: key[4], Count: count})
	}); err != nil {
		return err
	}
	if err := counted(execs, 2, func(key []string, count int) {
		out.Execs = append(out.Execs, macports.CommandRun{Line: key[0], Source: key[1], Count: count})
	}); err != nil {
		return err
	}
	list, errs := syntax.ListValues(hooks)
	if len(errs) > 0 {
		return fmt.Errorf("macports: invalid hooks")
	}
	for _, hook := range list {
		fields, errs := syntax.ListValues(hook)
		if len(errs) > 0 || len(fields) != 3 {
			return fmt.Errorf("macports: invalid hook")
		}
		out.Hooks = append(out.Hooks, macports.Hook{Procedure: fields[0], Origin: fields[1], Body: fields[2]})
	}
	return nil
}

var _ macports.Observer = (*Evaluator)(nil)

var operandName = regexp.MustCompile(`^(?:option:)?[a-zA-Z_][a-zA-Z0-9_.]*$`)

//go:embed platform.tcl
var platformScript string
