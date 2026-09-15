# Concise publication environment details

Removed the separate pre-verification cleanliness sentence. The recorded image name now receives `(pristine)` when the selected attempt records both no active MacPorts ports and no detected foreign package-manager prefixes. Missing or partial evidence does not receive that label.

The checklist footer now reads only: “Unchecked manual items require contributor review.” Existing command rendering is unchanged.

Xcode version/build details were already included for Xcode-selected images. Guest diagnostics now also retain the installed Command Line Tools package version independently, allowing those PR bodies to include both toolchain observations alongside macOS. Older evidence keeps its recorded selected-toolchain details without inventing missing CLT data. The added optional JSON field requires no database migration.

Updated existing publication and provider-evidence tests for the wording, positive/partial cleanliness evidence, and CLT retention. No existing remote PR was edited.

Validation: `go test ./internal/publish ./internal/record ./internal/verify/tart` and `git diff --check` passed.
