// Package pypi reads what the Python Package Index publishes for a
// release, through its JSON API, which PyPI documents
// (https://docs.pypi.org/api/json/): the files a release has, a source
// archive, wheels, or both.
package pypi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// API is where PyPI's JSON API answers.
const API = "https://pypi.org/pypi/"

// File is one file a release publishes, and whether it's a source archive
// ("sdist") or a wheel ("bdist_wheel").
type File struct {
	Filename    string `json:"filename"`
	PackageType string `json:"packagetype"`
}

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
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pypi: %s %s: HTTP %d", project, version, response.StatusCode)
	}
	var release struct {
		URLs []File `json:"urls"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&release); err != nil {
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
