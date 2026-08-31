//go:build !darwin

package platform

// PhysicalCores on a non-darwin host answers zero — "the host will not
// say" — which consumers already treat as sizing nothing. dockhand
// runs on macOS; this stub exists so the codebase stays honest to its
// own hermetic-CI rule: everything builds on a machine with nothing
// but Go, Linux runners included.
func PhysicalCores() int { return 0 }
