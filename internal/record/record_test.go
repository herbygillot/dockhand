package record

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/assert"
)

func TestHeadlineIsTheFirstSubject(t *testing.T) {
	r := Record{Subjects: []Subject{
		{Port: "libwidget", Target: "3.0"},
		{Port: "widget-tools", Target: "rev2"},
	}}
	assert.Equal(t, "libwidget", r.Headline().Port)
	assert.Equal(t, "3.0", r.Headline().Target)
}

func TestHeadlineOfARecordWithNoSubjects(t *testing.T) {
	// The zero Subject names no port, which is the same answer an empty
	// record gives everywhere else.
	assert.Equal(t, Subject{}, Record{}.Headline())
}

func TestPortsKeepBuildOrder(t *testing.T) {
	// Not sorted: the order is the order a cohort must be built in, and
	// Ports[0] is the headline.
	r := Record{Subjects: []Subject{{Port: "libwidget"}, {Port: "widget-tools"}, {Port: "aardvark"}}}
	assert.Equal(t, []string{"libwidget", "widget-tools", "aardvark"}, r.Ports())
	assert.Empty(t, Record{}.Ports())
}

func TestPortdirsAreStageable(t *testing.T) {
	// This projection feeds staging, so it drops the empties and the
	// repeats: staging one directory twice is at best wasted work, and
	// staging "" is not a thing to do at all.
	r := Record{Subjects: []Subject{
		{Port: "libwidget", Portdir: "devel/libwidget"},
		{Port: "libwidget-tools", Portdir: "devel/libwidget"},
		{Port: "aardvark"},
		{Port: "zebra", Portdir: "science/zebra"},
	}}
	assert.Equal(t, []string{"devel/libwidget", "science/zebra"}, r.Portdirs())
	assert.Empty(t, Record{}.Portdirs())
}

func TestPlatformsProjectTheJobsAndNotTheRuns(t *testing.T) {
	// Three subjects on two platforms is two environments. Reading the
	// run keys would answer six, with the wrong words in them.
	r := Record{
		Jobs: map[string]JobRecord{
			"Testos":    {Job: verify.Job{ID: "fake-1"}},
			"Ancientos": {Job: verify.Job{ID: "fake-2"}},
		},
		Runs: map[string]Run{
			RunKey("jq", "Testos"):           {State: Passed, Platform: "Testos"},
			RunKey("oniguruma", "Testos"):    {State: Passed, Platform: "Testos"},
			RunKey("jq", "Ancientos"):        {State: Unsupported, Platform: "Ancientos"},
			RunKey("oniguruma", "Ancientos"): {State: Unsupported, Platform: "Ancientos"},
		},
	}
	assert.Equal(t, []string{"Ancientos", "Testos"}, r.Platforms(), "sorted for stable rendering")
}

func TestPlatformsOfAnEmptyRecord(t *testing.T) {
	assert.Empty(t, Record{}.Platforms())
}

func TestAnyState(t *testing.T) {
	r := Record{Runs: map[string]Run{
		RunKey("jq", "Testos"): {State: Passed},
		RunKey("jq", "Oldos"):  {State: Blocked},
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
		{"a single pass", map[string]Run{RunKey("jq", "Testos"): {State: Passed}}, true},
		{"a pass beside a port that declines the platform", map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("jq", "Oldos"): {State: Unsupported}}, true},
		{"a pass beside a dependency that blocked the test", map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("jq", "Oldos"): {State: Blocked}}, true},
		{"a pass beside a run still going", map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("jq", "Oldos"): {State: Running}}, true},
		{"a pass beside a run still being submitted", map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("jq", "Oldos"): {State: Submitting}}, true},
		{"one member of the cohort failed, which is the question review asks", map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Failed}}, false},
		{"nothing passed yet", map[string]Run{RunKey("jq", "Testos"): {State: Running}}, false},
		{"the machine could not answer", map[string]Run{RunKey("jq", "Testos"): {State: Errored}}, false},
		{"no runs at all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Record{Runs: tc.runs}.Promotable())
		})
	}
}
