package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Who decides that a verification must ignore the port's binary
// archive. The intent answers first and the run may add itself to the
// set, never remove itself from it: an archive that predates the change
// is a liar whatever the caller wanted, and bump's --recheck reaches the
// refresh's situation by a different verb.
func TestPolicyFromSourceOnlyWidens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy Policy
		intent string
		want   bool
	}{
		{"a plain bump builds against the archive", Policy{}, "bump", false},
		{"a revision bump does too", Policy{}, "bump-revision", false},
		{"a refresh never does", Policy{}, "refresh-checksums", true},
		{"--recheck says so for a bump", Policy{FromSource: true}, "bump", true},
		{"and cannot unsay a refresh", Policy{FromSource: true}, "refresh-checksums", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.policy.fromSource(tc.intent))
		})
	}
}
