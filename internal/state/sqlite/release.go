package sqlite

import (
	"encoding/hex"
	"net/url"

	"github.com/herbygillot/dockhand/internal/record"
)

func validReleaseSource(r record.Release) bool {
	if !r.Archive {
		return r.Listing == nil && r.Forge != "" && r.Instance != "" && r.Repository != "" && r.Tag != "" && objectID(record.ObjectID(r.Commit))
	}
	if r.CurrentVersion == "" || r.Forge != "" || r.Instance != "" || r.Repository != "" || r.Tag != "" || r.Commit != "" {
		return false
	}
	if r.Requested != "" {
		return r.Requested == r.Version && r.Listing == nil
	}
	if r.Listing == nil {
		return false
	}
	u, err := url.Parse(r.Listing.URL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	digest, err := hex.DecodeString(r.Listing.SHA256)
	return err == nil && len(digest) == 32
}
