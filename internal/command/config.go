package command

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/prose"
)

// configSetting is one line of dockhand config: a key, its value, and
// where the value came from.
type configSetting struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

func configCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the settings in effect, and where each comes from",
		Long: `Shows every setting dockhand reads from its configuration file, with the
value in effect and whether it came from the file or is the default. The
file is $DOCKHAND_CONFIG, else ~/.dockhand/config.toml; an unknown key in
it is refused by name. Flags, then the environment, come before the file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, file, path, err := s.options(cmd.Context())
			if err != nil {
				return err
			}
			var all []configSetting
			add := func(key, value, fallback string) {
				source := "file"
				if value == "" {
					value, source = fallback, "default"
				}
				all = append(all, configSetting{Key: key, Value: value, Source: source})
			}
			add("worktrees", tildeOrEmpty(file.Worktrees), "~/Source/macports-branches")
			add("maintainer", file.Maintainer, "(none; create asks for it)")
			add("check.on", strings.Join(file.Check.On, ", "), "command when [providers.command] is set up, else Tart on this Mac's release")
			add("check.tests", file.Check.Tests, "declared")
			baselineValue := ""
			if file.Check.Baseline {
				baselineValue = "true"
			}
			add("check.baseline", baselineValue, "false")
			add("submit.rerequest_review", file.Submit.RerequestReview, "ask")
			automatic := ""
			if file.Cleanup.Automatic != nil {
				automatic = fmt.Sprint(*file.Cleanup.Automatic)
			}
			add("cleanup.automatic", automatic, "true")
			add("cleanup.after", file.Cleanup.After, "15d")
			// min_free as written, with what it reads as: config left it
			// out (the acceptance harness's row C10, 2026-10-03).
			minFree := ""
			if file.Cleanup.MinFree != "" {
				minFree = fmt.Sprintf("%s (%s)", file.Cleanup.MinFree, prose.Bytes(int64(file.Cleanup.Free())))
			}
			add("cleanup.min_free", minFree, prose.Bytes(config.DefaultMinFree))
			add("serve.for_outdated", file.Serve.ForOutdated, "list")
			add("serve.outdated_at", file.Serve.OutdatedAt, "07:00")
			submitPassing := ""
			if file.Serve.SubmitPassing {
				submitPassing = "true"
			}
			add("serve.submit_passing", submitPassing, "false")
			add("serve.submit_limit", positive(file.Serve.SubmitLimit), "10")
			notify := ""
			if file.Serve.Notify != nil {
				notify = fmt.Sprint(*file.Serve.Notify)
			}
			add("serve.notify", notify, "true")
			add("providers.tart.capacity", positive(file.Providers.Tart.Capacity), "1")
			add("providers.tart.test_timeout", file.Providers.Tart.TestTimeout, "30m")
			add("providers.tart.build_timeout", file.Providers.Tart.BuildTimeout, "6h")
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
				add("providers.command.run", command.Run, "")
				add("providers.command.name", command.Name, "command")
				add("providers.command.capacity", positive(command.Capacity), "1")
			} else {
				add("providers.command", "", "(not set up)")
			}
			add("providers.github.remote", file.Providers.GitHub.Remote, "the one remote pushing to your fork")
			add("providers.github.capacity", positive(file.Providers.GitHub.Capacity), "2")
			add("providers.github.build_timeout", file.Providers.GitHub.BuildTimeout, "6h")
			streams.emit(map[string]any{"file": path, "database": options.Database, "settings": all})
			fmt.Fprintf(streams.Out, "File      %s\nDatabase  %s\n\n", tilde(path), tilde(options.Database))
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
}

// positive is a count the file sets, or empty for its default.
func positive(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func tildeOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	return tilde(path)
}
