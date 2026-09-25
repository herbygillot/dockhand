package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
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
			options, file, path, err := s.options()
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
			add("worktrees", tildeOrEmpty(file.Worktrees), "~/src/macports-branches, beside the clone")
			add("maintainer", file.Maintainer, "(none; create asks for it)")
			add("check.on", strings.Join(file.Check.On, ", "), "command, when [providers.command] is set up")
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
			add("cleanup.after", file.Cleanup.After, "7d")
			if command := file.Providers.Command; command != nil {
				add("providers.command.run", command.Run, "")
				add("providers.command.name", command.Name, "command")
			} else {
				add("providers.command", "", "(not set up)")
			}
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

func tildeOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	return tilde(path)
}
