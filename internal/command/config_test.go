package command

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigShowsWhatIsInEffect(t *testing.T) {
	w := newWorld(t)
	out, _, err := dockhand(t, "config")
	require.NoError(t, err)
	require.Contains(t, out, "File      ~/.dockhand/config.toml\nDatabase  ~/.dockhand/dockhand.db\n")
	setting := func(key, value string) string {
		return `(?m)^` + regexp.QuoteMeta(key) + ` +` + regexp.QuoteMeta(value) + `$`
	}
	require.Regexp(t, setting("submit.rerequest_review", "ask  (default)"), out)
	require.Regexp(t, setting("cleanup.after", "15d  (default)"), out)
	require.Regexp(t, setting("serve.notify", "true  (default)"), out)
	require.Regexp(t, setting("providers.tart.test_timeout", "30m  (default)"), out)
	require.Regexp(t, setting("providers.tart.xcode.sonoma", "15.4, as MacPorts' arm64 buildbot runs  (default)"), out)

	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n[cleanup]\nautomatic = false\n[serve]\nnotify = false\nsubmit_limit = 3\n[providers.tart]\ncapacity = 2\n[providers.tart.xcode]\n14 = \"16.2\"\n"), 0o644))
	out, _, err = dockhand(t, "config")
	require.NoError(t, err)
	require.Regexp(t, setting("maintainer", "{@ada example.org:ada} openmaintainer"), out)
	require.Regexp(t, setting("cleanup.automatic", "false"), out)
	require.Regexp(t, setting("serve.notify", "false"), out)
	require.Regexp(t, setting("serve.submit_limit", "3"), out)
	require.Regexp(t, setting("providers.tart.capacity", "2"), out)
	require.Regexp(t, setting("providers.tart.xcode.sonoma", "16.2"), out, "by release number")
	require.Regexp(t, setting("providers.tart.xcode.tahoe", "26.6, as MacPorts' arm64 buildbot runs  (default)"), out)

	result, err := jsonOf(t, "config")
	require.NoError(t, err)
	require.Equal(t, "maintainer", dig(t, result.Result, "settings", 1, "key"))
	require.Equal(t, "file", dig(t, result.Result, "settings", 1, "source"))

	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada\"\n"), 0o644))
	_, _, err = dockhand(t, "config")
	require.ErrorContains(t, err, "maintainer: \"{@ada\" leaves a group open")
}
