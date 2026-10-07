//go:build !acceptance

package failpoint

// Enabled says whether this build has failpoints: a normal build hasn't.
const Enabled = false

// Hit does nothing in a normal build.
func Hit(string) {}

// Fails asks nothing of a step in a normal build.
func Fails(string) string { return "" }
