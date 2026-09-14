# Module path transition

Changed the active Go module from `github.com/herbygillot/dockhand/v2` to `github.com/herbygillot/dockhand` in preparation for replacing the prerelease repository's main branch with this implementation.

Updated every production, test, and tool import along with the Makefile linker symbol used to inject the GitHub OAuth client ID. Historical activity reports retain the module path that was true when those reports were written.

No package names, external dependencies, runtime data formats, database schemas, or command behavior changed.
