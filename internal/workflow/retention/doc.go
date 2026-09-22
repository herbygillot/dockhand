// Package retention is the housekeeping the engine hands off to: releasing
// terminal verification resources past their age, pruning their diagnostic
// files and the providers' log caches, and deleting the local branches
// merged contributions left behind. It reads and writes the store and the
// repository and calls providers through what the engine gives it, and it
// knows neither the engine nor the cycle: releasing a resource goes through
// the engine's claimed release path, handed in as a function. The
// branch-deletion decision lives here once, for the contribution lifecycle
// that settles a merged contribution's cleanup and the sweep that catches
// what it missed.
package retention
