package config

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// configSetting is one line of dockhand config: a key, its value, and
// where the value came from.
type configSetting struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the settings in effect, and where each comes from",
		Long: `Shows every setting dockhand reads from its configuration file, with the
value in effect and whether it came from the file or is the default. The
file is $DOCKHAND_CONFIG, else ~/.dockhand/config.toml; an unknown key in
it is refused by name. Flags, then the environment, come before the file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, file, path, err := s.Options(cmd.Context())
			if err != nil {
				return err
			}
			var all []configSetting
			defaults := config.Defaults()
			add := func(key, value, fallback string) {
				source := "file"
				if value == "" {
					value, source = fallback, "default"
				}
				all = append(all, configSetting{Key: key, Value: value, Source: source})
			}
			set := func(key, value string) { add(key, value, defaults[key]) }
			set("worktrees", s.Places().Tilde(file.Worktrees))
			set("maintainer", file.Maintainer)
			set("build.default", file.Build.Default)
			set("build.unattended", file.Build.Unattended)
			var profiles []string
			for name := range file.Build.Profiles {
				profiles = append(profiles, name)
			}
			slices.Sort(profiles)
			for _, name := range profiles {
				profile := file.Build.Profiles[name]
				releases, rule := profile.List()
				add("build.profiles."+name, fmt.Sprintf("%s, %s", cmp.Or(profile.Provider, "tart"), cmp.Or(string(rule), strings.Join(releases, ", "), string(config.RuleHost))), "")
			}
			set("check.tests", file.Check.Tests)
			baselineValue := ""
			if file.Check.Baseline {
				baselineValue = "true"
			}
			set("check.baseline", baselineValue)
			set("submit.rerequest_review", file.Submit.RerequestReview)
			automatic := ""
			if file.Cleanup.Automatic != nil {
				automatic = fmt.Sprint(*file.Cleanup.Automatic)
			}
			set("cleanup.automatic", automatic)
			set("cleanup.after", file.Cleanup.After)
			// min_free as written, with what it reads as: config left it
			// out (the acceptance harness's row C10, 2026-10-03).
			minFree := ""
			if file.Cleanup.MinFree != "" {
				minFree = fmt.Sprintf("%s (%s)", file.Cleanup.MinFree, prose.Bytes(int64(file.Cleanup.Free())))
			}
			set("cleanup.min_free", minFree)
			set("holds.majors", file.Holds.Majors)
			set("holds.allow", strings.Join(file.Holds.Allow, ", "))
			set("serve.ship_to", file.Serve.ShipTo)
			set("serve.outdated_at", file.Serve.OutdatedAt)
			set("serve.submit_limit", positive(file.Serve.SubmitLimit))
			notify := ""
			if file.Serve.Notify != nil {
				notify = fmt.Sprint(*file.Serve.Notify)
			}
			set("serve.notify", notify)
			set("providers.tart.capacity", positive(file.Providers.Tart.Capacity))
			set("providers.tart.test_timeout", file.Providers.Tart.TestTimeout)
			set("providers.tart.build_timeout", file.Providers.Tart.BuildTimeout)
			// Each release's Xcode image installs what MacPorts' arm64
			// buildbot for it runs, unless the file names another.
			xcodes, err := tart.Xcodes(file.Providers.Tart.Xcode)
			if err != nil {
				return err
			}
			for _, xcode := range xcodes {
				configured, builder := "", "(none: MacPorts has no arm64 buildbot for it that dockhand knows)"
				if xcode.Configured {
					configured = xcode.Version
				}
				if xcode.Release.Xcode != "" {
					builder = xcode.Release.Xcode + ", as MacPorts' arm64 buildbot runs"
				}
				add("providers.tart.xcode."+xcode.Release.Slug, configured, builder)
			}
			if command := file.Providers.Command; command != nil {
				set("providers.command.run", command.Run)
				set("providers.command.name", command.Name)
				set("providers.command.capacity", positive(command.Capacity))
			} else {
				set("providers.command", "")
			}
			set("providers.github.remote", file.Providers.GitHub.Remote)
			set("providers.github.capacity", positive(file.Providers.GitHub.Capacity))
			set("providers.github.build_timeout", file.Providers.GitHub.BuildTimeout)
			streams.Emit(map[string]any{"file": path, "database": options.Database, "settings": all})
			fmt.Fprintf(streams.Out, "File      %s\nDatabase  %s\n\n", s.Places().Tilde(path), s.Places().Tilde(options.Database))
			width := 0
			for _, setting := range all {
				width = max(width, len(setting.Key))
			}
			for _, setting := range all {
				mark := ""
				if setting.Source == "default" {
					mark = "  (default)"
				}
				fmt.Fprintf(streams.Out, "%-*s  %s%s\n", width, setting.Key, setting.Value, mark)
			}
			return nil
		},
	}
	settings.SupportsJSON(cmd)
	return cmd
}

// positive is a count the file sets, or empty for its default.
func positive(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}
