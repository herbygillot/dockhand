package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/config"
)

func TestConfigShowsWhatIsInEffect(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	out, _, err := cmdtest.Dockhand(t, "config")
	require.NoError(t, err)
	require.Contains(t, out, "File      ~/.dockhand/config.toml\nDatabase  ~/.dockhand/dockhand.db\n")
	setting := func(key, value string) string {
		return `(?m)^` + regexp.QuoteMeta(key) + ` +` + regexp.QuoteMeta(value) + `$`
	}
	require.Regexp(t, setting("submit.rerequest_review", "ask  (default)"), out)
	require.Regexp(t, setting("cleanup.after", "15d  (default)"), out)
	require.Regexp(t, setting("serve.notify", "true  (default)"), out)
	require.Regexp(t, setting("providers.tart.test_timeout", "30m  (default)"), out)
	require.Regexp(t, setting("providers.tart.keep_failed", "true  (default)"), out)
	require.Regexp(t, setting("check.baseline", "false  (default)"), out)
	require.Regexp(t, setting("providers.tart.xcode.sonoma", "15.4, as MacPorts' arm64 buildbot runs  (default)"), out)

	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada example.org:ada} openmaintainer\"\n[cleanup]\nautomatic = false\n[serve]\nnotify = false\nsubmit_limit = 3\n[check]\nbaseline = false\n[providers.tart]\nkeep_failed = false\n[providers.tart.xcode]\n14 = \"16.2\"\n"), 0o644))
	out, _, err = cmdtest.Dockhand(t, "config")
	require.NoError(t, err)
	require.Regexp(t, setting("maintainer", "{@ada example.org:ada} openmaintainer"), out)
	require.Regexp(t, setting("cleanup.automatic", "false"), out)
	require.Regexp(t, setting("serve.notify", "false"), out)
	require.Regexp(t, setting("serve.submit_limit", "3"), out)
	// An explicit false is the file's, not the default it equals.
	require.Regexp(t, setting("check.baseline", "false"), out)
	require.Regexp(t, setting("update.keep_http", "false  (default)"), out)
	require.Regexp(t, setting("providers.tart.keep_failed", "false"), out)
	require.NotContains(t, out, "providers.tart.capacity", "the Mac's VMs are Tart's limit, no setting")
	require.Regexp(t, setting("providers.tart.xcode.sonoma", "16.2"), out, "by release number")
	require.Regexp(t, setting("providers.tart.xcode.tahoe", "26.6, as MacPorts' arm64 buildbot runs  (default)"), out)

	// Each program dockhand runs, as named: the file's [tools], a
	// variable, --tool, else the default search (the roadmap's "--tool").
	require.Regexp(t, setting("tools.git", "git on PATH  (default)"), out)
	git := filepath.Join(w.Home, "git")
	require.NoError(t, os.WriteFile(git, []byte("#!/bin/sh\n"), 0o755))
	cmdtest.Setenv(t, "GIT_BIN", git)
	out, _, err = cmdtest.Dockhand(t, "config", "--tool", "diff="+git)
	require.NoError(t, err)
	require.Regexp(t, setting("tools.git", git+"  (GIT_BIN)"), out)
	require.Regexp(t, setting("tools.diff", git+"  (--tool diff)"), out)
	cmdtest.Setenv(t, "GIT_BIN", "")

	result, err := cmdtest.JSONOf(t, "config")
	require.NoError(t, err)
	require.Equal(t, "maintainer", cmdtest.Dig(t, result.Result, "settings", 1, "key"))
	require.Equal(t, "file", cmdtest.Dig(t, result.Result, "settings", 1, "source"))

	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("maintainer = \"{@ada\"\n"), 0o644))
	_, _, err = cmdtest.Dockhand(t, "config")
	require.ErrorContains(t, err, "maintainer: \"{@ada\" leaves a group open")
}

// Every setting config shows has its default in config's table, apart
// from each release's Xcode, which the facts table gives.
func TestEverySettingHasItsDefaultInTheTable(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	out, _, err := cmdtest.Dockhand(t, "config")
	require.NoError(t, err)
	defaults := config.Defaults()
	_, shared, _ := strings.Cut(out, "\n\n")
	for _, line := range strings.Split(strings.TrimSpace(shared), "\n") {
		key := strings.Fields(line)[0]
		if !strings.HasPrefix(key, "providers.tart.xcode.") {
			require.Contains(t, defaults, key)
		}
	}
}
