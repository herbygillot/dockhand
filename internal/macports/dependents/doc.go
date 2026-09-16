// Package dependents discovers downstream verification coverage from a frozen
// ports tree.
//
// Service stages a matching PortIndex, selects direct build, library, and runtime
// dependents, and evaluates their targets. It projects verify.Coverage with tool
// requirements while keeping native evaluator and index types local. Results retain selection reasons,
// evaluation failures, and index gaps. Dependency closures are default-variant
// estimates; callers decide revision edits, verification plans, and scheduling.
package dependents
