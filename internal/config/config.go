// Package config reads and writes dockhand's configuration file,
// ~/.dockhand/config.toml (decision 15; docs/design-v3.md §12).
//
// A setting's value comes from a flag, then the environment, then this
// file, then its default. The file holds only keys dockhand uses: an
// unknown key or a malformed value is refused by name, never ignored.
// Reading the file creates nothing.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
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
	Submit     Submit `toml:"submit"`
	Serve      Serve  `toml:"serve"`
	Providers  struct {
		Command *CommandProvider `toml:"command"`
		GitHub  GitHubProvider   `toml:"github"`
		Tart    TartProvider     `toml:"tart"`
	} `toml:"providers"`
	Cleanup Cleanup `toml:"cleanup"`
}

// Submit holds submit's defaults.
type Submit struct {
	// RerequestReview is what submit does after pushing to a pull request
	// whose reviewers requested changes: ask (the default), always, or
	// never ask them to review again.
	RerequestReview string `toml:"rerequest_review"`
}

// Serve is what serve does besides running checks (Design v3 §11).
type Serve struct {
	// ForOutdated is what serve does with your outdated ports each day:
	// list (the default) counts them for status, draft prepares a branch
	// for each, and check also checks each.
	ForOutdated string `toml:"for_outdated"`
	// OutdatedAt is when, each day, serve looks for new releases, as
	// "07:00" in local time; 07:00 when unset.
	OutdatedAt string `toml:"outdated_at"`
	// SubmitPassing opens pull requests for the updates serve prepared
	// that pass, within §11's guardrails; serve --submit-passing and
	// --no-submit-passing override it for one run.
	SubmitPassing bool `toml:"submit_passing"`
	// SubmitLimit is the most pull requests serve opens a day; 10 when
	// unset.
	SubmitLimit int `toml:"submit_limit"`
	// Notify posts macOS notifications when a check finishes or a pull
	// request changes; on when unset.
	Notify *bool `toml:"notify"`
}

// Mode is for_outdated, list when unset.
func (s Serve) Mode() string {
	if s.ForOutdated == "" {
		return "list"
	}
	return s.ForOutdated
}

// Time is outdated_at as hours and minutes.
func (s Serve) Time() (hour, minute int) {
	hour, minute = 7, 0
	if s.OutdatedAt != "" {
		// Load checked the form, so there is nothing to report here.
		_, _ = fmt.Sscanf(s.OutdatedAt, "%d:%d", &hour, &minute)
	}
	return hour, minute
}

// Limit is submit_limit, 10 when unset.
func (s Serve) Limit() int {
	if s.SubmitLimit <= 0 {
		return 10
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
	// as "7d" or "36h"; 7 days when unset.
	After string `toml:"after"`
	// MinFree is the free space, such as "30GB", below which cleanup runs
	// at once rather than waiting for its day; 30 GB when unset.
	MinFree string `toml:"min_free"`
}

// DefaultMinFree is the free space cleanup keeps when min_free is unset.
const DefaultMinFree = 30 << 30

// Free is min_free in bytes.
func (c Cleanup) Free() uint64 {
	free, err := parseSize(c.MinFree)
	if err != nil || c.MinFree == "" {
		return DefaultMinFree
	}
	return free
}

// parseSize reads a size in gigabytes or terabytes, such as "30GB", "30G",
// or "1TB".
func parseSize(value string) (uint64, error) {
	number := strings.TrimSpace(strings.ToUpper(value))
	unit := uint64(1 << 30)
	switch {
	case strings.HasSuffix(number, "TB"), strings.HasSuffix(number, "T"):
		unit = 1 << 40
	case strings.HasSuffix(number, "GB"), strings.HasSuffix(number, "G"):
	default:
		return 0, fmt.Errorf("%q is not a size such as 30GB", value)
	}
	number = strings.TrimSpace(strings.TrimRight(number, "TGB"))
	n, err := strconv.ParseUint(number, 10, 64)
	// A size past what 64 bits count of bytes wrapped around to a small
	// one, which cleanup took as min_free (the limits sweep, 2026-10-01).
	if err != nil || n == 0 || n > math.MaxUint64/unit {
		return 0, fmt.Errorf("%q is not a size such as 30GB", value)
	}
	return n * unit, nil
}

// DefaultCleanupAfter is how long cleanup waits when after is unset.
const DefaultCleanupAfter = 7 * 24 * time.Hour

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
	// On are the providers every check must pass on, such as
	// "tart:tahoe" or "command"; all of them, never one of them.
	On []string `toml:"on"`
	// Tests is declared, required, or skip.
	Tests string `toml:"tests"`
	// Baseline runs a baseline of what failed after a check fails.
	Baseline bool `toml:"baseline"`
}

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
		return 2
	case buildenv.Tart:
		if f.Providers.Tart.Capacity > 0 {
			return f.Providers.Tart.Capacity
		}
	}
	return 1
}

// Path is the configuration file to use: $DOCKHAND_CONFIG, or
// ~/.dockhand/config.toml.
func Path() (string, error) {
	if path := os.Getenv(PathVariable); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dockhand", "config.toml"), nil
}

// Load reads the file at path. A missing file is an empty configuration.
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
	if f.Worktrees, err = expandHome(f.Worktrees); err != nil {
		return File{}, fmt.Errorf("%s: worktrees: %w", path, err)
	}
	switch f.Check.Tests {
	case "", "declared", "required", "skip":
	default:
		return File{}, fmt.Errorf("%s: check.tests: %q is not declared, required, or skip", path, f.Check.Tests)
	}
	switch f.Serve.ForOutdated {
	case "", "list", "draft", "check":
	default:
		return File{}, fmt.Errorf("%s: serve.for_outdated: %q is not list, draft, or check", path, f.Serve.ForOutdated)
	}
	if f.Serve.OutdatedAt != "" && !clockTime.MatchString(f.Serve.OutdatedAt) {
		return File{}, fmt.Errorf("%s: serve.outdated_at: %q is not a time of day, such as \"07:00\"", path, f.Serve.OutdatedAt)
	}
	if f.Serve.SubmitLimit < 0 {
		return File{}, fmt.Errorf("%s: serve.submit_limit: %d is not a number of pull requests", path, f.Serve.SubmitLimit)
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

func expandHome(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%q is not an absolute path", path)
	}
	return filepath.Clean(path), nil
}

var worktreesLine = regexp.MustCompile(`(?m)^[ \t]*worktrees[ \t]*=.*$`)

// SetWorktrees records the worktrees directory in the file at path,
// creating it when absent and leaving every other line as it was.
func SetWorktrees(path, directory string) error {
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("worktrees: %q is not an absolute path", directory)
	}
	line := "worktrees = " + strconv.Quote(directory)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	// A top-level key must come before the first table, so an existing line
	// counts only there; otherwise the key goes at the top.
	top := text
	if i := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text); i != nil {
		top = text[:i[0]]
	}
	if loc := worktreesLine.FindStringIndex(top); loc != nil {
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
