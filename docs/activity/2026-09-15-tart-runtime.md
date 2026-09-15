# Tart runtime resolution

Provisioning and verification now use one `tart.Client.Resolve` implementation for executable/home defaults and canonical home identity. Existing symlink ancestors are resolved even before a new home exists. This prevents two spellings of the same Tart home from selecting different verification pools or image coordination paths. Broken symlinks and non-directory ancestors fail explicitly.

Verification config inspection no longer creates the artifact directory. Request operations initialize it when opening their coordination boundary. Canonical artifact identity remains stable before and after creation.

Authored the shared resolver and regression tests; adapted existing v2 consumers without bringing in v1 code. Tests cover defaults, aliases, missing paths, invalid ancestors, and BuildConfig leaving runtime directories absent.

Validation: `go test ./internal/tart/... ./internal/verify/tart` passed.
