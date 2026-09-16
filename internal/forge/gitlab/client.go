package gitlab

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	sdk "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/herbygillot/dockhand/internal/forge"
)

type Client struct {
	HTTP *http.Client
}

type repository struct {
	client  *Client
	name    string
	apiBase string
	project string
}

func (c *Client) Repository(instance, name string) (forge.Repository, error) {
	if c == nil {
		return nil, fmt.Errorf("gitlab: client is required")
	}
	apiBase, project, err := endpoint(instance, name)
	if err != nil {
		return nil, err
	}
	return &repository{client: c, name: name, apiBase: apiBase, project: project}, nil
}

func (r *repository) Name() string { return r.name }

func (r *repository) api() (*sdk.Client, error) {
	options := []sdk.ClientOptionFunc{sdk.WithBaseURL(r.apiBase)}
	if r.client.HTTP != nil {
		options = append(options, sdk.WithHTTPClient(r.client.HTTP))
	}
	return sdk.NewClient("", options...)
}

func endpoint(instance, name string) (string, string, error) {
	parsed, err := url.Parse(instance)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", "", fmt.Errorf("gitlab: invalid instance %q", instance)
	}
	if !validProjectPath(name) {
		return "", "", fmt.Errorf("gitlab: invalid repository %q", name)
	}
	prefix := strings.Trim(parsed.Path, "/")
	project := name
	if prefix != "" {
		project = prefix + "/" + project
	}
	origin := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
	return origin.String(), project, nil
}

func validProjectPath(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.IndexFunc(part, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("?#%\\", r)
		}) >= 0 {
			return false
		}
	}
	return true
}
