package github

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/forge"
)

const webOrigin = "https://github.com"

type repository struct {
	client *Client
	name   string
}

// Repository validates a GitHub owner/name without making a network request.
func (c *Client) Repository(name string) (forge.Repository, error) {
	if c == nil || !validRepositoryName(name) {
		return nil, fmt.Errorf("github: invalid repository %q", name)
	}
	return &repository{client: c, name: name}, nil
}

func (r *repository) Name() string        { return r.name }
func (r *repository) TagsPageURL() string { return webOrigin + "/" + r.name + "/tags" }
func (r *repository) TagLivecheckURL(tag string) string {
	path := &url.URL{Path: "/" + r.name + "/archive/refs/tags/" + tag + ".tar.gz"}
	return webOrigin + path.String()
}

func validRepositoryName(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return false
			}
		}
	}
	return true
}
