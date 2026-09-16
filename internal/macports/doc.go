// Package macports defines bound ports-tree contexts and evaluated observations.
//
// Tree and Context tie a materialized snapshot to its source identity, target,
// variants, and platform. Reader is the evaluation contract used by preparation,
// discovery, and verification. The eval subpackage supplies the native MacPorts
// implementation; editing, source conventions, and installation have separate
// subpackages.
package macports
