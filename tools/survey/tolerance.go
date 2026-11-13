package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/model"
)

const toleranceVersion = 1

type tolerancePair struct {
	On, Off         observe.Explanation
	OnLeft, OffLeft bool
	Parity          bool
}

type toleranceSummary struct {
	Evaluation      int64
	Platform        model.Platform
	On, Off         bool
	OnLeft, OffLeft bool
	Parity          bool
}

type capturedFile struct {
	Hash string
	Data []byte `json:"-"`
}

type toleranceCapture struct {
	Evaluation  int64 `json:"-"`
	Version     int
	Target      model.Target
	Platform    model.Platform
	Modeled     bool
	Unanswered  string
	Error       string `json:",omitempty"`
	ErrorCode   string `json:",omitempty"`
	Contents    string
	Root, Base  string
	Aliases     map[string]string
	Files       map[string]capturedFile
	Observation macports.PortObservation
	Pair        tolerancePair
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func captureTolerance(ctx context.Context, target model.Target, platform model.Platform, modeled bool, unanswered string, port macports.PortObservation, contents []byte, root string) (*toleranceCapture, error) {
	on, off, err := measurePolicies(ctx, port, contents, root)
	if err != nil {
		return nil, err
	}
	c := &toleranceCapture{Version: toleranceVersion, Target: target, Platform: platform, Modeled: modeled, Unanswered: unanswered, Contents: string(contents), Root: root, Base: port.BaseLibrary, Observation: port, Files: map[string]capturedFile{}, Aliases: map[string]string{}, Pair: tolerancePair{On: on, Off: off, OnLeft: modeled && (unanswered != "" || on.Inconclusive), OffLeft: modeled && (unanswered != "" || off.Inconclusive), Parity: true}}
	alias := func(original, role string) {
		if original == "" {
			return
		}
		c.Aliases[filepath.Clean(original)] = role
		if resolved, e := filepath.EvalSymlinks(original); e == nil {
			c.Aliases[resolved] = role
		}
	}
	alias(root, "source")
	alias(filepath.Join(root, macports.PortGroupDirectory), filepath.Join("source", macports.PortGroupDirectory))
	alias(port.BaseLibrary, "base")
	put := func(role string, data []byte) {
		c.Files[filepath.ToSlash(role)] = capturedFile{Hash: digest(data), Data: data}
	}
	put(filepath.Join("source", target.Portfile), contents)
	if port.BaseLibrary != "" {
		files, e := filepath.Glob(filepath.Join(port.BaseLibrary, "port1.0", "*.tcl"))
		if e != nil {
			return nil, e
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("capture: Base port1.0 sources are absent")
		}
		for _, file := range files {
			data, e := os.ReadFile(file)
			if e != nil {
				return nil, e
			}
			put(filepath.Join("base", "port1.0", filepath.Base(file)), data)
		}
	}
	// Capture exactly the PortGroup files the current classifier may read.
	// The complete Portfile and Base sources retain the known #247 boundary.
	groupDir := filepath.Join(root, macports.PortGroupDirectory)
	if resolved, e := filepath.EvalSymlinks(groupDir); e == nil {
		groupDir = resolved
	}
	for _, d := range port.Declarations {
		for _, f := range d.Frames {
			file := f.File
			if resolved, e := filepath.EvalSymlinks(file); e == nil {
				file = resolved
			}
			if file != "" && filepath.Dir(file) == groupDir {
				data, e := os.ReadFile(file)
				if e != nil {
					return nil, e
				}
				put(filepath.Join("source", macports.PortGroupDirectory, filepath.Base(file)), data)
			}
		}
	}
	return c, nil
}

// materialize uses only captured bytes. No original tree or Base path is
// opened; all frame paths and embedded source references are remapped.
func (c *toleranceCapture) materialize(parent string) (macports.PortObservation, string, error) {
	if c.Version != toleranceVersion {
		return macports.PortObservation{}, "", fmt.Errorf("unsupported tolerance capture version %d", c.Version)
	}
	var aliases []string
	for old := range c.Aliases {
		aliases = append(aliases, old)
	}
	sort.Slice(aliases, func(i, j int) bool { return len(aliases[i]) > len(aliases[j]) })
	replace := func(value string) string {
		for _, old := range aliases {
			value = strings.ReplaceAll(value, old, filepath.Join(parent, c.Aliases[old]))
		}
		return value
	}
	for role, f := range c.Files {
		if !filepath.IsLocal(role) {
			return macports.PortObservation{}, "", fmt.Errorf("capture contains nonlocal path %q", role)
		}
		if digest(f.Data) != f.Hash {
			return macports.PortObservation{}, "", fmt.Errorf("capture file checksum mismatch: %s", role)
		}
		file := filepath.Join(parent, filepath.FromSlash(role))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return macports.PortObservation{}, "", err
		}
		if err := os.WriteFile(file, []byte(replace(string(f.Data))), 0o400); err != nil {
			return macports.PortObservation{}, "", err
		}
	}
	port := c.Observation
	port.BaseLibrary = replace(port.BaseLibrary)
	port.Declarations = append([]macports.Declaration(nil), port.Declarations...)
	for i, d := range port.Declarations {
		d.Frames = append([]macports.SourceFrame(nil), d.Frames...)
		for j := range d.Frames {
			d.Frames[j].File = replace(d.Frames[j].File)
			d.Frames[j].Command = replace(d.Frames[j].Command)
		}
		d.Values = append([]string(nil), d.Values...)
		for j := range d.Values {
			d.Values[j] = replace(d.Values[j])
		}
		port.Declarations[i] = d
	}
	port.Problems = append([]string(nil), port.Problems...)
	for i := range port.Problems {
		port.Problems[i] = replace(port.Problems[i])
	}
	return port, filepath.Join(parent, "source"), nil
}
