// Package go2port is the Go side of vendored dependency blocks: the
// go.vendors option as the golang PortGroup reads it, and the go2port
// tool that computes one.
//
// The tool's shape differs from cargo2port's: there is no local
// lockfile mode. `go2port get <package> <version>` generates a whole
// portfile, resolving the module's go.mod at that version from its
// forge and downloading every dependency to checksum it — exactly what
// a maintainer runs by hand, network cost included. dockhand runs it
// and extracts only the go.vendors block from the output; the rest of
// the generated portfile is discarded, because the user's Portfile is
// edited by spans, never replaced.
package go2port

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/vendored"
)

const (
	// ToolName is the generator that owns the go.vendors format.
	// doctor probes for it under this name.
	ToolName = "go2port"
	// Kind is the block this package generates.
	Kind = vendored.GoVendors
)

// Generate computes a go.vendors block for a module at a version, by
// running the tool and extracting the block from the portfile it
// writes. pkg is the port's evaluated go.package — the module path the
// golang PortGroup derived from go.setup. The tool is resolved through
// the run's finder; a miss is ErrNoGenerator naming the tool, and a
// failed run reads "vendored: go2port: <stderr>".
func Generate(ctx context.Context, tools *tool.Finder, pkg, version string) ([]byte, error) {
	bin, err := tools.Find(tool.Go2Port)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", vendored.ErrNoGenerator, ToolName)
	}
	out, _, err := tool.Output(ctx, bin, tool.Opts{Args: []string{"get", pkg, version}})
	if err != nil {
		return nil, fmt.Errorf("vendored: %s: %s", ToolName, err) //nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
	}
	return ExtractBlock(out)
}

// ExtractBlock locates the go.vendors block inside a generated
// portfile. Split from Generate so the extraction — the part with
// judgment in it — is testable without the tool or its network.
func ExtractBlock(generated []byte) ([]byte, error) {
	cst, errs := syntax.Parse(generated)
	if len(errs) != 0 {
		return nil, fmt.Errorf("vendored: %s produced output that does not parse: %w", ToolName, errs[0])
	}
	// The generated portfile is the tool's own, not the user's: every
	// command is in scope, and exactly one go.vendors is expected.
	span, err := vendored.Locate(generated, cst, func(syntax.Command) bool { return true }, Kind)
	if err != nil {
		return nil, fmt.Errorf("vendored: %s output carries no %s block: %w", ToolName, Kind, err)
	}
	return vendored.ValidateBlock([]byte(span.Text(generated)), Kind)
}
