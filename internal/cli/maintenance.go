package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) databaseCommand() *cobra.Command {
	command := &cobra.Command{Use: "db", Short: "Back up, check, and migrate the shared state database", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	backup := &cobra.Command{
		Use: "backup <file>", Short: "Write a consistent standalone database snapshot",
		Long: "Back up every repository in the selected database, including committed WAL contents. The destination must not exist. Git objects, logs, VMs, and remote state are not included. No repository or provider setup is required.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.BackupDatabase(cmd.Context(), r.config, args[0])
			if err != nil {
				return err
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Database backup: %s (%d bytes)\n", result.Path, result.Bytes)
			return err
		},
	}
	check := &cobra.Command{
		Use: "check", Short: "Check database integrity without advancing work", Args: cobra.NoArgs,
		Long: "Check SQLite integrity and foreign-key references in the selected database. This does not verify external Git objects, logs, VMs, or pull requests, and it does not repair or resume work.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.CheckDatabase(cmd.Context(), r.config); err != nil {
				return err
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct{ Valid bool }{true})
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Database integrity: ok")
			return err
		},
	}
	migrate := &cobra.Command{
		Use: "migrate", Short: "Upgrade an existing Dockhand database without advancing work", Args: cobra.NoArgs,
		Long: "Apply supported schema migrations transactionally to the selected database for all repositories. A current schema needs no upgrade. Missing, unrecognized, and newer databases are refused. This requires no checkout or provider and does not run driver cycles. Use db backup beforehand if you want a snapshot of the old schema.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.MigrateDatabase(cmd.Context(), r.config); err != nil {
				return err
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct{ Current bool }{true})
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Database schema is current.")
			return err
		},
	}
	command.AddCommand(backup, check, migrate)
	return command
}

func (r *runtime) gcCommand() *cobra.Command {
	options := workflow.RetentionOptions{}
	command := &cobra.Command{
		Use: "gc", Short: "Release old environments and prune old diagnostics and caches", Args: cobra.NoArgs,
		Long: "Clean up terminal work in the current repository. Release retained environments whose jobs finished before the age threshold; prune diagnostic files only when confirmed release is also that old. Future explicit retention deadlines are honored. History, evidence, submission identities, and lockfiles remain. Active or unresolved attempts are never cleaned up. Old local GitHub log caches are removed only for terminal jobs; remote logs and database evidence remain. Shared PortIndex caches are removed by last-use age, across repositories, under their existing locks. Busy caches are skipped. This command does not advance jobs.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, callErr := app.Collect(cmd.Context(), r.config, options)
			if r.json {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
					return err
				}
			} else {
				prefix := ""
				if options.DryRun {
					prefix = "Would "
				}
				for _, item := range result.Items {
					status := "pending"
					if item.Completed {
						status = "completed"
					}
					if options.DryRun {
						status = "preview"
					}
					target := string(item.ResourceID)
					if item.AttemptID != "" {
						target = string(item.AttemptID)
					}
					if item.Path != "" {
						target = item.Path
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s%s %s: %s\n", prefix, item.Action, target, status); err != nil {
						return err
					}
					if item.Detail != "" {
						if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", item.Detail); err != nil {
							return err
						}
					}
				}
				if len(result.Items) == 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No eligible cleanup."); err != nil {
						return err
					}
				}
			}
			if callErr != nil {
				return databaseReadError(callErr)
			}
			if !options.DryRun {
				for _, item := range result.Items {
					if !item.Completed {
						return fmt.Errorf("cleanup remains pending; rerun gc or inspect status")
					}
				}
			}
			return nil
		},
	}
	command.Flags().DurationVar(&options.OlderThan, "older-than", 7*24*time.Hour, "Minimum age since job completion, resource release, or cache use (0 includes recent work)")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show eligible cleanup without changing files/state or contacting remote services")
	return command
}

func databaseReadError(err error) error {
	var migration *state.MigrationRequiredError
	if !errors.As(err, &migration) {
		return err
	}
	return fmt.Errorf("%w; this command is read-only. Run dockhand db migrate with the same --db option, then retry. To save the old schema first, use dockhand db backup <file> with the same --db option", err)
}
