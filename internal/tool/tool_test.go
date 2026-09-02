package tool

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These three pin the memo semantics the former package-level finder
// pinned, adapted to a Finder value: one lookup per success, misses
// never remembered, the fallback path honored.

func TestFindMemoizesOneAnswerPerFinder(t *testing.T) {
	calls := 0
	f := NewFinder(func(name string) (string, error) {
		calls++
		return "/fake/" + name, nil
	})

	p1, err := f.Find(Git)
	require.NoError(t, err)
	p2, err := f.Find(Git)
	require.NoError(t, err)
	assert.Equal(t, p1, p2)
	assert.Equal(t, 1, calls, "one fact, looked up once — the same answer doctor reported")
}

func TestFindMissesAreNotCached(t *testing.T) {
	present := false
	f := NewFinder(func(name string) (string, error) {
		if present {
			return "/fake/" + name, nil
		}
		return "", errors.New("nope")
	})

	assert.False(t, f.Have(Tart))
	present = true
	assert.True(t, f.Have(Tart), "a tool installed mid-process is found, not remembered absent")
}

func TestFindWithFallsBack(t *testing.T) {
	f := NewFinder(func(name string) (string, error) {
		if name == "/opt/local/bin/port-tclsh" {
			return name, nil
		}
		return "", errors.New("nope")
	})

	path, err := f.FindWith(PortTclsh, "/opt/local/bin/port-tclsh")
	require.NoError(t, err)
	assert.Equal(t, "/opt/local/bin/port-tclsh", path)
}
