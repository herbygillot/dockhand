package macports

import (
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

type Dependency struct {
	Port  string
	Phase string
	Spec  string
}

// FetchSemantics describes inspected target registration without running hooks.
// Rejected means an unconditional rejection guard is active in this context.
type FetchSemantics struct {
	Kind      string
	Procedure string
	Guards    []string `json:",omitempty"`
	Rejected  bool     `json:",omitempty"`
	Problem   string   `json:",omitempty"`
}

type PortInfo struct {
	Fetch        *FetchSemantics `json:",omitempty"`
	Name         string
	Version      string
	Revision     int
	Epoch        int
	Options      map[string]string // Evaluated Tcl values; list-valued options retain their list encoding.
	OptionErrors map[string]string
	Dependencies []Dependency
}

type Snapshot struct {
	Runtime    Runtime
	Source     record.Source
	Target     record.Target
	Platform   record.Platform
	Ports      map[string]PortInfo
	ObservedAt time.Time
	// Root is the directory the evaluation ran in, which option values
	// such as filespath name absolutely. Comparisons normalize each
	// snapshot by its own root, so two evaluations of the same tree in
	// different directories compare equal. It is not persisted; a snapshot
	// read back has none, and normalization then leaves values as they are.
	Root string `json:"-"`
}

func (s Snapshot) RequiresXcode() (bool, error) {
	port, ok := s.Ports[s.Target.Name]
	if !ok {
		return false, fmt.Errorf("%w: evaluated target %s is missing", ErrTarget, s.Target.Name)
	}
	value, err := port.Bool("use_xcode")
	if err != nil {
		return false, fmt.Errorf("%w for %s", err, s.Target.Name)
	}
	return value, nil
}

// Bool reads an evaluated option as Tcl reads a boolean: yes, true, on,
// and 1 are true, no, false, off, 0, and an unset option are false, in
// any case, and anything else is an error, as is an option the
// evaluation could not settle. Every reader of use_xcode and
// extract.rename goes through it, so no site keeps its own list.
func (p PortInfo) Bool(option string) (bool, error) {
	if failure, ok := p.OptionErrors[option]; ok {
		return false, fmt.Errorf("macports: evaluating %s: %s", option, failure)
	}
	switch strings.ToLower(strings.TrimSpace(p.Options[option])) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	}
	return false, fmt.Errorf("macports: invalid %s value %q", option, p.Options[option])
}
