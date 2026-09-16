// Package dependents discovers downstream verification coverage from a frozen
// ports tree.
//
// Service stages a matching PortIndex, selects direct build, library, and runtime
// dependents, and evaluates their targets. Coverage retains selection reasons,
// evaluation failures, and index gaps. Dependency closures are default-variant
// estimates; callers decide revision edits, verification plans, and scheduling.
package dependents
