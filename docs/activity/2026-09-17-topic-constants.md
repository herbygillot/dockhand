# Topic facts live in their topic package

Several shared facts had settled in subpackages or as repeated string literals: the MacPorts Base version Dockhand installs was `installation.DefaultVersion`, the MacPorts prefix appeared as `"/opt/local"` in five places, the upstream ports repository name in two, the ports CI workflow path in the GitHub verifier, the verification provider names as two `ProviderName` constants plus literals across `app`, `cli`, and `verify`, the forge names as typed constants in `macports/source` plus literals in the GitHub adapter, and the tart guest agent release, digest, and path in `tart/provision`.

Each now lives once, in the package that owns the topic:

- `macports`: `PortsRepository`, `PortsRepositoryURL`, `PortsBranch`, `PortsWorkflowPath`, `DefaultBaseVersion`, `DefaultPrefix`, and `TclShell`.
- `verify`: `ProviderTart` and `ProviderGitHub`; the provider packages and their callers use them.
- `forge`: `GitHub` and `GitLab`; `macports/source` types its `Forge` constants from them, and the GitHub adapter compares against them instead of literals.
- `tart`: `GuestAgentRelease`, `GuestAgentDigest`, and `GuestAgentPath`, used by provisioning.

No behavior changed; every value is the same string. The import graph gained three small edges toward leaf packages (`installation` and `eval` to `macports`, `macports/source` to `forge`) and no cycles. This precedes the exported-surface audit so that audit sees one definition per fact.
