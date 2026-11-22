package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A branch started at a detached HEAD is named for the first port its
// commits change, by the tree's one rule for a port's directory: a
// dotfile directory's path, as .github/workflows, names no port, where it
// named one called "workflows" (#287).
func TestABranchStartedAtHeadIsNamedForAPortNotADotfileDirectory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	testsupport.Git(t, f.clone, "checkout", "-q", "--detach")
	testsupport.WriteFiles(t, f.clone, map[string]string{".github/workflows/main.yml": "on: push\n"})
	testsupport.Git(t, f.clone, "add", "-A")
	testsupport.Git(t, f.clone, "commit", "-qm", "ci: a workflow")
	_, err := e.StartAtHead(t.Context())
	require.ErrorContains(t, err, "this checkout's commits change no port")

	testsupport.WriteFiles(t, f.clone, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	testsupport.Git(t, f.clone, "commit", "-qam", "jq: update to 1.8.1")
	adopted, err := e.StartAtHead(t.Context())
	require.NoError(t, err)
	require.Contains(t, adopted.Branch.Name, "jq")
	require.NotContains(t, adopted.Branch.Name, "workflows")
}
