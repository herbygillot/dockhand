// Package macports evaluates and resolves ports through native MacPorts Base.
//
// Tree and Context bind a materialized source to its immutable identity, target,
// variants, and platform. Evaluator uses a Tcl child to obtain port metadata,
// dependencies, fetch information, version comparisons, and runtime capability
// diagnostics. Evaluation requires the native platform. Source conventions,
// Portfile edits, indexes, and installation have separate subpackages.
package macports
