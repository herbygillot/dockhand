package eval

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

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

// PreviewAdapter is the Evaluator.Adapter that admits a development build
// of MacPorts Base, which reports an x.y.99 version. It is for developing
// dockhand against Base master: master asks the host questions from its
// parent interpreter that this dockhand does not observe, so preparation
// with it would be judged on answers dockhand never saw.
const PreviewAdapter = "preview"

// supportedFamilies are the released Base families this dockhand
// evaluates Portfiles with, oldest first.
var supportedFamilies = [][2]int{{2, 11}, {2, 12}}

// admitBase judges the version Base reports once its package is loaded,
// before mportinit reads the host's configuration. Released versions of a
// supported family are admitted by default; a development build only when
// adapter is PreviewAdapter, and nothing else is; the startup checks run
// either way.
func admitBase(version, adapter string) error {
	supported := make([]string, len(supportedFamilies))
	for i, family := range supportedFamilies {
		supported[i] = fmt.Sprintf("%d.%d", family[0], family[1])
	}
	releases := "released MacPorts Base " + strings.Join(supported, " and ")
	if adapter != "" && adapter != PreviewAdapter {
		return fmt.Errorf("%w: unknown MacPorts Base adapter %q; the one adapter that can be selected is %q", macports.ErrStartup, adapter, PreviewAdapter)
	}
	unrecognized := fmt.Errorf("%w: MacPorts Base reports version %q, which this dockhand does not recognize; it evaluates Portfiles with %s; check the selected --prefix/port-tclsh installation", macports.ErrStartup, version, releases)
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return unrecognized
	}
	var numbers [3]int
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return unrecognized
		}
		numbers[i] = number
	}
	family := [2]int{numbers[0], numbers[1]}
	development := numbers[2] >= 90
	if development {
		if adapter == PreviewAdapter {
			return nil
		}
		return fmt.Errorf("%w: MacPorts Base %s is a development build; this dockhand evaluates Portfiles with %s, since a development Base can ask the host questions this dockhand does not observe; choose a released installation with --prefix", macports.ErrStartup, version, releases)
	}
	if adapter == PreviewAdapter {
		return fmt.Errorf("%w: the %s adapter is for development builds of MacPorts Base, and %s is a release", macports.ErrStartup, PreviewAdapter, version)
	}
	for _, supported := range supportedFamilies {
		if family == supported {
			return nil
		}
	}
	if oldest := supportedFamilies[0]; family[0] < oldest[0] || family[0] == oldest[0] && family[1] < oldest[1] {
		return fmt.Errorf("%w: MacPorts Base %s is older than this dockhand evaluates Portfiles with (%s); update it with `sudo port selfupdate`, or choose another installation with --prefix", macports.ErrStartup, version, releases)
	}
	return fmt.Errorf("%w: MacPorts Base %s is newer than this dockhand evaluates Portfiles with (%s); commands that evaluate Portfiles are unavailable until dockhand supports it, while status and recorded evidence remain usable", macports.ErrStartup, version, releases)
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
