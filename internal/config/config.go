// Package config reads and writes dockhand's configuration file,
// ~/.dockhand/config.toml (decision 15; docs/design-v3.md §12).
//
// A setting's value comes from a flag, then the environment, then this
// file, then its default. The file holds only keys dockhand uses: an
// unknown key or a malformed value is refused by name, never ignored.
// Reading the file creates nothing.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	// buildenv for its constants alone: the providers' names, which
	// a profile and capacity name, and their timeout defaults.
	"github.com/herbygillot/dockhand/internal/buildenv"
	// macports for the maintainers line's rules, which maintainer is
	// checked against as it's read.
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/prose"
)

// PathVariable names another configuration file, for tests and experiments.
const PathVariable = "DOCKHAND_CONFIG"

// File is the configuration file's contents.
type File struct {
	// Worktrees is the directory managed branches' worktrees live in, with
	// a leading ~ expanded.
	Worktrees string `toml:"worktrees"`
	// Maintainer is you as a Portfile's maintainers line names you, such
	// as "{@ada example.org:ada} openmaintainer".
	Maintainer string `toml:"maintainer"`
	Check      Check  `toml:"check"`
	Build      Build  `toml:"build"`
	Submit     Submit `toml:"submit"`
	Serve      Serve  `toml:"serve"`
	Providers  struct {
		Command *CommandProvider `toml:"command"`
		GitHub  GitHubProvider   `toml:"github"`
		Tart    TartProvider     `toml:"tart"`
	} `toml:"providers"`
	Cleanup Cleanup `toml:"cleanup"`
	Holds   Holds   `toml:"holds"`
}

// Holds are what holds an update nobody looks over, bump's and serve's, for
// a person's look, beside what its comparison finds: the hold list that
// ship's holds join.
type Holds struct {
	// Majors is what such an update does where its major version moves:
	// hold (the default) or allow (the person's preference since
	// 2026-10-06; hyperfine went from 1.21.0 to 2.0.0 unlooked at).
	Majors string `toml:"majors"`
	// Allow are the rules whose concerns such an update is said to have,
	// and goes on past: a rule of what comparing its archives found, such
	// as build-file-changed, a commit rule's error, or one of the holds
	// dockhand raises, such as open, for another open pull request
	// (engine.HoldRules, which the command line checks it against).
	Allow []string `toml:"allow"`
}

// HoldMajors reports whether an unattended update holds where its major
// version moves: unless majors is allow.
func (h Holds) HoldMajors() bool { return h.Majors != "allow" }

// Submit holds submit's defaults.
type Submit struct {
	// RerequestReview is what submit does after pushing to a pull request
	// whose reviewers requested changes: ask (the default), always, or
	// never ask them to review again.
	RerequestReview string `toml:"rerequest_review"`
}

// Serve is what serve does besides running checks (Design v3 §11).
type Serve struct {
	// ShipTo is how far serve carries your outdated ports each day, as
	// ship --to does: list (the default) counts them for status, commit
	// prepares a branch with one commit for each, check also checks each,
	// and submit opens a pull request for each that passes, within §11's
	// guardrails (the command-line UX second look's §6).
	ShipTo string `toml:"ship_to"`
	// OutdatedAt is when, each day, serve looks for new releases, as
	// "07:00" in local time; 07:00 when unset.
	OutdatedAt string `toml:"outdated_at"`
	// SubmitLimit is the most pull requests serve opens a day; 10 when
	// unset.
	SubmitLimit int `toml:"submit_limit"`
	// Notify posts macOS notifications when a check finishes or a pull
	// request changes; on when unset.
	Notify *bool `toml:"notify"`
}

// Stage is ship_to, list when unset.
func (s Serve) Stage() string {
	return cmp.Or(s.ShipTo, DefaultShipTo)
}

// Time is outdated_at as hours and minutes.
func (s Serve) Time() (hour, minute int) {
	// Load checked the form, so there is nothing to report here.
	_, _ = fmt.Sscanf(cmp.Or(s.OutdatedAt, DefaultOutdatedAt), "%d:%d", &hour, &minute)
	return hour, minute
}

// Limit is submit_limit, 10 when unset.
func (s Serve) Limit() int {
	if s.SubmitLimit <= 0 {
		return DefaultSubmitLimit
	}
	return s.SubmitLimit
}

// Notifies reports whether serve posts notifications.
func (s Serve) Notifies() bool { return s.Notify == nil || *s.Notify }

var clockTime = regexp.MustCompile(`^([01]?[0-9]|2[0-3]):[0-5][0-9]$`)

// Cleanup is decision 36's automatic cleanup, which serve runs at most
// once a day.
type Cleanup struct {
	// Automatic turns it off when false; it is on when unset.
	Automatic *bool `toml:"automatic"`
	// After is how long something goes unused before it is removed, such
	// as "7d" or "36h"; 15 days when unset.
	After string `toml:"after"`
	// MinFree is the free space, such as "30GB", below which cleanup runs
	// at once rather than waiting for its day; 30 GB when unset.
	MinFree string `toml:"min_free"`
}

// DefaultMinFree is the free space cleanup keeps when min_free is unset:
// 30 GB, in SI units as Finder and df -H count them, like every size
// dockhand says (the library survey, the person's choice of 2026-10-02).
const DefaultMinFree = 30_000_000_000

// Free is min_free in bytes.
func (c Cleanup) Free() uint64 {
	free, err := parseSize(c.MinFree)
	if err != nil || c.MinFree == "" {
		return DefaultMinFree
	}
	return free
}

// sizeUnits are the units a size may name: SI, as dockhand says sizes,
// with G and T for GB and TB, and binary ones by their own names.
var sizeUnits = map[string]uint64{
	"MB": 1e6, "GB": 1e9, "TB": 1e12, "G": 1e9, "T": 1e12,
	"MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40,
}

// parseSize reads a size such as "30GB", "1.5TB", "500MB", or "64GiB".
func parseSize(value string) (uint64, error) {
	refused := fmt.Errorf("%q is not a size such as 30GB", value)
	text := strings.ToUpper(strings.TrimSpace(value))
	cut := strings.LastIndexAny(text, "0123456789.") + 1
	unit, ok := sizeUnits[strings.TrimSpace(text[cut:])]
	if !ok {
		return 0, refused
	}
	number, ok := new(big.Rat).SetString(strings.TrimSpace(text[:cut]))
	if !ok || number.Sign() <= 0 {
		return 0, refused
	}
	bytes := new(big.Int).Mul(number.Num(), new(big.Int).SetUint64(unit))
	bytes.Quo(bytes, number.Denom())
	// A size past what 64 bits count of bytes wrapped around to a small
	// one, which cleanup took as min_free (the limits sweep, 2026-10-01).
	if bytes.Sign() <= 0 || !bytes.IsUint64() {
		return 0, refused
	}
	return bytes.Uint64(), nil
}

// DefaultCleanupAfter is how long cleanup waits when after is unset: for
// caches, kept archives, events, logs, and what an ended branch's checks
// recorded alike, one setting the person chose at 15 days (D6).
const DefaultCleanupAfter = 15 * 24 * time.Hour

// On reports whether automatic cleanup runs.
func (c Cleanup) On() bool { return c.Automatic == nil || *c.Automatic }

// Age is after as a duration.
func (c Cleanup) Age() time.Duration {
	age, err := parseAge(c.After)
	if err != nil || c.After == "" {
		return DefaultCleanupAfter
	}
	return age
}

// parseAge reads a duration that may count days, such as "7d".
func parseAge(value string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.Atoi(days)
		// Days past what a duration holds, about 292 years, wrapped around
		// to a negative age, and cleanup would prune everything.
		if err != nil || n <= 0 || n > int(math.MaxInt64/(24*time.Hour)) {
			return 0, fmt.Errorf("%q is not a number of days, such as 7d", value)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	age, err := time.ParseDuration(value)
	if err != nil || age <= 0 {
		return 0, fmt.Errorf("%q is not a duration, such as 7d or 36h", value)
	}
	return age, nil
}

// Check holds check's defaults.
type Check struct {
	// Tests is declared, required, or skip.
	Tests string `toml:"tests"`
	// Baseline runs a baseline of what failed after a check fails.
	Baseline bool `toml:"baseline"`
}

// Build is where checks build: the profile a person's check takes, the one
// a check nobody looks over takes, and the person's own profiles (the
// person's design, 2026-10-13).
type Build struct {
	// Default is the profile a check takes where neither --build nor --on
	// names where; host when unset, or command where [providers.command]
	// is set up.
	Default string `toml:"default"`
	// Unattended is the profile of a check nobody looks over before it's
	// submitted, as serve's and bump's; minmax when unset, where Tart is
	// set up, and else Default's.
	Unattended string `toml:"unattended"`
	// Profiles are the person's own, by name.
	Profiles map[string]Profile `toml:"profiles"`
}

// Profile is a provider and the releases it builds on: a list, as ["15",
// "26"], or a rule's name, host, minmax, macports-ci, or all.
type Profile struct {
	// Provider is tart, github, or command; tart when unset.
	Provider string `toml:"provider"`
	// Releases are a list or a rule, as Load checked.
	Releases any `toml:"releases"`
}

// The rules a profile's releases may name: this Mac's release; each port's
// oldest and newest that dockhand runs; the releases MacPorts' CI builds
// on, at master; and every release Tart runs here. The one vocabulary,
// which config checks a file against and the engine applies (the
// architecture review of 5897a82, R3-16). Each of dockhand's own profiles
// is named for its rule.
type ReleaseRule string

const (
	RuleHost       ReleaseRule = "host"
	RuleMinMax     ReleaseRule = "minmax"
	RuleMacPortsCI ReleaseRule = "macports-ci"
	RuleAll        ReleaseRule = "all"
)

// ProfileLocal is host's profile by its other name.
const ProfileLocal = "local"

// releaseRules are the rules a profile's releases may name.
var releaseRules = []ReleaseRule{RuleHost, RuleMinMax, RuleMacPortsCI, RuleAll}

// builtinProfiles are dockhand's own profiles, all on Tart, each named for
// its rule, local being host's other name.
var builtinProfiles = []string{string(RuleHost), ProfileLocal, string(RuleMinMax), string(RuleMacPortsCI), string(RuleAll)}

// List is a profile's releases as a list, and its rule where it names one.
func (p Profile) List() (releases []string, rule ReleaseRule) {
	switch value := p.Releases.(type) {
	case string:
		return nil, ReleaseRule(value)
	case []any:
		for _, release := range value {
			if name, ok := release.(string); ok {
				releases = append(releases, name)
			}
		}
	}
	return releases, ""
}

// The profiles build.default and build.unattended take when unset.
const (
	DefaultBuildProfile      = "host"
	DefaultUnattendedProfile = "minmax"
)

// CommandProvider is a person's own build script (Design v3 §7): it is
// given a request file and writes a result file.
type CommandProvider struct {
	// Run is the command, given the request file's path as its argument.
	Run string `toml:"run"`
	// Name labels its results: "reported by <name>".
	Name string `toml:"name"`
	// Capacity is how many checks serve runs on it at once; 1 when unset.
	Capacity int `toml:"capacity"`
}

// GitHubProvider builds with MacPorts' own workflow in your fork.
type GitHubProvider struct {
	// Remote is the Git remote that pushes to your fork, when more than
	// one pushes to a fork you own.
	Remote string `toml:"remote"`
	// Capacity is how many checks serve runs on it at once; 2 when unset.
	// Each run takes one runner per macOS release MacPorts' workflow
	// builds on, and GitHub queues what a plan's limits won't start.
	Capacity int `toml:"capacity"`
	// BuildTimeout bounds how long a run builds before dockhand cancels
	// it, as a duration such as "4h"; 6 hours when unset, GitHub's own cap
	// on a job, which a longer one doesn't lift (D16).
	BuildTimeout string `toml:"build_timeout"`
}

// BuildBound is the build timeout the configuration sets, or zero.
func (g GitHubProvider) BuildBound() time.Duration {
	timeout, _ := time.ParseDuration(g.BuildTimeout)
	return timeout
}

// xcodeVersion is an Xcode version as Apple numbers it: 26.6, 14.0.1, 27.
var xcodeVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// TartProvider builds in dockhand's Tart images, one fresh clone per
// release and attempt.
type TartProvider struct {
	// Capacity is how many checks serve runs on it at once; 1 when unset,
	// since macOS runs two VMs at most, the person's own among them.
	Capacity int `toml:"capacity"`
	// TestTimeout bounds a target's tests, as a duration such as "45m";
	// 30 minutes when unset.
	TestTimeout string `toml:"test_timeout"`
	// BuildTimeout bounds a target's build, its dependencies' installs,
	// fetch, and its own install, as a duration such as "8h"; 6 hours,
	// GitHub's cap on a job, when unset (D16).
	BuildTimeout string `toml:"build_timeout"`
	// Xcode is the Xcode each release's Xcode image installs, by release
	// name or number: tahoe = "26.6". A release it doesn't name gets what
	// MacPorts' arm64 builder for the release runs.
	Xcode map[string]string `toml:"xcode"`
}

// Timeout is the test timeout the configuration sets, or zero.
func (t TartProvider) Timeout() time.Duration {
	timeout, _ := time.ParseDuration(t.TestTimeout)
	return timeout
}

// BuildBound is the build timeout the configuration sets, or zero.
func (t TartProvider) BuildBound() time.Duration {
	timeout, _ := time.ParseDuration(t.BuildTimeout)
	return timeout
}

// Capacity is how many checks serve runs on a provider at once.
func (f File) Capacity(provider string) int {
	switch provider {
	case buildenv.Command:
		if f.Providers.Command != nil && f.Providers.Command.Capacity > 0 {
			return f.Providers.Command.Capacity
		}
	case buildenv.GitHub:
		if f.Providers.GitHub.Capacity > 0 {
			return f.Providers.GitHub.Capacity
		}
		return DefaultGitHubCapacity
	case buildenv.Tart:
		if f.Providers.Tart.Capacity > 0 {
			return f.Providers.Tart.Capacity
		}
	}
	return DefaultCapacity
}

// The defaults config's accessors fall back on, where neither a flag, the
// environment, nor the file sets a value.
const (
	DefaultShipTo      = "list"
	DefaultOutdatedAt  = "07:00"
	DefaultSubmitLimit = 10
	// DefaultCapacity is a provider's, Tart's and a command's, apart from
	// GitHub's: macOS runs two VMs at most, the person's own among them.
	DefaultCapacity       = 1
	DefaultGitHubCapacity = 2
)

// Defaults are every setting's default as dockhand config shows it, keyed
// as the file names it: one table, so a default changed in the code is
// the one config says, and build profiles and ship add a row rather than
// a literal (the command package review's config.Defaults). Where the
// default is a rule rather than a value, the row says the rule. Each
// release's Xcode, providers.tart.xcode.<release>, is MacPorts' arm64
// buildbot's for it, which config reads from the facts table instead.
func Defaults() map[string]string {
	return map[string]string{
		"worktrees":                      "~/Source/macports-branches",
		"maintainer":                     "(none; create writes nomaintainer)",
		"build.default":                  DefaultBuildProfile + " (command where [providers.command] is set up)",
		"build.unattended":               DefaultUnattendedProfile + " (build.default's where Tart isn't installed)",
		"check.tests":                    "declared",
		"check.baseline":                 "false",
		"submit.rerequest_review":        "ask",
		"cleanup.automatic":              "true",
		"cleanup.after":                  ageWords(DefaultCleanupAfter),
		"cleanup.min_free":               prose.Bytes(DefaultMinFree),
		"holds.majors":                   "hold",
		"holds.allow":                    "(none)",
		"serve.ship_to":                  DefaultShipTo,
		"serve.outdated_at":              DefaultOutdatedAt,
		"serve.submit_limit":             strconv.Itoa(DefaultSubmitLimit),
		"serve.notify":                   "true",
		"providers.tart.capacity":        strconv.Itoa(DefaultCapacity),
		"providers.tart.test_timeout":    ageWords(buildenv.DefaultTestTimeout),
		"providers.tart.build_timeout":   ageWords(buildenv.DefaultBuildTimeout),
		"providers.command":              "(not set up)",
		"providers.command.run":          "",
		"providers.command.name":         "command",
		"providers.command.capacity":     strconv.Itoa(DefaultCapacity),
		"providers.github.remote":        "the one remote pushing to your fork",
		"providers.github.capacity":      strconv.Itoa(DefaultGitHubCapacity),
		"providers.github.build_timeout": ageWords(buildenv.DefaultBuildTimeout),
	}
}

// ageWords is a duration as the file writes one: 15d, 6h, 30m.
func ageWords(age time.Duration) string {
	switch {
	case age%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", age/(24*time.Hour))
	case age%time.Hour == 0:
		return fmt.Sprintf("%dh", age/time.Hour)
	}
	return fmt.Sprintf("%dm", age/time.Minute)
}

// Places are a command line's home and working directory, which the
// paths a person gives are read against and shown in.
type Places struct {
	Home, Dir string
}

// errNoHome is a command line without a home directory.
var errNoHome = errors.New("$HOME is not defined")

// ConfigPath is the configuration file to use: named, $DOCKHAND_CONFIG's
// value, where it's set, else ~/.dockhand/config.toml.
func (p Places) ConfigPath(named string) (string, error) {
	if named != "" {
		return p.Absolute(named)
	}
	if p.Home == "" {
		return "", errNoHome
	}
	return filepath.Join(p.Home, ".dockhand", "config.toml"), nil
}

// Load reads the file at path, its worktrees' leading ~ expanded.
func (p Places) Load(path string) (File, error) {
	f, err := Load(path)
	if err != nil {
		return f, err
	}
	if f.Worktrees, err = p.withHome(f.Worktrees); err != nil {
		return File{}, fmt.Errorf("%s: worktrees: %w", path, err)
	}
	return f, nil
}

// Load reads the file at path. A missing file is an empty configuration.
// A worktrees directory under ~ is kept as written (Places.Load).
func Load(path string) (File, error) {
	var f File
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	return parse(path, string(data))
}

func parse(path, text string) (File, error) {
	var f File
	meta, err := toml.Decode(text, &f)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		// check.on went with build profiles, with no alias (the person,
		// 2026-10-13): said with what took its place.
		for _, key := range unknown {
			switch key.String() {
			case "check.on":
				return File{}, fmt.Errorf("%s: check.on is gone: build.default names a build profile, as \"host\" or \"macports-ci\", or one of your own under [build.profiles]", path)
			case "serve.for_outdated", "serve.submit_passing":
				// As did these with ship (the second look's §6).
				return File{}, fmt.Errorf("%s: %s is gone: serve.ship_to says how far serve carries your outdated ports, list, commit, check, or submit; for_outdated = \"draft\" is ship_to = \"commit\", and submit_passing = true is ship_to = \"submit\"", path, key)
			}
		}
		// A table is reported beside the keys in it; name only the keys.
		var keys []string
		for i, key := range unknown {
			name := key.String()
			if i+1 < len(unknown) && strings.HasPrefix(unknown[i+1].String(), name+".") {
				continue
			}
			keys = append(keys, name)
		}
		return File{}, fmt.Errorf("%s: unknown setting %s", path, strings.Join(keys, ", "))
	}
	if err := absoluteOrHome(f.Worktrees); err != nil {
		return File{}, fmt.Errorf("%s: worktrees: %w", path, err)
	}
	switch f.Check.Tests {
	case "", "declared", "required", "skip":
	default:
		return File{}, fmt.Errorf("%s: check.tests: %q is not declared, required, or skip", path, f.Check.Tests)
	}
	if err := checkBuild(f); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	switch f.Serve.ShipTo {
	case "", "list", "commit", "check", "submit":
	default:
		return File{}, fmt.Errorf("%s: serve.ship_to: %q is not list, commit, check, or submit", path, f.Serve.ShipTo)
	}
	if f.Serve.OutdatedAt != "" && !clockTime.MatchString(f.Serve.OutdatedAt) {
		return File{}, fmt.Errorf("%s: serve.outdated_at: %q is not a time of day, such as \"07:00\"", path, f.Serve.OutdatedAt)
	}
	if f.Serve.SubmitLimit < 0 {
		return File{}, fmt.Errorf("%s: serve.submit_limit: %d is not a number of pull requests", path, f.Serve.SubmitLimit)
	}
	switch f.Holds.Majors {
	case "", "hold", "allow":
	default:
		return File{}, fmt.Errorf("%s: holds.majors: %q is not hold or allow", path, f.Holds.Majors)
	}
	switch f.Submit.RerequestReview {
	case "", "ask", "always", "never":
	default:
		return File{}, fmt.Errorf("%s: submit.rerequest_review: %q is not ask, always, or never", path, f.Submit.RerequestReview)
	}
	if f.Maintainer != "" {
		if err := macports.CheckMaintainers(f.Maintainer); err != nil {
			return File{}, fmt.Errorf("%s: maintainer: %w", path, err)
		}
	}
	if f.Cleanup.After != "" {
		if _, err := parseAge(f.Cleanup.After); err != nil {
			return File{}, fmt.Errorf("%s: cleanup.after: %w", path, err)
		}
	}
	if f.Cleanup.MinFree != "" {
		if _, err := parseSize(f.Cleanup.MinFree); err != nil {
			return File{}, fmt.Errorf("%s: cleanup.min_free: %w", path, err)
		}
	}
	if f.Providers.GitHub.Capacity < 0 {
		return File{}, fmt.Errorf("%s: providers.github.capacity: %d is not a number of checks", path, f.Providers.GitHub.Capacity)
	}
	if f.Providers.Tart.Capacity < 0 {
		return File{}, fmt.Errorf("%s: providers.tart.capacity: %d is not a number of checks", path, f.Providers.Tart.Capacity)
	}
	if timeout := f.Providers.Tart.TestTimeout; timeout != "" {
		if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
			return File{}, fmt.Errorf("%s: providers.tart.test_timeout: %q is not a duration such as \"45m\"", path, timeout)
		}
	}
	if timeout := f.Providers.GitHub.BuildTimeout; timeout != "" {
		if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
			return File{}, fmt.Errorf("%s: providers.github.build_timeout: %q is not a duration such as \"4h\"", path, timeout)
		}
	}
	if timeout := f.Providers.Tart.BuildTimeout; timeout != "" {
		if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
			return File{}, fmt.Errorf("%s: providers.tart.build_timeout: %q is not a duration such as \"8h\"", path, timeout)
		}
	}
	for release, version := range f.Providers.Tart.Xcode {
		if !xcodeVersion.MatchString(version) {
			return File{}, fmt.Errorf("%s: providers.tart.xcode.%s: %q is not an Xcode version such as \"26.6\"", path, release, version)
		}
	}
	if command := f.Providers.Command; command != nil {
		if command.Capacity < 0 {
			return File{}, fmt.Errorf("%s: providers.command.capacity: %d is not a number of checks", path, command.Capacity)
		}
		if strings.TrimSpace(command.Run) == "" {
			return File{}, fmt.Errorf("%s: providers.command.run: the command to run is required", path)
		}
		if command.Name == "" {
			command.Name = "command"
		}
	}
	return f, nil
}

// Maintainers are the spellings the maintainer line names you by, as the
// port index's maintainers field carries them: @ada, example.org:ada. The
// keywords openmaintainer and nomaintainer name no one.
func (f File) Maintainers() []string {
	maintainers, err := macports.ReadMaintainers(f.Maintainer)
	if err != nil {
		return nil
	}
	var spellings []string
	for _, maintainer := range maintainers {
		for _, spelling := range maintainer {
			if !macports.MaintainerKeyword(spelling) {
				spellings = append(spellings, spelling)
			}
		}
	}
	return spellings
}

// Tilde abbreviates the home directory in a path shown to a person: ~ and
// ~/rest. A home reached through a symbolic link is matched in its
// resolved form too, as Git reports resolved paths.
func (p Places) Tilde(path string) string {
	if p.Home == "" {
		return path
	}
	homes := []string{p.Home}
	if resolved, err := filepath.EvalSymlinks(p.Home); err == nil && resolved != p.Home {
		homes = append(homes, resolved)
	}
	for _, home := range homes {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return path
}

// Here reports whether the working directory is dir or within it, where
// naming dir tells the person nothing; symbolic links are resolved, as
// Git reports resolved paths.
func (p Places) Here(dir string) bool {
	if p.Dir == "" || dir == "" {
		return false
	}
	resolve := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return filepath.Clean(path)
	}
	rel, err := filepath.Rel(resolve(dir), resolve(p.Dir))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Absolute is a path a person gave, its leading ~ expanded, made absolute
// against the working directory.
func (p Places) Absolute(path string) (string, error) {
	path, err := p.withHome(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.Dir, path)
	}
	return filepath.Clean(path), nil
}

// withHome expands a path's leading ~.
func (p Places) withHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if p.Home == "" {
			return "", errNoHome
		}
		path = filepath.Join(p.Home, strings.TrimPrefix(path, "~"))
	}
	return path, nil
}

// absoluteOrHome refuses a path that's neither absolute nor under ~.
func absoluteOrHome(path string) error {
	if path == "" || path == "~" || strings.HasPrefix(path, "~/") || filepath.IsAbs(path) {
		return nil
	}
	return fmt.Errorf("%q is not an absolute path", path)
}

// SetWorktrees records the worktrees directory in the file at path,
// creating it when absent and leaving every other line as it was.
func SetWorktrees(path, directory string) error {
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("worktrees: %q is not an absolute path", directory)
	}
	return setTopLevel(path, "worktrees", directory)
}

// SetMaintainer records the maintainers line in the file at path, as
// setup writes the one it inferred, with the person's agreement.
func SetMaintainer(path, maintainer string) error {
	return setTopLevel(path, "maintainer", maintainer)
}

// setTopLevel sets a top-level string key in the file at path, creating
// it when absent and leaving every other line as it was. A top-level key
// must come before the first table, so an existing line counts only
// there; otherwise the key goes at the top. What it writes is parsed
// before it's kept.
func setTopLevel(path, key, value string) error {
	line := key + " = " + strconv.Quote(value)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	top := text
	if i := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text); i != nil {
		top = text[:i[0]]
	}
	if loc := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=.*$`).FindStringIndex(top); loc != nil {
		text = text[:loc[0]] + line + text[loc[1]:]
	} else {
		text = line + "\n" + text
	}
	if _, err := parse(path, text); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(text), 0o600)
}

// checkBuild refuses a profile that names no provider dockhand has, a
// release list that isn't one, a rule it doesn't know, a name a built-in
// profile has, and a default naming no profile.
func checkBuild(f File) error {
	for name, profile := range f.Build.Profiles {
		if slices.Contains(builtinProfiles, name) || name == "command" {
			return fmt.Errorf("build.profiles.%s: %s is dockhand's own profile; name yours another way", name, name)
		}
		switch profile.Provider {
		case "", buildenv.Tart, buildenv.GitHub, buildenv.Command:
		default:
			return fmt.Errorf("build.profiles.%s.provider: %q is not tart, github, or command", name, profile.Provider)
		}
		switch value := profile.Releases.(type) {
		case nil:
		case string:
			if !slices.Contains(releaseRules, ReleaseRule(value)) {
				return fmt.Errorf("build.profiles.%s.releases: %q is not a list of releases, or host, minmax, macports-ci, or all", name, value)
			}
		case []any:
			for _, release := range value {
				if _, ok := release.(string); !ok {
					return fmt.Errorf("build.profiles.%s.releases: %v is not a release, such as \"15\" or \"tahoe\"", name, release)
				}
			}
		default:
			return fmt.Errorf("build.profiles.%s.releases: a list of releases, as [\"15\", \"26\"], or a rule, as \"minmax\"", name)
		}
		if releases, rule := profile.List(); profile.Provider != "" && profile.Provider != buildenv.Tart && (len(releases) > 0 || rule != "" && rule != "macports-ci") {
			return fmt.Errorf("build.profiles.%s: the %s provider builds where it does, so its releases can only be macports-ci, for github", name, profile.Provider)
		}
	}
	for key, name := range map[string]string{"build.default": f.Build.Default, "build.unattended": f.Build.Unattended} {
		if _, defined := f.Build.Profiles[name]; name != "" && !defined && !slices.Contains(builtinProfiles, name) && name != "command" {
			return fmt.Errorf("%s: %q names no profile: host, minmax, macports-ci, all, or one under [build.profiles]", key, name)
		}
	}
	return nil
}
