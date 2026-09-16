# Registered GitHub OAuth application

Dockhand now includes the public client ID for the registered "Dockhand for MacPorts" OAuth application. Ordinary source builds can use `dockhand auth login` without build-time configuration. The command-line flag, environment variable, and linker override remain available for development and alternate registrations.

The registration uses GitHub's device flow and does not require or distribute a client secret. Expiring user access tokens remain disabled because the current credential record retains only the access token. Refresh-token persistence, rotation, and cross-process coordination are tracked before that registration option is enabled.

Validation covered the focused application and CLI authentication tests, the full Go test suite, race tests, vet, module verification, a normal build, and whitespace checks.
