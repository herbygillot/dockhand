package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
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

// GitEnv is the environment for a git.Program a test runs: the process's,
// but nothing of the person's git configuration, the system's, or GIT_
// variables. HOME and the global configuration are the test's own, in a
// temporary directory, where commits are by a fixed author, master is the
// first branch, and hooks and signing are off. A test that needs git to
// see one more variable, such as GIT_SSH_COMMAND, appends it, so it runs
// in parallel where t.Setenv would keep it serial. It is an environment
// rather than a git.Program because testsupport can't import
// internal/git, which imports internal/subprocess.
func GitEnv(t testing.TB) []string {
	t.Helper()
	home := t.TempDir()
	global := filepath.Join(home, ".gitconfig")
	config := "[user]\n\tname = Test\n\temail = test@example.org\n[init]\n\tdefaultBranch = master\n[commit]\n\tgpgSign = false\n[tag]\n\tgpgSign = false\n[core]\n\thooksPath = " + os.DevNull + "\n"
	require.NoError(t, os.WriteFile(global, []byte(config), 0o644))
	var env []string
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if !strings.HasPrefix(name, "GIT_") && name != "HOME" && name != "XDG_CONFIG_HOME" {
			env = append(env, variable)
		}
	}
	return append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GIT_CONFIG_GLOBAL="+global, "GIT_CONFIG_NOSYSTEM=1")
}
