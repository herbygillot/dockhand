package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAMissingFileIsEmptyAndCreatesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	f, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, File{}, f)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUnknownAndMalformedSettingsAreRefusedByName(t *testing.T) {
	dir := t.TempDir()
	unknown := filepath.Join(dir, "unknown.toml")
	require.NoError(t, os.WriteFile(unknown, []byte("worktrees = \"/w\"\n[serve]\nupdates = \"check\"\n"), 0o600))
	_, err := Load(unknown)
	require.ErrorContains(t, err, "unknown setting serve.updates")

	relative := filepath.Join(dir, "relative.toml")
	require.NoError(t, os.WriteFile(relative, []byte("worktrees = \"src/branches\"\n"), 0o600))
	_, err = Load(relative)
	require.ErrorContains(t, err, "not an absolute path")

	wrongType := filepath.Join(dir, "type.toml")
	require.NoError(t, os.WriteFile(wrongType, []byte("worktrees = 3\n"), 0o600))
	_, err = Load(wrongType)
	require.Error(t, err)
}

func TestHomeIsExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("worktrees = \"~/src/macports-branches\"\n"), 0o600))
	f, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "src", "macports-branches"), f.Worktrees)
}

func TestSetWorktreesKeepsTheRestOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	require.NoError(t, SetWorktrees(path, "/src/branches"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "worktrees = \"/src/branches\"\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.WriteFile(path, []byte("# mine\nworktrees = \"/old\"   \n"), 0o600))
	require.NoError(t, SetWorktrees(path, "/new"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# mine\nworktrees = \"/new\"\n", string(data), "the line is replaced in place")

	require.Error(t, SetWorktrees(path, "relative"), "only absolute directories")
}

func TestPathFollowsTheEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PathVariable, "")
	path, err := Path()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".dockhand", "config.toml"), path)
	t.Setenv(PathVariable, "/elsewhere/config.toml")
	path, err = Path()
	require.NoError(t, err)
	require.Equal(t, "/elsewhere/config.toml", path)
}

func TestCheckAndProviderSettings(t *testing.T) {
	f, err := parse("config.toml", "[check]\non = [\"command\"]\ntests = \"required\"\n\n[providers.command]\nrun = \"~/bin/build-ports\"\n")
	require.NoError(t, err)
	require.Equal(t, []string{"command"}, f.Check.On)
	require.Equal(t, "required", f.Check.Tests)
	require.Equal(t, "~/bin/build-ports", f.Providers.Command.Run)
	require.Equal(t, "command", f.Providers.Command.Name)

	_, err = parse("config.toml", "[check]\ntests = \"sometimes\"\n")
	require.ErrorContains(t, err, "check.tests")
	_, err = parse("config.toml", "[providers.command]\nname = \"box\"\n")
	require.ErrorContains(t, err, "providers.command.run")
	_, err = parse("config.toml", "[providers.prefix]\npath = \"x\"\n")
	require.ErrorContains(t, err, "unknown setting providers.prefix.path")

	f, err = parse("config.toml", "[providers.tart]\ncapacity = 2\ntest_timeout = \"45m\"\n")
	require.NoError(t, err)
	require.Equal(t, 2, f.Capacity("tart"))
	require.Equal(t, 45*time.Minute, f.Providers.Tart.Timeout())
	f, err = parse("config.toml", "")
	require.NoError(t, err)
	require.Equal(t, 1, f.Capacity("tart"), "macOS runs two VMs, the person's own among them")
	require.Zero(t, f.Providers.Tart.Timeout())
	_, err = parse("config.toml", "[providers.tart]\ntest_timeout = \"soon\"\n")
	require.ErrorContains(t, err, "providers.tart.test_timeout")
	_, err = parse("config.toml", "[providers.tart]\ncapacity = -1\n")
	require.ErrorContains(t, err, "providers.tart.capacity")
}

func TestCleanupSettings(t *testing.T) {
	f, err := parse("config.toml", "")
	require.NoError(t, err)
	require.True(t, f.Cleanup.On())
	require.Equal(t, DefaultCleanupAfter, f.Cleanup.Age())

	f, err = parse("config.toml", "[cleanup]\nautomatic = false\nafter = \"3d\"\n")
	require.NoError(t, err)
	require.False(t, f.Cleanup.On())
	require.Equal(t, 72*time.Hour, f.Cleanup.Age())
	f, err = parse("config.toml", "[cleanup]\nafter = \"36h\"\n")
	require.NoError(t, err)
	require.Equal(t, 36*time.Hour, f.Cleanup.Age())

	for _, bad := range []string{"0d", "soon", "-1h"} {
		_, err = parse("config.toml", "[cleanup]\nafter = \""+bad+"\"\n")
		require.ErrorContains(t, err, "cleanup.after", bad)
	}

	require.Equal(t, uint64(30_000_000_000), f.Cleanup.Free(), "30 GB when unset, as Finder counts it")
	for value, bytes := range map[string]uint64{"50GB": 50e9, "12g": 12e9, "1TB": 1e12, " 2 T ": 2e12, "1.5TB": 1.5e12, "500MB": 500e6, "64GiB": 64 << 30, "1 tib": 1 << 40} {
		f, err = parse("config.toml", "[cleanup]\nmin_free = \""+value+"\"\n")
		require.NoError(t, err, value)
		require.Equal(t, bytes, f.Cleanup.Free(), value)
	}
	for _, bad := range []string{"30", "0GB", "lots", "30KB", "-5GB", "GB", "1.2.3GB", "0.0000000001MB"} {
		_, err = parse("config.toml", "[cleanup]\nmin_free = \""+bad+"\"\n")
		require.ErrorContains(t, err, "cleanup.min_free", bad)
	}
}

// A size or an age past what dockhand counts in is refused, as any value
// that isn't one is: 16777216TiB is 2^64 bytes, which wrapped around to
// none, and 106752 days is past a duration's 292 years, which wrapped
// around to a negative age, before which cleanup would prune everything.
func TestASettingThatOverflowsIsRefused(t *testing.T) {
	for _, bad := range []string{"18446745TB", "18446744074GB", "16777216TiB", "99999999999999999999GB"} {
		_, err := parse("config.toml", "[cleanup]\nmin_free = \""+bad+"\"\n")
		require.EqualError(t, err, "config.toml: cleanup.min_free: \""+bad+"\" is not a size such as 30GB")
	}
	f, err := parse("config.toml", "[cleanup]\nmin_free = \"18446744TB\"\n")
	require.NoError(t, err, "the largest that fits")
	require.Equal(t, uint64(18446744e12), f.Cleanup.Free())

	for _, bad := range []string{"106752d", "9223372036854775807d"} {
		_, err := parse("config.toml", "[cleanup]\nafter = \""+bad+"\"\n")
		require.EqualError(t, err, "config.toml: cleanup.after: \""+bad+"\" is not a number of days, such as 7d")
	}
	f, err = parse("config.toml", "[cleanup]\nafter = \"106751d\"\n")
	require.NoError(t, err)
	require.Positive(t, f.Cleanup.Age())
}

func TestMaintainerAndSubmitSettings(t *testing.T) {
	f, err := parse("config.toml", "maintainer = \"{@ada example.org:ada} openmaintainer\"\n\n[submit]\nrerequest_review = \"always\"\n")
	require.NoError(t, err)
	require.Equal(t, "{@ada example.org:ada} openmaintainer", f.Maintainer)
	require.Equal(t, "always", f.Submit.RerequestReview)
	_, err = parse("config.toml", "maintainer = \"ada@example.org\"\n")
	require.NoError(t, err)

	for _, bad := range []string{"{@ada example.org:ada", "@ada}", "{a {b}}", "a}b", "[exec true]", " "} {
		_, err = parse("config.toml", "maintainer = \""+bad+"\"\n")
		require.ErrorContains(t, err, "maintainer:", bad)
	}
	_, err = parse("config.toml", "[submit]\nrerequest_review = \"sometimes\"\n")
	require.ErrorContains(t, err, "submit.rerequest_review")
}

func TestMaintainersAreTheIdentitiesNamed(t *testing.T) {
	f, err := parse("config.toml", "maintainer = \"{@ada example.org:ada} openmaintainer\"\n")
	require.NoError(t, err)
	require.Equal(t, []string{"@ada", "example.org:ada"}, f.Maintainers())
	require.Empty(t, File{}.Maintainers())
}

func TestServeSettings(t *testing.T) {
	f, err := parse("config.toml", "")
	require.NoError(t, err)
	require.Equal(t, "list", f.Serve.Mode())
	hour, minute := f.Serve.Time()
	require.Equal(t, [2]int{7, 0}, [2]int{hour, minute})
	require.Equal(t, 10, f.Serve.Limit())
	require.True(t, f.Serve.Notifies())

	f, err = parse("config.toml", "[serve]\nfor_outdated = \"check\"\noutdated_at = \"6:30\"\nsubmit_passing = true\nsubmit_limit = 3\nnotify = false\n")
	require.NoError(t, err)
	require.Equal(t, "check", f.Serve.Mode())
	hour, minute = f.Serve.Time()
	require.Equal(t, [2]int{6, 30}, [2]int{hour, minute})
	require.True(t, f.Serve.SubmitPassing)
	require.Equal(t, 3, f.Serve.Limit())
	require.False(t, f.Serve.Notifies())

	for setting, bad := range map[string]string{"for_outdated": `"often"`, "outdated_at": `"25:00"`, "submit_limit": "-1"} {
		_, err = parse("config.toml", "[serve]\n"+setting+" = "+bad+"\n")
		require.ErrorContains(t, err, "serve."+setting)
	}
}
