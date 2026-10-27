package command

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

// removedFlags are the flags dockhand has removed, by command, each with
// what replaced it, or that nothing did: a removed flag, like a removed
// setting, says so rather than "unknown flag" (the person's word on the
// docs re-review, 2026-10-27; nothing is aliased). v0.4.0 removed these
// when ship took update's and submit's later stages, and they stay here
// for good, as the settings' words do.
var removedFlags = map[string]map[string]string{
	"update": {
		"submit":            "update --submit is gone: ship <port> updates, checks, and submits it",
		"outdated":          "update --outdated is gone: update --maintainer or --category updates the ports with newer releases, and ship --mine ships yours",
		"check":             "update --check is gone: ship <port> --to check updates and checks it",
		"on":                "update --on is gone: update edits alone; ship --on and check --on say where to build",
		"note":              "update --note is gone: update edits alone; ship --note and submit --note write your note in the pull request",
		"title":             "update --title is gone: update edits alone; ship --title and submit --title give the pull request's title",
		"skip-notification": "update --skip-notification is gone: update edits alone; ship and submit take --skip-notification",
		"tested-binaries":   "update --tested-binaries is gone: update edits alone; ship and submit take --tested-binaries",
		"tested-variants":   "update --tested-variants is gone: update edits alone; ship and submit take --tested-variants",
	},
	"submit": {
		"check":   "submit --check is gone: ship -b <branch> checks a branch and submits it once its check passes",
		"on":      "submit --on is gone: ship --on says where its check builds",
		"passing": "submit --passing is gone: ship --passing submits the branches whose checks passed",
	},
	"serve": {
		"submit-passing":    "serve --submit-passing is gone: serve --ship-to submit, or serve.ship_to = \"submit\", submits what passes",
		"no-submit-passing": "serve --no-submit-passing is gone: serve --ship-to check stops at the check",
	},
	"clean":    {"legacy": "clean --legacy is gone, and nothing replaced it: branches from before dockhand v3 aren't sorted any more"},
	"logs":     {"all": "logs --all is gone: logs --full shows the whole log"},
	"outdated": {"all": "outdated --all is gone: outdated --current lists the ports with no newer release too"},
	"diff":     {"archive": "diff --archive is gone: diff --source compares the source archives"},
}

// removedFlag says what replaced a removed flag a command was given, as
// cobra's flag error names it, "unknown flag: --submit"; the error as it
// was for any other.
func removedFlag(cmd *cobra.Command, err error) error {
	name, ok := strings.CutPrefix(err.Error(), "unknown flag: --")
	if !ok {
		return err
	}
	if words, ok := removedFlags[strings.TrimPrefix(cmd.CommandPath(), "dockhand ")][name]; ok {
		return errors.New(words)
	}
	return err
}
