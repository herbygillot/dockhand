// Package eval implements MacPorts evaluation through its native Tcl runtime.
//
// Evaluator owns interpreter sessions, compatibility checks, selector resolution,
// metadata decoding, and version comparisons. It implements macports.Reader;
// source contexts and observations remain independent of this implementation.
// Evaluation observes Portfiles and does not decide which declarations to edit.
package eval
