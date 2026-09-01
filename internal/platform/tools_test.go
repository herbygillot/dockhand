package platform

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindMemoizesOneAnswerPerProcess(t *testing.T) {
	calls := 0
	restore := StubLookup(func(name string) (string, error) {
		calls++
		return "/fake/" + name, nil
	})
	defer restore()

	p1, err := Find(Git)
	require.NoError(t, err)
	p2, err := Find(Git)
	require.NoError(t, err)
	assert.Equal(t, p1, p2)
	assert.Equal(t, 1, calls, "one fact, looked up once — the same answer doctor reported")
}

func TestFindMissesAreNotCached(t *testing.T) {
	present := false
	restore := StubLookup(func(name string) (string, error) {
		if present {
			return "/fake/" + name, nil
		}
		return "", errors.New("nope")
	})
	defer restore()

	assert.False(t, Have(Tart))
	present = true
	assert.True(t, Have(Tart), "a tool installed mid-process is found, not remembered absent")
}

func TestFindWithFallsBack(t *testing.T) {
	restore := StubLookup(func(name string) (string, error) {
		if name == "/opt/local/bin/port-tclsh" {
			return name, nil
		}
		return "", errors.New("nope")
	})
	defer restore()

	path, err := FindWith(PortTclsh, "/opt/local/bin/port-tclsh")
	require.NoError(t, err)
	assert.Equal(t, "/opt/local/bin/port-tclsh", path)
}
