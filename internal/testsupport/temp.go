package testsupport

import "os"

// IsolateTemp points TMPDIR at a fresh directory for a test binary, so the
// process's run root, which internal/scratch makes under the temporary
// directory and every transient directory lives in, stays out of the
// person's, where the next dockhand command would sweep it as a dead
// root. Call it from TestMain before m.Run; the returned function removes
// the directory.
//
// It is for the binary, not one test: the run root is the process's, made
// under the temporary directory of the moment, so a test can't have its
// own without a t.Setenv, which keeps it from running in parallel.
func IsolateTemp() func() {
	temp, err := os.MkdirTemp("", "dockhand-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TMPDIR", temp)
	return func() { os.RemoveAll(temp) }
}
