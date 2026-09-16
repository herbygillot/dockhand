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

type PortInfo struct {
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
}

func (s Snapshot) RequiresXcode() (bool, error) {
	port, ok := s.Ports[s.Target.Name]
	if !ok {
		return false, fmt.Errorf("%w: evaluated target %s is missing", ErrTarget, s.Target.Name)
	}
	if failure, ok := port.OptionErrors["use_xcode"]; ok {
		return false, fmt.Errorf("macports: evaluating use_xcode for %s: %s", s.Target.Name, failure)
	}
	switch strings.ToLower(strings.TrimSpace(port.Options["use_xcode"])) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("macports: invalid use_xcode value %q for %s", port.Options["use_xcode"], s.Target.Name)
	}
}
