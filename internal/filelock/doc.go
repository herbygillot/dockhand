// Package filelock provides context-aware advisory locks for files and external
// resources shared by Dockhand processes.
//
// Acquire initializes the lock path and waits for shared or exclusive ownership.
// TryExisting attempts an existing lock without waiting or creating paths, so
// maintenance can skip active work. Callers close the returned file to release
// ownership and preserve the lock path so cooperating processes lock the same
// file.
package filelock
