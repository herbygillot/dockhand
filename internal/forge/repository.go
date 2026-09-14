// Package forge defines remote repository facts shared by discovery and publication.
// Adapters observe these facts; capability packages decide what they mean for a port.
package forge

import (
	"context"
	"errors"
	"time"
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

// Repository binds observations and public URLs to one validated repository.
// Catalog methods return complete observations or an error, never partial success.
// Tag resolves an exact name to a commit; an absent ref reports ErrNotFound.
type Repository interface {
	Name() string
	// TagsPageURL and TagLivecheckURL describe the web links used by livecheck
	// regexes. They are not API endpoints or resolved download locations.
	TagsPageURL() string
	TagLivecheckURL(string) string
	Tag(context.Context, string) (Tag, error)
	Releases(context.Context) ([]Release, error)
	ListTags(context.Context) ([]Tag, error)
}
