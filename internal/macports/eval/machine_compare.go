package eval

import (
	"reflect"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// Only names and registration text are compared; positions and private
// diagnostic text cannot affect a plan and never leave the operation.
func machineHooks(p macports.PortObservation) []macports.Hook {
	var hooks []macports.Hook
	for _, h := range p.Hooks {
		if strings.Contains(h.Procedure, "-org.macports.fetch-") || strings.Contains(h.Procedure, "-org.macports.checksum-") {
			h.Origin = ""
			hooks = append(hooks, h)
		}
	}
	return hooks
}
func machinePhaseGuard(port macports.PortObservation) string {
	for _, h := range machineHooks(port) {
		if strings.Contains(h.Procedure, "-org.macports.checksum-") {
			return "checksum phase"
		}
	}
	return ""
}
func compareMachinePlans(a, b macports.PortInfo, x, y macports.PortObservation, acted []string) (string, []string) {
	for _, field := range []struct {
		name string
		a, b any
	}{
		{"name", a.Name, b.Name}, {"version", a.Version, b.Version}, {"epoch", a.Epoch, b.Epoch}, {"revision", a.Revision, b.Revision},
		{"distfiles", x.Distfiles, y.Distfiles},
	} {
		if !reflect.DeepEqual(field.a, field.b) {
			return field.name, nil
		}
	}
	declarations := func(p macports.PortObservation) []macports.Declaration {
		var out []macports.Declaration
		for _, d := range p.Declarations {
			if d.Command == "revision" || d.Command == "checksums" {
				d.Frames = nil
				out = append(out, d)
			}
		}
		return out
	}
	if !reflect.DeepEqual(declarations(x), declarations(y)) {
		return "declarations", nil
	}
	for _, name := range acted {
		av, aok := a.Options[name]
		bv, bok := b.Options[name]
		if av != bv || aok != bok || a.OptionErrors[name] != b.OptionErrors[name] {
			return "option." + name, nil
		}
	}
	if !reflect.DeepEqual(machineHooks(x), machineHooks(y)) {
		return "phase registrations", nil
	}
	// Positions of fetch guards are diagnostic only. Compare the live kind,
	// target procedure and rejection status, never their source locations.
	fetch := func(p macports.PortInfo) any {
		if p.Fetch == nil {
			return nil
		}
		return struct {
			Kind, Procedure string
			Rejected        bool
		}{p.Fetch.Kind, p.Fetch.Procedure, p.Fetch.Rejected}
	}
	if !reflect.DeepEqual(fetch(a), fetch(b)) {
		return "fetch", nil
	}
	var moved []string
	for _, name := range macports.ReadOptions {
		if !slices.Contains(acted, name) && (a.Options[name] != b.Options[name] || a.OptionErrors[name] != b.OptionErrors[name]) {
			moved = append(moved, name)
		}
	}
	return "", moved
}
