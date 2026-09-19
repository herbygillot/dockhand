// Package patchcheck tells whether a port's patch files still apply to a
// source archive before any build is attempted. It extracts only the files
// the patches name and runs the system patch command in check mode, so the
// answer costs seconds instead of a provisioned build. A rejected patch is a
// finding for the maintainer, never a refusal of the version.
package patchcheck
