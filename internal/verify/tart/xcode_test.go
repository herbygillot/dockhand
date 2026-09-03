package tart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/tool"
)

// stubTart stands in for the tart binary, recording every argv it is
// handed and answering with out and code. The returned reader is the
// transcript.
//
// The transcript is what makes the use_xcode road testable at all: a
// probe that ran leaves a line in it and a probe that did not leaves it
// empty, which is the only way to tell an answer that came from the
// capability from one that came from the guest — both are just a bool.
func stubTart(t *testing.T, out string, code int) (*tool.Finder, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	bin := filepath.Join(dir, "tart")
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	// A heredoc, so the answer's own words reach stdout unread by the
	// shell — xcodebuild's output has no interesting characters, but a
	// stub that quietly ate one would fake a pass.
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\ncat <<'DOCKHAND_STUB_EOF'\n%sDOCKHAND_STUB_EOF\nexit %d\n",
		log, out, code)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))

	finder := tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return bin, nil
		}
		return "", fmt.Errorf("this test resolves only tart, not %s", name)
	})
	return finder, func() []string {
		b, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
}

// A base nothing has spoken for is asked, and asked exactly as it was
// asked before the capability existed: one `tart exec` of xcodebuild in
// the booted worker. Nothing populates the map today, so this is the
// live road for every base on every machine, and the argv is the claim
// that routing the fact through the capability moved no bytes.
func TestHasXcodeAsksTheGuestWhenNothingSpeaksForTheBase(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	tools, transcript := stubTart(t, "Xcode 16.2\nBuild version 16C5032a\n", 0)

	p := Provider{Bases: []Base{{VM: "dockhand-base-sequoia", Release: seq}}, Tools: tools}
	assert.True(t, p.hasXcode(t.Context(), seq, "dockhand-worker-1"))
	assert.Equal(t, []string{"exec dockhand-worker-1 /usr/bin/xcodebuild -version"}, transcript(),
		"the guest is asked the same question, once")
}

// The guest's answer is read the way it always has been: the word
// Xcode in the output of a command that succeeded. A toolchain without
// a full Xcode answers from xcode-select and says so in lower case,
// which is exactly why the test is a substring and exactly why the case
// matters.
func TestHasXcodeReadsTheGuestsAnswerUnchanged(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	for _, tc := range []struct {
		name string
		out  string
		code int
		want bool
	}{
		{"a full Xcode names itself", "Xcode 16.2\nBuild version 16C5032a\n", 0, true},
		{"command line tools only", "xcode-select: note: no developer tools were found\n", 0, false},
		{"a guest that could not answer", "Xcode 16.2\n", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools, transcript := stubTart(t, tc.out, tc.code)
			p := Provider{Bases: []Base{{VM: "dockhand-base-sequoia", Release: seq}}, Tools: tools}
			assert.Equal(t, tc.want, p.hasXcode(t.Context(), seq, "dockhand-worker-1"))
			assert.Len(t, transcript(), 1, "the answer came from the guest")
		})
	}
}

// A base the provider was told about is answered from the capability,
// and the guest is never asked. The told-false case is the one that
// matters: the stub would answer that this worker has a full Xcode, and
// the telling wins, because the map is the provider's own statement
// about its images and not a cache of what a guest last said.
func TestHasXcodeAnswersFromTheCapabilityWithoutAskingTheGuest(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	yes, no := true, false

	for _, tc := range []struct {
		name string
		told *bool
		want bool
	}{
		{"told it has one", &yes, true},
		{"told it has none", &no, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools, transcript := stubTart(t, "Xcode 26.1\n", 0)
			p := Provider{
				Bases: []Base{{VM: "dockhand-base-sequoia", Release: seq, Xcode: tc.told}},
				Tools: tools,
			}
			assert.Equal(t, tc.want, p.hasXcode(t.Context(), seq, "dockhand-worker-1"))
			assert.Empty(t, transcript(), "the capability answered, so no guest was asked")
		})
	}
}

// Being told about one base says nothing about another. A machine that
// records an Xcode for its newest image must still ask about the older
// ones, or the first recorded fact would silently answer for every
// release the provider holds.
func TestHasXcodeAsksPerRelease(t *testing.T) {
	seq, _ := platform.ByName("Sequoia")
	son, _ := platform.ByName("Sonoma")
	yes := true
	tools, transcript := stubTart(t, "xcode-select: note: no developer tools were found\n", 0)

	p := Provider{Bases: []Base{
		{VM: "dockhand-base-sequoia", Release: seq, Xcode: &yes},
		{VM: "dockhand-base-sonoma", Release: son},
	}, Tools: tools}

	assert.True(t, p.hasXcode(t.Context(), seq, "dockhand-worker-1"))
	assert.Empty(t, transcript())

	assert.False(t, p.hasXcode(t.Context(), son, "dockhand-worker-2"))
	assert.Equal(t, []string{"exec dockhand-worker-2 /usr/bin/xcodebuild -version"}, transcript(),
		"the unspoken release is asked, and only it")
}
