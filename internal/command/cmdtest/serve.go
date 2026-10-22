package cmdtest

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// SyncBuffer is a buffer two goroutines may share.
type SyncBuffer struct {
	Mu sync.Mutex
	B  bytes.Buffer
}

func (s *SyncBuffer) Write(p []byte) (int, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.B.Write(p)
}

func (s *SyncBuffer) String() string {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.B.String()
}

func CheckedBranch(t *testing.T) World {
	t.Helper()
	w := NewWorld(t)
	Versioned(t, w)
	WithBumper(t)
	WithScript(t, w, "passed")
	Hosted(t).Timings.ServePoll = 20 * time.Millisecond
	_, _, err := Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = Dockhand(t, "update", "jq")
	require.NoError(t, err)
	return w
}
