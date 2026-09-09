package classify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portstyle"
)

// THE COUNT COLUMN HAS TO BE A COLUMN. The width was a literal 14 and
// "bitbucket.setup" is fifteen, so the two longest style names pushed
// their counts one place right of everybody else's.
func TestTheCensusCountsLineUpUnderALongStyleName(t *testing.T) {
	c := &Census{}
	for i := 0; i < 3; i++ {
		c.Add(Result{Outcome: Located, Style: portstyle.VersionLine})
	}
	c.Add(Result{Outcome: Located, Style: portstyle.BitbucketSetup})

	var cols []int
	for _, line := range strings.Split(c.String(), "\n") {
		if !strings.HasPrefix(line, "  ") || !strings.Contains(line, "setup") && !strings.Contains(line, "version") {
			continue
		}
		cols = append(cols, strings.LastIndex(line, " "))
	}
	require.Len(t, cols, 2, "both style rows")
	assert.Equal(t, cols[0], cols[1], "the counts share a column whatever the label's length")
}
