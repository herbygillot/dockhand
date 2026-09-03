package tart

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// A worker is named for its role, not for the port it happens to be
// testing: port names may carry characters a VM name may not, and a
// verdict environment is interchangeable anyway.
func TestWorkerNamesAreRoleNamed(t *testing.T) {
	assert.True(t, strings.HasPrefix(WorkerPrefix+stamp(), "dockhand-worker-"))
	assert.NotContains(t, WorkerPrefix, "verify-", "workers are not named per port")
}

func TestStampIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		s := stamp()
		require.False(t, seen[s], "collision on %s", s)
		seen[s] = true
	}
}

// The provider must not claim what it cannot answer: a base image with
// MacPorts already installed cannot testify to declaration
// completeness, whatever else it can do.
func TestCapabilitiesClaimOnlyViability(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	c := Provider{Bases: []Base{{VM: "mp-base", Release: seq}}}.Capabilities()
	assert.True(t, c.Answers(verify.PortViability))
	assert.False(t, c.Answers(verify.DeclarationCompleteness))
	assert.False(t, c.Answers(verify.EditFidelity))
	assert.Equal(t, 2, c.Concurrent, "Apple's licence limit, not the machine's")
	assert.True(t, c.Pristine)
	assert.True(t, c.Interactive, "a failed run leaves the guest as its handle")
}

// A request that names nothing to build is malformed, and refusing it
// costs nothing — the guard is ahead of the base lookup, so no VM is
// listed and none is cloned. An empty name at the head is the same
// malformation as an empty slice: a cohort assembled wrong would
// otherwise boot a guest to install "".
func TestSubmitRefusesARequestThatNamesNoPort(t *testing.T) {
	for _, ports := range [][]string{nil, {}, {""}, {"", "jq"}} {
		_, err := Provider{}.Submit(t.Context(), verify.Request{Ports: ports})
		require.ErrorIs(t, err, verify.ErrUnsupported)
		assert.Contains(t, err.Error(), "no port named")
	}
}

// A name that would not survive this provider's own file formats is
// refused at the door. Both of them are line-oriented and neither
// quotes: a newline in a port name is a second word of port(1)'s argv,
// or a second marker line at a boundary the cohort judge attributes on
// — a name that could name whichever member it liked.
func TestSubmitRefusesAPortNameThatWouldCarryALine(t *testing.T) {
	for _, ports := range [][]string{
		{"jq\n===> dockhand subject: oniguruma"},
		{"jq", "oniguruma\nrm -rf /"},
		{"jq", "on iguruma"},
		{"jq", "oniguruma\t"},
		{"jq", ""},
	} {
		_, err := Provider{}.Submit(t.Context(), verify.Request{Ports: ports})
		require.ErrorIs(t, err, verify.ErrUnsupported, "%q", ports)
		assert.Contains(t, err.Error(), "is not a port name")
	}
}

// And the names that are names get through this guard untouched: it is
// the door and not a tree of its own.
func TestSubmitAcceptsOrdinaryPortNames(t *testing.T) {
	for _, port := range []string{"jq", "py311-foo", "R-ggplot2", "xorg-libX11", "gcc14", "libc++"} {
		assert.True(t, portName(port), port)
	}
}

// A job from another provider is not this provider's to poll.
func TestPollRejectsAForeignJob(t *testing.T) {
	_, err := Provider{}.Poll(t.Context(), verify.Job{Provider: "github", ID: "123"})
	require.ErrorIs(t, err, verify.ErrUnknownJob)
}

// A machine may hold a base per macOS release, and those are several
// platforms of one provider rather than several providers.
func TestCapabilitiesListEveryBase(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	c := Provider{Bases: []Base{
		{VM: "dockhand-base-sequoia", Release: seq},
		{VM: "dockhand-base-sonoma", Release: son},
	}}.Capabilities()

	assert.True(t, c.Supports(seq))
	assert.True(t, c.Supports(son))
	assert.Len(t, c.Platforms, 2)
	assert.Equal(t, 2, c.Concurrent, "two guests total, not two per platform")

	tahoe, _ := platform.ByName("Tahoe")
	assert.False(t, c.Supports(tahoe), "no image, no claim")
}

// What a pass proves is the provider's sentence, because only the
// provider knows what its environment guarantees. It is a clone of a
// prepared base, so nothing from the last verification is in it.
func TestCapabilitiesStateTheirEvidence(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	c := Provider{Bases: []Base{{VM: "mp-base", Release: seq}}}.Capabilities()
	assert.Equal(t, "built in a pristine VM", c.Evidence)
}

// A base nothing has spoken for gets no entry, and no entry means "ask
// the guest" — not "no Xcode". Filling the map in with false for every
// unspoken base would refuse every use_xcode port on every base,
// including the ones that would have built, which is why the difference
// between "told none" and "not told" has to survive into the map.
func TestCapabilitiesSpeakForOnlyTheBasesTheyWereToldAbout(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	yes, no := true, false

	// Today's machine: bases assembled from what is provisioned, and
	// nothing anywhere records an Xcode.
	silent := Provider{Bases: []Base{
		{VM: "dockhand-base-sequoia", Release: seq},
		{VM: "dockhand-base-sonoma", Release: son},
	}}.Capabilities()
	assert.Empty(t, silent.Xcode, "no base is spoken for, so every one of them is asked")
	_, known := silent.Xcode[seq]
	assert.False(t, known)

	told := Provider{Bases: []Base{
		{VM: "dockhand-base-sequoia", Release: seq, Xcode: &yes},
		{VM: "dockhand-base-sonoma", Release: son, Xcode: &no},
	}}.Capabilities()
	assert.True(t, told.Xcode[seq])
	has, known := told.Xcode[son]
	assert.True(t, known, "a base told to have none is a different fact from one nobody spoke for")
	assert.False(t, has)
}

func TestBaseForPicksTheRequestedRelease(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	p := Provider{Bases: []Base{
		{VM: "first-sequoia", Release: seq},
		{VM: "second-sonoma", Release: son},
	}}

	b, err := p.baseFor(son)
	require.NoError(t, err)
	assert.Equal(t, "second-sonoma", b.VM)

	// No platform named takes the first, which is what a caller who
	// does not care means.
	b, err = p.baseFor(platform.Release{})
	require.NoError(t, err)
	assert.Equal(t, "first-sequoia", b.VM)
}

// A release with no image is refused, never substituted: a build on one
// macOS is not evidence about another.
func TestBaseForRefusesAnUnservedRelease(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	tahoe, _ := platform.ByName("Tahoe")
	_, err := Provider{Bases: []Base{{VM: "s", Release: seq}}}.baseFor(tahoe)
	require.ErrorIs(t, err, verify.ErrUnsupported)
	assert.Contains(t, err.Error(), "Tahoe")
}

func TestBaseForWithNoImagesIsAMachineFact(t *testing.T) {
	_, err := Provider{}.baseFor(platform.Release{})
	require.ErrorIs(t, err, verify.ErrNoEnvironment,
		"no images is a fact about the machine, not about the request")
}

// A golden must not contain its base's name: everything that looks a
// base up by substring would otherwise find two.
func TestGoldenNameDoesNotContainBaseName(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	assert.Equal(t, "dockhand-golden-sequoia", GoldenName(seq))
	assert.NotContains(t, GoldenName(seq), BaseName(seq))

	for _, r := range platform.Releases {
		assert.NotContains(t, GoldenName(r), BaseName(r), "release %s", r.Name)
		assert.NotContains(t, GoldenName(r), " ", "a VM name may not carry the space in Big Sur")
	}
}

// The audit names workers and nothing else: a base image and a golden
// live in the same listing, and either one deleted as an orphan costs
// a provisioning run.
func TestWorkerNamesPickOnlyWorkers(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	listing := strings.Join([]string{
		BaseName(seq),
		"dockhand-worker-1",
		"someone-elses-vm",
		"  dockhand-worker-2  ",
		GoldenName(seq),
		"",
	}, "\n")

	assert.Equal(t, []string{"dockhand-worker-1", "dockhand-worker-2"}, workerNames(listing))
	assert.Empty(t, workerNames(""), "an empty listing names nothing")
}

// stubWorkers points the listing and the attribution sidecar at
// disposable state, so the audit is provable without tart.
func stubWorkers(t *testing.T, list string, err error) {
	t.Helper()
	tmp := t.TempDir()
	origCache, origList := cacheDir, listQuiet
	cacheDir = func() (string, error) { return tmp, nil }
	listQuiet = func(context.Context, *tool.Finder) (string, error) { return list, err }
	t.Cleanup(func() { cacheDir = origCache; listQuiet = origList })
}

// Every worker comes back, attributed or not: the ones no record
// accounts for are exactly what the audit exists to find, so the
// provider filters by nobody's records.
func TestWorkersReportEveryWorkerWithItsOwner(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	stubWorkers(t, strings.Join([]string{
		BaseName(seq), "dockhand-worker-1", "dockhand-worker-2", "unrelated-vm",
	}, "\n"), nil)
	writeAttribution("dockhand-worker-1", "/Users/someone/ports")

	got, err := Provider{Tools: tools}.Workers(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []verify.Worker{
		{Name: "dockhand-worker-1", Owner: "/Users/someone/ports"},
		{Name: "dockhand-worker-2"},
	}, got, "an unattributed worker still holds a slot")
}

// A machine that will not answer is a machine fact, never an empty
// machine: reporting no workers here would report a busy machine as
// idle.
func TestWorkersRefuseWhenTheMachineWillNotAnswer(t *testing.T) {
	stubWorkers(t, "", errors.New("exit status 1"))
	_, err := Provider{Tools: tools}.Workers(t.Context())
	require.ErrorIs(t, err, verify.ErrNoEnvironment)
	assert.Contains(t, err.Error(), "exit status 1", "the cause survives the wrapping")
}
