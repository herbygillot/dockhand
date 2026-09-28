// Package progress carries transient operation messages through a context to an
// optional observer.
//
// Report serializes callbacks from concurrent operations. Observers must
// return promptly and avoid recursive reporting. Messages describe activity
// in the current process; what lasts is recorded in the store, and read
// from there.
package progress
