package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("worktrees = \"~/src/macports-branches\"\n"), 0o600))
	f, err := Places{Home: home}.Load(path)
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

func TestConfigPathIsTheNamedFileElseTheHomes(t *testing.T) {
	home := t.TempDir()
	places := Places{Home: home, Dir: "/work"}
	path, err := places.ConfigPath("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".dockhand", "config.toml"), path)
	path, err = places.ConfigPath("/elsewhere/config.toml")
	require.NoError(t, err)
	require.Equal(t, "/elsewhere/config.toml", path)
	path, err = places.ConfigPath("here.toml")
	require.NoError(t, err)
	require.Equal(t, "/work/here.toml", path, "against the command line's directory")
	_, err = Places{}.ConfigPath("")
	require.ErrorContains(t, err, "$HOME is not defined")
}

// The database is the named one, else the home's, and says whether it's
// the home's, the one a release opens, by whatever path names it: a copy
// named in its place migrates unasked (field testing, 2026-10-29).
func TestDatabasePathIsTheNamedFileElseTheHomes(t *testing.T) {
	home := t.TempDir()
	byDefault := filepath.Join(home, ".dockhand", "dockhand.db")
	places := Places{Home: home, Dir: "/work"}
	path, standard, err := places.DatabasePath("")
	require.NoError(t, err)
	require.Equal(t, byDefault, path)
	require.True(t, standard)
	path, standard, err = places.DatabasePath("copy.db")
	require.NoError(t, err)
	require.Equal(t, "/work/copy.db", path, "against the command line's directory")
	require.False(t, standard)
	_, standard, err = places.DatabasePath("~/.dockhand/dockhand.db")
	require.NoError(t, err)
	require.True(t, standard, "the home's, named")

	require.NoError(t, os.MkdirAll(filepath.Dir(byDefault), 0o700))
	require.NoError(t, os.WriteFile(byDefault, nil, 0o600))
	link := filepath.Join(t.TempDir(), "linked.db")
	require.NoError(t, os.Symlink(byDefault, link))
	_, standard, err = places.DatabasePath(link)
	require.NoError(t, err)
	require.True(t, standard, "the home's, by a link")

	_, _, err = Places{}.DatabasePath("")
	require.EqualError(t, err, "$HOME is not defined; name the database with --db or DOCKHAND_DB")
	path, standard, err = Places{Dir: "/work"}.DatabasePath("/elsewhere/dockhand.db")
	require.NoError(t, err)
	require.Equal(t, "/elsewhere/dockhand.db", path)
	require.False(t, standard)
}

func TestCheckAndProviderSettings(t *testing.T) {
	f, err := parse("config.toml", "[check]\ntests = \"required\"\n\n[providers.command]\nrun = \"~/bin/build-ports\"\n")
	require.NoError(t, err)
	require.Equal(t, "required", f.Check.Tests)
	require.Equal(t, "~/bin/build-ports", f.Providers.Command.Run)
	require.Equal(t, "command", f.Providers.Command.Name)

	_, err = parse("config.toml", "[check]\ntests = \"sometimes\"\n")
	require.ErrorContains(t, err, "check.tests")
	_, err = parse("config.toml", "[providers.command]\nname = \"box\"\n")
	require.ErrorContains(t, err, "providers.command.run")
	_, err = parse("config.toml", "[providers.prefix]\npath = \"x\"\n")
	require.ErrorContains(t, err, "unknown setting providers.prefix")
	// The local provider's prefix, its toggle to keep what a check built
	// active, the prefixes it may clean, and whether serve checks there.
	f, err = parse("config.toml", "[providers.local]\nprefix = \"/opt/macports-test\"\nkeep = true\nclean = [\"/opt/macports-test\"]\nserve = true\n")
	require.NoError(t, err)
	require.Equal(t, LocalProvider{Prefix: "/opt/macports-test", Keep: true, Clean: []string{"/opt/macports-test"}, Serve: true}, f.Providers.Local)
	_, err = parse("config.toml", "[providers.local]\nprefix = \"opt/local\"\n")
	require.ErrorContains(t, err, "providers.local.prefix: \"opt/local\" is not a path from the root")
	_, err = parse("config.toml", "[providers.local]\nclean = [\"~/mp\"]\n")
	require.ErrorContains(t, err, "providers.local.clean")
	_, err = parse("config.toml", "[providers.local]\nbuild_timeout = \"soon\"\n")
	require.ErrorContains(t, err, "providers.local.build_timeout")

	f, err = parse("config.toml", "[providers.tart]\ntest_timeout = \"45m\"\n")
	require.NoError(t, err)
	require.Equal(t, 2, f.Capacity("tart"), "as many checks as the Mac runs VMs")
	require.Equal(t, 45*time.Minute, f.Providers.Tart.Timeout())
	f, err = parse("config.toml", "")
	require.NoError(t, err)
	require.Equal(t, TartCapacity, f.Capacity("tart"), "no setting: the Mac's VMs are the limit")
	require.Zero(t, f.Providers.Tart.Timeout())
	_, err = parse("config.toml", "[providers.tart]\ntest_timeout = \"soon\"\n")
	require.ErrorContains(t, err, "providers.tart.test_timeout")
	_, err = parse("config.toml", "[providers.tart]\ncapacity = 2\n")
	require.ErrorContains(t, err, "providers.tart.capacity", "the key is gone, refused by name as any unknown key is")
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
	require.Equal(t, "list", f.Serve.Stage())
	hour, minute := f.Serve.Time()
	require.Equal(t, [2]int{7, 0}, [2]int{hour, minute})
	require.Equal(t, 10, f.Serve.Limit())
	require.True(t, f.Serve.Notifies())

	f, err = parse("config.toml", "[serve]\nship_to = \"submit\"\noutdated_at = \"6:30\"\nsubmit_limit = 3\nnotify = false\n")
	require.NoError(t, err)
	require.Equal(t, "submit", f.Serve.Stage())
	hour, minute = f.Serve.Time()
	require.Equal(t, [2]int{6, 30}, [2]int{hour, minute})
	require.Equal(t, 3, f.Serve.Limit())
	require.False(t, f.Serve.Notifies())

	for setting, bad := range map[string]string{"ship_to": `"often"`, "outdated_at": `"25:00"`, "submit_limit": "-1"} {
		_, err = parse("config.toml", "[serve]\n"+setting+" = "+bad+"\n")
		require.ErrorContains(t, err, "serve."+setting)
	}
	// for_outdated and submit_passing went with ship, said with what
	// takes their place.
	_, err = parse("config.toml", "[serve]\nsubmit_passing = true\n")
	require.EqualError(t, err, `config.toml: serve.submit_passing is gone: serve.ship_to says how far serve carries your outdated ports, list, commit, check, or submit; for_outdated = "draft" is ship_to = "commit", and submit_passing = true is ship_to = "submit"`)
}

// holds.majors holds an unattended major-version update unless it's allow.
func TestHoldsMajorsHoldsUnlessAllowed(t *testing.T) {
	f, err := parse("config.toml", "")
	require.NoError(t, err)
	require.True(t, f.Holds.HoldMajors(), "held by default")
	f, err = parse("config.toml", "[holds]\nmajors = \"allow\"\n")
	require.NoError(t, err)
	require.False(t, f.Holds.HoldMajors())
	_, err = parse("config.toml", "[holds]\nmajors = \"sometimes\"\n")
	require.ErrorContains(t, err, "holds.majors")
}

// A port's own holds, [holds.ports."<port>"], read with their keys, and a
// value none of them takes refused by name (Then's item 2).
func TestAPortsOwnHoldsAreRead(t *testing.T) {
	f, err := parse("config.toml", "[holds]\nmajors = \"allow\"\n[holds.ports.\"harfbuzz\"]\nafter = [\"harfbuzz-devel\"]\nmajors = \"hold\"\n[holds.ports.\"skopeo\"]\nmoves = \"follow\"\nallow = [\"open\"]\n")
	require.NoError(t, err)
	require.Equal(t, map[string]PortHolds{"harfbuzz": {Majors: "hold", After: []string{"harfbuzz-devel"}}, "skopeo": {Moves: MovesFollow, Allow: []string{"open"}}}, f.Holds.Ports)
	for setting, words := range map[string]string{
		`majors = "sometimes"`: `holds.ports."harfbuzz".majors: "sometimes" is not hold or allow`,
		`moves = "always"`:     `holds.ports."harfbuzz".moves: "always" is not hold or follow`,
		`after = ["harfbuzz"]`: `holds.ports."harfbuzz".after: "harfbuzz" is not another port`,
	} {
		_, err = parse("config.toml", "[holds.ports.\"harfbuzz\"]\n"+setting+"\n")
		require.ErrorContains(t, err, words)
	}
	_, err = parse("config.toml", "[holds.ports.\"harfbuzz\"]\nfollow = true\n")
	require.ErrorContains(t, err, "follow", "an unknown key is refused by name")
}

// The maintainers line setup inferred is written beside what the file
// holds, and written again in its place.
func TestSetMaintainerWritesTheLineBesideWhatsThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("worktrees = \"/tmp/x\"\n\n[serve]\noutdated_at = \"07:00\"\n"), 0o600))
	require.NoError(t, SetMaintainer(path, "{gmail.com:ada @ada}"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "maintainer = \"{gmail.com:ada @ada}\"\nworktrees = \"/tmp/x\"\n\n[serve]\noutdated_at = \"07:00\"\n", string(data))
	require.NoError(t, SetMaintainer(path, "{@ada}"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "maintainer = \"{@ada}\"\nworktrees")
}

// The defaults table says what an empty file's accessors fall back on.
func TestDefaultsAreWhatTheAccessorsFallBackOn(t *testing.T) {
	var f File
	defaults := Defaults()
	hour, minute := f.Serve.Time()
	require.Equal(t, defaults["serve.outdated_at"], fmt.Sprintf("%02d:%02d", hour, minute))
	require.Equal(t, defaults["serve.ship_to"], f.Serve.Stage())
	require.Equal(t, defaults["serve.submit_limit"], strconv.Itoa(f.Serve.Limit()))
	require.Equal(t, defaults["cleanup.after"], ageWords(f.Cleanup.Age()))
	require.Equal(t, "15d", defaults["cleanup.after"])
	require.Equal(t, defaults["providers.command.capacity"], strconv.Itoa(f.Capacity("command")))
	require.Equal(t, defaults["providers.github.capacity"], strconv.Itoa(f.Capacity("github")))
	require.Equal(t, "30m", defaults["providers.tart.test_timeout"])
	require.Equal(t, "6h", defaults["providers.tart.build_timeout"])
	require.Equal(t, "30 GB", defaults["cleanup.min_free"])
	require.Equal(t, defaults["serve.notify"], strconv.FormatBool(f.Serve.Notifies()))
	require.Equal(t, defaults["cleanup.automatic"], strconv.FormatBool(f.Cleanup.On()))
	require.Equal(t, defaults["holds.majors"] == "hold", f.Holds.HoldMajors())
}

// Build profiles: dockhand's own and the person's, each a provider and
// its releases, a list or a rule; and check.on, gone, said with what took
// its place (the person's design, 2026-10-13).
func TestBuildProfiles(t *testing.T) {
	f, err := parse("config.toml", "[build]\ndefault = \"mine\"\nunattended = \"ci-exact\"\n[build.profiles]\nmine = { releases = [\"15\", \"26\"] }\nci-exact = { provider = \"github\", releases = \"macports-ci\" }\n")
	require.NoError(t, err)
	require.Equal(t, "mine", f.Build.Default)
	require.Equal(t, "ci-exact", f.Build.Unattended)
	releases, rule := f.Build.Profiles["mine"].List()
	require.Equal(t, []string{"15", "26"}, releases)
	require.Empty(t, rule)
	_, rule = f.Build.Profiles["ci-exact"].List()
	require.Equal(t, RuleMacPortsCI, rule)

	for text, refusal := range map[string]string{
		"[check]\non = [\"tart\"]\n":                                               "check.on is gone: build.default names a build profile",
		"[providers.tart]\ncapacity = 2\n":                                         "providers.tart.capacity is gone: Tart takes as many checks as the Mac runs VMs",
		"[build]\ndefault = \"elsewhere\"\n":                                       `build.default: "elsewhere" names no profile`,
		"[build.profiles]\nhost = { releases = [\"15\"] }\n":                       "host is dockhand's own profile",
		"[build.profiles]\nbox = { provider = \"vm\" }\n":                          `"vm" is not tart, github, or command`,
		"[build.profiles]\nbox = { releases = \"newest\" }\n":                      `"newest" is not a list of releases`,
		"[build.profiles]\nbox = { releases = [15] }\n":                            "15 is not a release",
		"[build.profiles]\nbox = { provider = \"github\", releases = [\"15\"] }\n": "the github provider builds where it does",
	} {
		_, err := parse("config.toml", text)
		require.ErrorContains(t, err, refusal, text)
	}
}

// Here is the working directory at or within a directory, which a header
// needn't name.
func TestPlacesHere(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "textproc", "jq")
	require.NoError(t, os.MkdirAll(inside, 0o755))
	require.True(t, Places{Dir: dir}.Here(dir))
	require.True(t, Places{Dir: inside}.Here(dir))
	require.False(t, Places{Dir: filepath.Dir(dir)}.Here(dir))
	require.False(t, Places{Dir: dir + "-other"}.Here(dir))
	require.False(t, Places{}.Here(dir))
}

// local was host's other name, on Tart, where a person asking for local
// means the local provider, which dockhand never chooses itself: it's
// refused as a default and as a profile's name (D17, 2026-10-29).
func TestLocalIsNoBuildProfile(t *testing.T) {
	for _, text := range []string{"[build]\ndefault = \"local\"\n", "[build]\nunattended = \"local\"\n"} {
		_, err := parse("config.toml", text)
		require.ErrorContains(t, err, `"local" is no build profile: "host" builds on Tart on this Mac's release, and --on local, asked each time, in a MacPorts prefix on this Mac`)
	}
	_, err := parse("config.toml", "[build.profiles.local]\nprovider = \"tart\"\n")
	require.ErrorContains(t, err, "build.profiles.local: local names the local provider, which --on local builds with; name yours another way")
}
