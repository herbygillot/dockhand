package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecideMint(t *testing.T) {
	cases := []struct {
		name                    string
		edits, force, hasBranch bool
		want                    MintDecision
		probes                  bool
	}{
		// A no-op realized as a branch would be an empty commit.
		{name: "no edits", want: NothingToMint},
		// --replace replaces only when there is something to replace it
		// with, and a user who asked and got silence would reasonably
		// believe it happened.
		{name: "no edits, forced", force: true, want: NothingToReplace},
		{name: "no edits, forced, a branch standing", force: true, hasBranch: true, want: NothingToReplace},
		{name: "edits, nothing standing", edits: true, want: MintBranch, probes: false},
		{name: "edits, a branch standing, no force", edits: true, hasBranch: true, want: MintBranch},
		{name: "edits, forced, nothing standing", edits: true, force: true, want: MintBranch, probes: true},
		{name: "edits, forced, a branch standing", edits: true, force: true, hasBranch: true,
			want: ReplaceThenMint, probes: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DecideMint(tc.edits, tc.force, tc.hasBranch))
			assert.Equal(t, tc.probes, MintProbesBranch(tc.edits, tc.force),
				"the branch probe costs a git call and is only worth it for a forced mint that has something to mint")
		})
	}
}

// The probe is what makes the two-step safe: whenever it says no, the
// decision does not depend on the answer, so a caller may pass false.
func TestMintProbeCoversEveryBranchQuestion(t *testing.T) {
	for _, edits := range []bool{false, true} {
		for _, force := range []bool{false, true} {
			if MintProbesBranch(edits, force) {
				continue
			}
			assert.Equal(t, DecideMint(edits, force, false), DecideMint(edits, force, true),
				"unprobed, the standing branch must not change the decision")
		}
	}
}
