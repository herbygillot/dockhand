package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ONE IDENTITY FOR THE WHOLE PROCESS. Me used to take the instant as a
// parameter so a resident dispatcher could restamp OwnerID.Since on
// every pass; lease.sameProcess admits a gap of at most one minute
// between a recorded Since and the kernel's fork time, so from its
// second minute of life such a dispatcher read as DEAD — and therefore
// seizable — to every peer that asked about it.
func TestMeIsStableAcrossCalls(t *testing.T) {
	born := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	ticks := []time.Time{born, born.Add(time.Hour), born.Add(9 * time.Hour)}
	i := 0
	s := &Services{TreeRoot: t.TempDir(), Tools: testFinder(), born: born,
		Now: func() time.Time { t := ticks[i%len(ticks)]; i++; return t }}

	first, second, third := s.Me(), s.Me(), s.Me()
	assert.Equal(t, born, first.Since, "the process's birth, not this call's instant")
	assert.Equal(t, first, second)
	assert.Equal(t, first, third, "and the clock moving nine hours does not move it")
}

// The pass distinction the restamp was reaching for lives on the pass
// token, which is a report field by construction and never a seize
// condition — record.Claim.Pass says so.
func TestTheProcessIdentityCarriesNoPassToken(t *testing.T) {
	born := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	s := &Services{TreeRoot: t.TempDir(), Tools: testFinder(), born: born, Now: time.Now}
	me := s.Me()
	assert.NotZero(t, me.PID)
	assert.Equal(t, born, me.Since)
}
