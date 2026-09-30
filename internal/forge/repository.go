package forge

import (
	"context"
	"errors"
	"io"
	"net/http"
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

// ErrAppRestricted is an organization refusing the app a client acts as,
// one it hasn't approved, as GitHub's OAuth App access restrictions do.
// Another app it has approved may do what this one can't.
var ErrAppRestricted = errors.New("forge: the organization restricts which apps may act for it")
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

// Documents is a forge client that fetches a livecheck document from the
// forge it serves through its own client, so the user's credentials and the
// forge's rate-limit handling apply, sending the given request headers in
// place of the client's own. served is false for a URL the forge does not
// serve, and the plain fetch is used instead.
type Documents interface {
	Document(ctx context.Context, url string, headers http.Header) (body []byte, served bool, err error)
}

// FileRepository reads one file of the repository at a commit, for
// manifests such as go.mod that a git-fetched port never downloads. An
// absent file reports ErrNotFound; the read is bounded by limit bytes.
type FileRepository interface {
	File(ctx context.Context, commit, path string, limit int64) ([]byte, error)
}

// ArchiveRepository writes the forge's archive of the repository's files
// at a commit, a gzipped tarball, bounded by limit bytes: for comparing a
// Git-fetched port's source from one commit to another, which a Git fetch
// downloads no archive of (the assessment design, D). It's the commit's
// files as the forge archives them, submodules left out, never what a
// build's clone checked out.
type ArchiveRepository interface {
	Archive(ctx context.Context, commit string, into io.Writer, limit int64) error
}

// DatedRepository reports when a commit was made, its committer's date, so
// discovery can tell an old tag from a new one however it is spelled.
type DatedRepository interface {
	CommitTime(ctx context.Context, commit string) (time.Time, error)
}

// Description is what a forge says about a repository: its one-line
// description, its homepage, and the license it detected, as an SPDX
// identifier.
type Description struct {
	Description string
	Homepage    string
	License     string
}

// DescribedRepository reports a repository's description.
type DescribedRepository interface {
	Describe(context.Context) (Description, error)
}
