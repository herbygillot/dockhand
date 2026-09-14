package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/spf13/cobra"
)

func (r *runtime) databaseCommand() *cobra.Command {
	command := &cobra.Command{Use: "db", Short: "Back up and check the shared state database", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
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
	command.AddCommand(backup, check)
	return command
}

func (r *runtime) gcCommand() *cobra.Command {
	options := workflow.RetentionOptions{}
	command := &cobra.Command{
		Use: "gc", Short: "Release old retained environments and prune released diagnostics", Args: cobra.NoArgs,
		Long: "Clean up terminal work in the current repository. Release retained environments whose jobs finished before the age threshold; prune diagnostic files only when confirmed release is also that old. Future explicit retention deadlines are honored. History, evidence, submission identities, and lockfiles remain. Active or unresolved attempts are never cleaned up. This command does not advance jobs.",
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
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s%s %s: %s\n", prefix, item.Action, item.ResourceID, status); err != nil {
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
				return callErr
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
	command.Flags().DurationVar(&options.OlderThan, "older-than", 7*24*time.Hour, "Minimum age since job completion and, for artifacts, resource release (0 includes recent work)")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show eligible cleanup without changing state or contacting providers")
	return command
}
