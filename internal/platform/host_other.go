//go:build !darwin

package platform

// physicalCores on a non-darwin host answers zero — "the host will
// not say" — and is unreachable anyway behind PhysicalCores'
// HostIsMac gate. dockhand runs on macOS; this stub exists so the
// codebase stays honest to its own hermetic-CI rule: everything
// builds on a machine with nothing but Go, Linux runners included.
func physicalCores() int { return 0 }
