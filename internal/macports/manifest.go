package macports

import "errors"

// ErrManifestMissing reports that a dependency manifest, a go.mod or a
// Cargo.lock, is absent where a port's source says it should be: in the
// archive, or at the release's commit for a git-fetched port. The archive
// reader and the forge reader both report it, so the editor asks one
// question of either.
var ErrManifestMissing = errors.New("dependency: manifest missing")
