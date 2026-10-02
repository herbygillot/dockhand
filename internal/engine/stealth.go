package engine

import "github.com/herbygillot/dockhand/internal/editprep"

// Stealth is a stealth update (Design v3 §6.5): a distfile that changed
// upstream without a new name. The editor finds and makes it, given the
// files the branch has changed since its base (Update).
type Stealth = editprep.Stealth

// StealthDistfile is one archive's checksums before and after.
type StealthDistfile = editprep.StealthDistfile
