package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlatformsSortsForStableRendering(t *testing.T) {
	r := Record{Runs: map[string]Run{
		"Testos":    {State: Passed},
		"Ancientos": {State: Unsupported},
		"Oldos":     {State: Deferred},
	}}
	assert.Equal(t, []string{"Ancientos", "Oldos", "Testos"}, r.Platforms())
}

func TestPlatformsOfAnEmptyRecord(t *testing.T) {
	assert.Empty(t, Record{}.Platforms())
}

func TestAnyState(t *testing.T) {
	r := Record{Runs: map[string]Run{
		"Testos": {State: Passed},
		"Oldos":  {State: Blocked},
	}}
	assert.True(t, r.AnyState(Passed))
	assert.True(t, r.AnyState(Blocked))
	assert.False(t, r.AnyState(Failed))
	assert.False(t, Record{}.AnyState(Passed), "a record with no runs is in no state")
}

func TestPromotable(t *testing.T) {
	for _, tc := range []struct {
		name string
		runs map[string]Run
		want bool
	}{
		{"a single pass", map[string]Run{"Testos": {State: Passed}}, true},
		{"a pass beside a port that declines the platform",
			map[string]Run{"Testos": {State: Passed}, "Oldos": {State: Unsupported}}, true},
		{"a pass beside a dependency that blocked the test",
			map[string]Run{"Testos": {State: Passed}, "Oldos": {State: Blocked}}, true,
		},
		{"a pass beside a run still going",
			map[string]Run{"Testos": {State: Passed}, "Oldos": {State: Running}}, true},
		{"a failure alongside the pass, which is the question review asks",
			map[string]Run{"Testos": {State: Passed}, "Oldos": {State: Failed}}, false},
		{"nothing passed yet", map[string]Run{"Testos": {State: Running}}, false},
		{"the machine could not answer", map[string]Run{"Testos": {State: Errored}}, false},
		{"no runs at all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Record{Runs: tc.runs}.Promotable())
		})
	}
}
