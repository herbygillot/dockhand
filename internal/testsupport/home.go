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

// liveTart are the variables that opt a test binary into its live Tart
// tests.
var liveTart = []string{"DOCKHAND_TEST_TART_LIVE", "DOCKHAND_TEST_BOOTSTRAP_VM", "DOCKHAND_TEST_TART_IMAGE"}

// IsolateHomeKeepingTart is IsolateHome for packages with opt-in live Tart
// tests: where one is asked for, it first pins dockhand's Tart home,
// DOCKHAND_TART_HOME, its SSH keys, DOCKHAND_SSH_DIR, and the person's Tart
// home, TART_HOME, unless set, to the real ones, so those tests still find
// and reach their images, and share their locks. Otherwise it's
// IsolateHome, and the locks the other tests take stay out of the
// person's ~/.dockhand.
func IsolateHomeKeepingTart() func() {
	live := false
	for _, name := range liveTart {
		live = live || os.Getenv(name) != ""
	}
	if home, err := os.UserHomeDir(); err == nil && live {
		if os.Getenv("DOCKHAND_TART_HOME") == "" {
			os.Setenv("DOCKHAND_TART_HOME", filepath.Join(home, ".dockhand", "tart"))
		}
		if os.Getenv("DOCKHAND_SSH_DIR") == "" {
			os.Setenv("DOCKHAND_SSH_DIR", filepath.Join(home, ".dockhand", "ssh"))
		}
		if os.Getenv("TART_HOME") == "" {
			os.Setenv("TART_HOME", filepath.Join(home, ".tart"))
		}
	}
	return IsolateHome()
}
