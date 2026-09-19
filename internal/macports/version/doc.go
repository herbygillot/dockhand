// Package version owns how Dockhand reads a version spelling: whether it is
// a usable spelling at all, how an upstream tag maps to and from it, and
// whether it is stable or a prerelease. Ordering is not here on purpose:
// MacPorts compares versions with its own vercmp through the evaluator.
package version
