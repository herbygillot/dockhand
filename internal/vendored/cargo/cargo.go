// Package cargo is the cargo side of vendored dependency blocks:
// the cargo.crates option as the cargo_fetch PortGroup reads and expands
// it, and the cargo2port tool that writes one from a Cargo.lock.
//
// Two facts are owned here because they are conventions of that
// PortGroup and that tool, not of vendoring in general. A block is a
// flat list of name, version and checksum triples — how rust::handle_
// crates itself reads it, foreach {cname cversion chksum}. And each
// entry supplies the distfile ${name}-${version}.crate, which is what
// lets the parent package subtract a block's contribution from a
// context's evaluated distfiles without reconstructing any file name.
//
// The block's contents are still opaque: nothing here interprets a
// crate. A regenerated block is the tool's output re-laid, when the
// existing block's geometry could be proven (Assess) — only the
// whitespace between opaque words is ever touched — and the tool's
// verbatim output otherwise.
package cargo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/vendored"
)

const (
	// ToolName is the generator that owns the cargo.crates format.
	// doctor probes for it under this name.
	ToolName = "cargo2port"
	// LockName is the file the generator reads.
	LockName = "Cargo.lock"
)

// Crate is one entry of a cargo.crates block.
type Crate struct {
	Name    string
	Version string
	SHA256  string
}

// Distfile is the file name the cargo PortGroup gives this crate:
// ${name}-${version}.crate, matching rust::handle_crates. Versions
// carrying build metadata (0.17.0+1.8.1) pass through unaltered, as they
// do there.
func (c Crate) Distfile() string { return c.Name + "-" + c.Version + ".crate" }

// Crates parses an evaluated cargo.crates option. Entry order is
// preserved.
func Crates(option string) ([]Crate, error) {
	words, errs := syntax.ListValues(option)
	if len(errs) != 0 {
		return nil, fmt.Errorf("%w: %s: %w", vendored.ErrMalformed, vendored.CargoCrates, errs[0])
	}
	if len(words)%3 != 0 {
		return nil, fmt.Errorf("%w: %s holds %d words, not whole triples", vendored.ErrMalformed, vendored.CargoCrates, len(words))
	}
	crates := make([]Crate, 0, len(words)/3)
	for i := 0; i < len(words); i += 3 {
		crates = append(crates, Crate{Name: words[i], Version: words[i+1], SHA256: words[i+2]})
	}
	return crates, nil
}

// SuppliedIn is the set of distfile names an evaluated cargo.crates
// option contributes, in the form vendored.Own subtracts. It is the
// composition every consumer wants: any intent that must tell a port's
// own distfiles from the block's needs exactly this, and each keeping
// its own copy of the parse-then-derive dance is how they drift.
func SuppliedIn(option string) ([]string, error) {
	if option == "" {
		return nil, nil
	}
	crates, err := Crates(option)
	if err != nil {
		return nil, err
	}
	return Supplied(crates), nil
}

// Supplied is the set of distfile names a block contributes, in the form
// vendored.Own subtracts.
func Supplied(crates []Crate) []string {
	names := make([]string, 0, len(crates))
	for _, c := range crates {
		names = append(names, c.Distfile())
	}
	return names
}

// Lockfile reads Cargo.lock out of a port's own distfiles, returning the
// contents and the distfile that carried it. worksrcdir picks between
// copies when an archive holds more than one.
func Lockfile(ctx context.Context, tools *tool.Finder, archives []string, worksrcdir string) (data []byte, from string, err error) {
	return distfile.Extract(ctx, tools, archives, worksrcdir, LockName)
}

// Generate writes a cargo.crates block from lockfile contents, returning
// the command text with no trailing newline — the shape that replaces a
// located block's span.
//
// Every token comes from the tool; the caller may then re-lay the
// whitespace under a proven Geometry (Reformat), because the tree's
// blocks are written by more rules than the tool has flags — a script
// column layout regenerated in the tool's own rewrites every line it
// did not change.
//
// The lockfile is passed as bytes rather than a path because it comes
// from inside a distfile: the caller extracted it, and staging it under
// root keeps every caller from having to. The tool is resolved through
// the run's finder; a miss is ErrNoGenerator naming the tool, and a
// failed run reads "vendored: cargo2port: <stderr>".
func Generate(ctx context.Context, tools *tool.Finder, root tempdir.Root, lock []byte, layout Layout) ([]byte, error) {
	bin, err := tools.Find(tool.Cargo2Port)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", vendored.ErrNoGenerator, ToolName)
	}
	dir, remove, err := root.MakeDir("cargo2port")
	if err != nil {
		return nil, err
	}
	defer remove()
	path := filepath.Join(dir, LockName)
	if err := os.WriteFile(path, lock, 0o644); err != nil {
		return nil, err
	}

	out, _, err := tool.Output(ctx, bin, tool.Opts{Args: []string{layout.alignFlag(), path}})
	if err != nil {
		return nil, fmt.Errorf("vendored: %s: %s", ToolName, err) //nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
	}
	return vendored.ValidateBlock(out, vendored.CargoCrates)
}
