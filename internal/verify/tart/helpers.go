package tart

import (
	"crypto/rand"
	"encoding/hex"
)

// stamp makes a worker name unique. The pid would not do it: a guest
// outlives the process that created it, and pids are reused.
func stamp() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
