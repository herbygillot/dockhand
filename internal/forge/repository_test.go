package forge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The forge's sentinels are said to a person where they surface, so none
// leads with the package's name: serve said "could not read pull requests:
// forge: authentication is required" (D-C3 on rc8, 2026-10-29).
func TestNoSentinelLeadsWithThePackagesName(t *testing.T) {
	for _, sentinel := range []error{ErrRejected, ErrAuthentication, ErrNotFound, ErrAppRestricted, ErrIncomplete, ErrAccessUnknown} {
		require.False(t, strings.HasPrefix(sentinel.Error(), "forge"), sentinel.Error())
	}
}
