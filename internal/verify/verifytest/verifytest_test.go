package verifytest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The optional capabilities are optional in fact, not only in the
// documentation: a caller's branch for a provider that cannot answer
// is dead code until some double actually cannot.
func TestIncapableAnswersOnlyTheContract(t *testing.T) {
	var v verify.Verifier = Incapable{Fake: &Fake{}}

	_, isLister := v.(verify.WorkerLister)
	assert.False(t, isLister, "Incapable must not list workers")
	_, isExecutor := v.(verify.Executor)
	assert.False(t, isExecutor, "Incapable must not reach inside an environment")
	_, isManifester := v.(verify.Manifester)
	assert.False(t, isManifester, "Incapable must not describe an installation")
	_, isProber := v.(verify.Prober)
	assert.False(t, isProber, "Incapable must not run a port's own binaries")

	var full verify.Verifier = &Fake{}
	_, isLister = full.(verify.WorkerLister)
	assert.True(t, isLister, "Fake must list workers, or nothing exercises the audit")
	_, isManifester = full.(verify.Manifester)
	assert.True(t, isManifester, "Fake must answer manifests, or nothing exercises the comparison")
	_, isProber = full.(verify.Prober)
	assert.True(t, isProber, "Fake must answer probes, or nothing exercises them")
}

func TestFakeWorkersAreScripted(t *testing.T) {
	f := &Fake{Live: []verify.Worker{{Name: "dockhand-worker-1", Owner: "/elsewhere"}}}
	got, err := f.Workers(t.Context())
	require.NoError(t, err)
	assert.Equal(t, f.Live, got)

	// A machine that will not answer is not an idle machine, and the
	// double has to be able to say so.
	boom := errors.New("no answer")
	_, err = (&Fake{WorkersErr: boom}).Workers(t.Context())
	require.ErrorIs(t, err, boom)
}

func TestFakeManifestsAreScripted(t *testing.T) {
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	want := verify.Manifests{
		Baseline:       &verify.Manifest{Port: "jq", Version: "1.7", Platform: "darwin 24 arm64"},
		BaselineSource: verify.BaselineArchive,
		Installed: &verify.Manifest{Port: "jq", Version: "1.8", Platform: "darwin 24 arm64",
			Files:  []string{"/opt/local/bin/jq", "/opt/local/lib/libjq.1.dylib"},
			Dylibs: []verify.Dylib{{Path: "/opt/local/lib/libjq.1.dylib", InstallName: "/opt/local/lib/libjq.1.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.0.0"}}},
		Links: map[string]map[string][]string{
			"oniguruma": {"/opt/local/lib/libjq.1.dylib": {"/opt/local/bin/onig"}},
		},
	}
	f := &Fake{Inventory: map[string]verify.Manifests{job.ID: want}}
	got, err := f.Manifests(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// The refusal is a state too, and it carries its reason: "none" alone
	// is the shape of a guess, and the finding a caller writes from it
	// has to say which unavailability this was.
	declined := verify.Manifests{
		Installed:      want.Installed,
		BaselineSource: verify.BaselineNone,
		BaselineReason: "the archive was never published for this frame",
	}
	got, err = (&Fake{Inventory: map[string]verify.Manifests{job.ID: declined}}).Manifests(t.Context(), job)
	require.NoError(t, err)
	assert.Nil(t, got.Baseline)
	assert.Equal(t, "the archive was never published for this frame", got.BaselineReason)

	// Nothing to compare is a state a real provider reports, so a job
	// nobody scripted must not look like a job nobody knows.
	empty, err := (&Fake{}).Manifests(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, verify.Manifests{}, empty)

	boom := errors.New("the guest stopped answering")
	_, err = (&Fake{ManifestsErr: map[string]error{job.ID: boom}}).Manifests(t.Context(), job)
	require.ErrorIs(t, err, boom)
}

func TestFakeProbesAreScriptedPerPort(t *testing.T) {
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	jq := []verify.ProbeLine{{Binary: "/opt/local/bin/jq", Argv: "jq --version", Output: "jq-1.8\n"}}
	oniguruma := []verify.ProbeLine{{Binary: "/opt/local/bin/onig-config", Argv: "onig-config --version", Output: "6.9.9\n"}}
	f := &Fake{Probes: map[string]map[string][]verify.ProbeLine{job.ID: {"jq": jq, "oniguruma": oniguruma}}}

	// Each member of a cohort is probed as itself: a double that
	// answered the same lines for every port would let a caller that
	// mixed two ports up still pass.
	got, err := f.Probe(t.Context(), job, "jq")
	require.NoError(t, err)
	assert.Equal(t, jq, got)
	got, err = f.Probe(t.Context(), job, "oniguruma")
	require.NoError(t, err)
	assert.Equal(t, oniguruma, got)

	got, err = f.Probe(t.Context(), job, "unprobed")
	require.NoError(t, err)
	assert.Empty(t, got, "a port nothing was run against has no lines, which is not an error")

	boom := errors.New("the guest stopped answering")
	_, err = (&Fake{ProbeErr: map[string]error{job.ID: boom}}).Probe(t.Context(), job, "jq")
	require.ErrorIs(t, err, boom)
}

// Evidence and Xcode are the provider's statements about itself, so a
// test standing in for a provider has to be able to make them — and a
// fake that says nothing must keep answering exactly as it always has,
// because every engine test that reads capabilities constructs one.
func TestFakeCapabilitiesCarryEvidenceAndXcode(t *testing.T) {
	seq, ok := platform.ByName("Sequoia")
	require.True(t, ok)

	plain := (&Fake{}).Capabilities()
	assert.Empty(t, plain.Evidence)
	assert.Empty(t, plain.Xcode)

	caps := (&Fake{Evidence: "built in a pristine VM", Xcode: map[platform.Release]bool{seq: true}}).Capabilities()
	assert.Equal(t, "built in a pristine VM", caps.Evidence)
	assert.True(t, caps.Xcode[seq])

	// An unmentioned release is one the provider has not been told
	// about, not one it knows has no Xcode. Nothing may read the
	// missing entry as a no.
	son, ok := platform.ByName("Sonoma")
	require.True(t, ok)
	has, known := caps.Xcode[son]
	assert.False(t, known, "an absent release must be absent, not false")
	assert.False(t, has)
}

// The subject rides on a Status, so scripting one needs no new field:
// a cohort's provider names the member its verdict is about, and the
// double has to be able to name one too.
func TestFakeStatusCarriesASubject(t *testing.T) {
	job := verify.Job{Provider: "fake", ID: "fake-1"}
	f := &Fake{States: map[string]verify.Status{
		job.ID: {State: verify.Failed, Handle: job.ID, Subject: "oniguruma"},
	}}
	st, err := f.Poll(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, "oniguruma", st.Subject)

	// Empty is what every provider says today, and callers fall back to
	// the request when they hear it.
	st, err = (&Fake{}).Poll(t.Context(), job)
	require.NoError(t, err)
	assert.Empty(t, st.Subject)
}

// Declaring the capability and implementing the interface are two
// different facts, and the fake has to be able to hold them apart.
//
// The provider that could describe an installation and does not say so
// is a real state — a provider reconfigured between the submit that
// decided and the settle that asks — and a caller must refuse it by name
// rather than discover it as a request nobody can answer. With the
// declaration wired to the method it would be untestable.
func TestTheManifestCapabilityIsDeclaredSeparatelyFromTheMethod(t *testing.T) {
	quiet := &Fake{}
	assert.False(t, quiet.Capabilities().InstalledManifest,
		"a fake that says nothing must not set Request.Manifest across the tree")
	_, isManifester := any(quiet).(verify.Manifester)
	assert.True(t, isManifester, "and it implements the interface all the same")

	assert.True(t, (&Fake{CanManifest: true}).Capabilities().InstalledManifest)
}
