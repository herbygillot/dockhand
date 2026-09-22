package eval

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

//go:embed compatibility.tcl
var compatibilityScript string

func (e *Evaluator) Inspect(ctx context.Context) (macports.Runtime, error) {
	session, runtime, err := e.start(ctx, macports.Tree{})
	if err != nil {
		return macports.Runtime{}, err
	}
	return runtime, session.Close()
}

func decodeRuntime(reply string) (macports.Runtime, error) {
	fields, failures := syntax.DictValues(reply)
	if len(failures) > 0 {
		return macports.Runtime{}, fmt.Errorf("%w: invalid runtime reply", macports.ErrStartup)
	}
	platform, err := decodePlatform(fields["platform"])
	if err != nil || fields["base_version"] == "" || fields["tcl_version"] == "" {
		return macports.Runtime{}, fmt.Errorf("%w: incomplete runtime reply", macports.ErrStartup)
	}
	result := macports.Runtime{Platform: platform, BaseVersion: fields["base_version"], TclVersion: fields["tcl_version"]}
	switch result.BaseVersion {
	case "2.12.2", "2.12.3", "2.12.4", "2.12.5", "2.12.6", "2.11.6", "2.10.7", "2.9.3", "2.8.1":
		result.SourceReviewed = true
	}
	return result, nil
}

// decodePlatform reads the interpreter's os_platform, os_major, and
// build_arch, the three a record.Platform holds.
func decodePlatform(reply string) (record.Platform, error) {
	values, failures := syntax.ListValues(reply)
	if len(failures) > 0 || len(values) != 3 || values[0] == "" || values[1] == "" || values[2] == "" {
		return record.Platform{}, fmt.Errorf("%w: incomplete platform reply", macports.ErrStartup)
	}
	return record.Platform{OS: values[0], Version: values[1], Architecture: values[2]}, nil
}
