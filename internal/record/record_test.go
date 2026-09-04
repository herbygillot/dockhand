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

// The gate asks after every member and not only after the run map. A
// cohort reaches shapes one subject cannot — a pass beside a member
// nothing ever built — and the run arithmetic reads those as
// promotable, which publishes a port on evidence that does not exist.
//
// What it asks of a DEPENDENT changed on 2026-09-04: the dependents are
// best effort and do not gate on their outcome, only on having reached
// one. A dependent that failed, was blocked, or errored is published
// over and named on stderr and in the body; a dependent still building,
// or with no run at all, is still a hole and still blocks. The headline
// is unchanged and is not best effort.
func TestPromotableAnswersForEverySubject(t *testing.T) {
	cohort := []Subject{{Port: "jq"}, {Port: "oniguruma"}}
	for _, tc := range []struct {
		name     string
		subjects []Subject
		runs     map[string]Run
		want     bool
	}{
		{"both members passed", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Passed}}, true},
		// Best effort: these reached an outcome, and the outcome does
		// not gate. Each is stated to the author before the pull request
		// exists and to the reviewer in its body.
		{"a dependent blocked by a stranger", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Blocked}}, true},
		{"a dependent the guest said nothing about", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Errored}}, true},
		{"a dependent that failed", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Failed}}, true},
		// And these did not reach one. No outcome is not a best-effort
		// outcome: the guest is still mid-way through answering, or
		// nobody ever asked.
		{"a dependent still queued", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Queued}}, false},
		{"a dependent still running", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Running}}, false},
		{"a dependent with no run at all", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}}, false},
		// The headline is the change, and none of the above applies.
		{"the headline failed", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Failed}, RunKey("oniguruma", "Testos"): {State: Passed}}, false},
		// A port that declined every platform it was asked about has
		// said the change is right about it, which is the unsupported
		// rule read per member.
		{"a member that declines every platform", cohort, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Unsupported}}, true},
		{"a member proven on one platform and declining the other", cohort, map[string]Run{
			RunKey("jq", "Testos"):        {State: Passed},
			RunKey("oniguruma", "Testos"): {State: Unsupported},
			RunKey("oniguruma", "Oldos"):  {State: Passed}}, true},
		// The map hands its keys over in no order, so a member's pass
		// must be found whichever run is met first.
		{"a member proven on one platform and canceled on the other", cohort, map[string]Run{
			RunKey("jq", "Testos"):        {State: Passed},
			RunKey("oniguruma", "Testos"): {State: Passed},
			RunKey("oniguruma", "Oldos"):  {State: Canceled}}, true},
		// A note naming no subjects is answered by the runs alone: it
		// was written by something that does not name them, and a roster
		// guessed out of the keys would be a guess that blocks.
		{"a record that names no subjects", nil, map[string]Run{
			RunKey("jq", "Testos"): {State: Passed}, RunKey("oniguruma", "Testos"): {State: Blocked}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Record{Subjects: tc.subjects, Runs: tc.runs}.Promotable())
		})
	}
}

// A cohort that holds two ports MacPorts will not activate together
// builds one of them and bumps both. If the held-back member counted as
// unanswered, the change could never be published — and since most
// cohorts hold such a pair, the gate would refuse nearly everything,
// which protects nothing.
func TestAWithheldMemberDoesNotBlockPromotion(t *testing.T) {
	r := Record{
		Subjects: []Subject{{Port: "libraw"}, {Port: "gegl"}, {Port: "gegl-devel"}},
		Runs: map[string]Run{
			RunKey("libraw", "Testos"):     {State: Passed, Platform: "Testos"},
			RunKey("gegl", "Testos"):       {State: Passed, Platform: "Testos"},
			RunKey("gegl-devel", "Testos"): {State: Withheld, Platform: "Testos"},
		},
	}
	assert.True(t, r.Promotable(),
		"a member held back from one guest is answered for, not unanswered")
}

// What it must not do is paper over a member nobody has an answer for.
// Withheld says dockhand chose not to ask; queued says nobody has asked
// yet, and that is still a change with a hole in it.
func TestWithheldDoesNotExcuseAnUnansweredMember(t *testing.T) {
	r := Record{
		Subjects: []Subject{{Port: "libraw"}, {Port: "gegl-devel"}, {Port: "gthumb"}},
		Runs: map[string]Run{
			RunKey("libraw", "Testos"):     {State: Passed, Platform: "Testos"},
			RunKey("gegl-devel", "Testos"): {State: Withheld, Platform: "Testos"},
			RunKey("gthumb", "Testos"):     {State: Queued, Platform: "Testos"},
		},
	}
	assert.False(t, r.Promotable(), "gthumb has no verdict, and withholding a sibling does not give it one")
}

// A dependent that failed does not block: its revision is owed because
// the library it links moved, and whether it builds today is usually a
// fact about the dependent. gthumb was already broken on the platform
// when this was measured, and a gate that held the change for it would
// make a cohort hostage to the least maintained port in it.
func TestAFailedDependentDoesNotBlockPromotion(t *testing.T) {
	r := Record{
		Subjects: []Subject{{Port: "libraw"}, {Port: "gegl-devel"}, {Port: "gthumb"}},
		Runs: map[string]Run{
			RunKey("libraw", "Testos"):     {State: Passed, Platform: "Testos"},
			RunKey("gegl-devel", "Testos"): {State: Withheld, Platform: "Testos"},
			RunKey("gthumb", "Testos"):     {State: Failed, Platform: "Testos"},
		},
	}
	assert.True(t, r.Promotable(), "the dependents are best effort; the body says which did not pass")
}

// The headline is not best effort. It is the change.
func TestAFailedHeadlineStillBlocks(t *testing.T) {
	r := Record{
		Subjects: []Subject{{Port: "libraw"}, {Port: "gegl"}},
		Runs: map[string]Run{
			RunKey("libraw", "Testos"): {State: Failed, Platform: "Testos"},
			RunKey("gegl", "Testos"):   {State: Passed, Platform: "Testos"},
		},
	}
	assert.False(t, r.Promotable(), "a dependent passing does not answer for the port that broke")
}

// Best effort is about outcomes, not about waiting. A dependent still
// building has no outcome at all, and publishing over it would put a
// body in front of a reviewer that its own guest is mid-way through
// disproving.
func TestADependentStillBuildingBlocks(t *testing.T) {
	r := Record{
		Subjects: []Subject{{Port: "libraw"}, {Port: "gegl"}},
		Runs: map[string]Run{
			RunKey("libraw", "Testos"): {State: Passed, Platform: "Testos"},
			RunKey("gegl", "Testos"):   {State: Running, Platform: "Testos"},
		},
	}
	assert.False(t, r.Promotable(), "no outcome is not a best-effort outcome")
}
