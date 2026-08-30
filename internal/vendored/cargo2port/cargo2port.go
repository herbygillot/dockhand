// Package cargo2port is the cargo side of vendored dependency blocks:
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
// crate, and a regenerated block is taken from the tool verbatim.
package cargo2port

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/vendored"
)

const (
	// ToolName is the generator that owns the cargo.crates format.
	// doctor probes for it under this name.
	ToolName = "cargo2port"
	// LockName is the file the generator reads.
	LockName = "Cargo.lock"
	// AlignFlag asks the generator for the column layout the ports tree
	// actually uses: the crate name left-aligned, the version right-
	// aligned against a common column. It is not the tool's default,
	// and the difference is not cosmetic in the way it looks — a block
	// written in one layout and regenerated in another rewrites every
	// line it did not change. 224 of the tree's cargo blocks are in
	// this layout against 5 in the alternative, and generating tokei's
	// block with it reproduces the committed one byte for byte.
	AlignFlag = "--align=justify"
	// Kind is the block this package generates.
	Kind = vendored.CargoCrates
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
		return nil, fmt.Errorf("%w: %s: %w", vendored.ErrMalformed, Kind, errs[0])
	}
	if len(words)%3 != 0 {
		return nil, fmt.Errorf("%w: %s holds %d words, not whole triples", vendored.ErrMalformed, Kind, len(words))
	}
	crates := make([]Crate, 0, len(words)/3)
	for i := 0; i < len(words); i += 3 {
		crates = append(crates, Crate{Name: words[i], Version: words[i+1], SHA256: words[i+2]})
	}
	return crates, nil
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
func Lockfile(ctx context.Context, archives []string, worksrcdir string) (data []byte, from string, err error) {
	return distfile.Extract(ctx, archives, worksrcdir, LockName)
}

// Generate writes a cargo.crates block from lockfile contents, returning
// the command text with no trailing newline — the shape that replaces a
// located block's span.
//
// The block is taken from the tool verbatim; what this package chooses
// is the layout to ask for, not how to lay it out. Reflowing the output
// would mean parsing what it has no business reading, but requesting the
// tree's own alignment keeps a regenerated block's diff to the crates
// that actually moved.
//
// The lockfile is passed as bytes rather than a path because it comes
// from inside a distfile: the caller extracted it, and staging it under
// root keeps every caller from having to.
func Generate(ctx context.Context, root tempdir.Root, lock []byte) ([]byte, error) {
	bin, err := exec.LookPath(ToolName)
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

	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, AlignFlag, path)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("vendored: %s: %s", ToolName, msg)
	}
	return vendored.ValidateBlock(out.Bytes(), Kind)
}
