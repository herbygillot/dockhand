package testsupport

import (
	"os"
	"path/filepath"
)

// IsolateHome points HOME at a fresh temporary directory for a test binary,
// so what dockhand keeps per user, such as its Tart image locks in
// ~/.dockhand, stays out of the person's. Call it from TestMain before
// m.Run; the returned function removes the directory.
func IsolateHome() func() {
	home, err := os.MkdirTemp("", "dockhand-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	return func() { os.RemoveAll(home) }
}

// IsolateHomeKeepingTart is IsolateHome for packages with opt-in live Tart
// tests: it first pins dockhand's Tart home, DOCKHAND_TART_HOME, and the
// person's, TART_HOME, unless set, to the real ones, so those tests still
// find their images.
func IsolateHomeKeepingTart() func() {
	if home, err := os.UserHomeDir(); err == nil {
		if os.Getenv("DOCKHAND_TART_HOME") == "" {
			os.Setenv("DOCKHAND_TART_HOME", filepath.Join(home, ".dockhand", "tart"))
		}
		if os.Getenv("TART_HOME") == "" {
			os.Setenv("TART_HOME", filepath.Join(home, ".tart"))
		}
	}
	return IsolateHome()
}
