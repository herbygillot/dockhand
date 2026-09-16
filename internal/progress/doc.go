// Package progress carries transient operation messages through a context to an
// optional observer.
//
// Report includes the current scope and serializes callbacks from concurrent
// operations. Observers must return promptly and avoid recursive reporting.
// Messages describe activity in the current process; durable workflow status is
// read separately from state.
package progress
