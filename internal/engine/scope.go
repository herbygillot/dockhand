package engine

import "github.com/herbygillot/dockhand/internal/macports"

// Scope is what a set of changed paths touches, by MacPorts CI's rule,
// which is macports' (macports.ScopeOf).
type Scope = macports.Scope
