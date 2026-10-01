// Package pypi reads what the Python Package Index publishes for a
// release, through its JSON API, which PyPI documents
// (https://docs.pypi.org/api/json/): the files a release has, a source
// archive, wheels, or both.
package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/herbygillot/dockhand/internal/fetch"
)

// API is where PyPI's JSON API answers.
const API = "https://pypi.org/pypi/"

// File is one file a release publishes, and whether it's a source archive
// ("sdist") or a wheel ("bdist_wheel").
type File struct {
	Filename    string `json:"filename"`
	PackageType string `json:"packagetype"`
}

// maxRelease bounds a release's JSON read from PyPI, which names its files
// and the project's description; a release's is a few kilobytes.
const maxRelease = 8 << 20

// Client asks PyPI's JSON API.
type Client struct {
	HTTP *http.Client
	// Base is the API's address, API unless a test sets another.
	Base string
}

// Files are the files a project's release publishes.
func (c Client) Files(ctx context.Context, project, version string) ([]File, error) {
	base := c.Base
	if base == "" {
		base = API
	}
	address := strings.TrimRight(base, "/") + "/" + url.PathEscape(project) + "/" + url.PathEscape(version) + "/json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", fetch.UserAgent)
	var release struct {
		URLs []File `json:"urls"`
	}
	// Through fetch.Open, as dockhand's other reads go: a redirect from
	// HTTPS to plain HTTP is refused, and a reply too large is said as
	// one, where a body cut at the bound read as "unexpected EOF" (the
	// limits sweep, 2026-10-01).
	response, err := fetch.Open(c.HTTP, request, maxRelease)
	if err == nil {
		defer response.Body.Close()
		err = json.NewDecoder(response.Body).Decode(&release)
	}
	switch {
	case errors.Is(err, fetch.ErrTooLarge):
		return nil, fmt.Errorf("pypi: %s %s: its JSON is larger than the %d MiB dockhand reads of a release", project, version, maxRelease>>20)
	case err != nil:
		return nil, fmt.Errorf("pypi: %s %s: %w", project, version, err)
	}
	return release.URLs, nil
}

// SourceProject is the project and file a URL of PyPI's source archives
// names, files.pythonhosted.org/packages/source/f/flatbuffers/…, as the
// pypi mirror group expands; false for any other URL.
func SourceProject(address string) (project string, ok bool) {
	u, err := url.Parse(address)
	if err != nil || u.Host != "files.pythonhosted.org" {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "packages" || parts[1] != "source" {
		return "", false
	}
	return parts[3], true
}
