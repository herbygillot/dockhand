package forge

import (
	"context"
	"errors"
	"time"
)

// Forge names as recorded on releases, destinations, and pull requests.
const (
	GitHub = "github"
	GitLab = "gitlab"
)

var ErrRejected = errors.New("forge: remote write was rejected")
var ErrAuthentication = errors.New("forge: authentication is required")

var ErrNotFound = errors.New("forge: requested object was not found")
var ErrIncomplete = errors.New("forge: incomplete repository evidence")

type Tag struct{ Name, Commit string }

type Release struct {
	Tag         string
	URL         string
	Draft       bool
	Prerelease  bool
	PublishedAt time.Time
}

// Repository binds tag observations to one validated repository.
// Catalog methods return complete observations or an error, never partial success.
// Tag resolves an exact name to a commit; an absent ref reports ErrNotFound.
type Repository interface {
	Name() string
	Tag(context.Context, string) (Tag, error)
	ListTags(context.Context) ([]Tag, error)
}

type ReleaseRepository interface {
	Releases(context.Context) ([]Release, error)
}

// FileRepository reads one file of the repository at a commit, for
// manifests such as go.mod that a git-fetched port never downloads. An
// absent file reports ErrNotFound; the read is bounded by limit bytes.
type FileRepository interface {
	File(ctx context.Context, commit, path string, limit int64) ([]byte, error)
}
