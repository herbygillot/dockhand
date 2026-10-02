package testsupport

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Git runs git in dir for a test's fixture and returns what it printed,
// trimmed, failing the test where git fails. It is the one way tests set
// up repositories: commits by a fixed author, master the first branch,
// and nothing of the person's git configuration, hooks, signing, or GIT_
// variables reaching it, so a fixture is the same on every Mac.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	flags := []string{"-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "init.defaultBranch=master", "-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-c", "core.hooksPath=" + os.DevNull}
	cmd := exec.Command("git", append(flags, args...)...)
	cmd.Dir = dir
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}
