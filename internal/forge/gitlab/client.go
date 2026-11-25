package gitlab

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdk "gitlab.com/gitlab-org/api/client-go/v3"

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
	options := []sdk.ClientOptionFunc{sdk.WithBaseURL(r.apiBase), sdk.WithHTTPClient(limitedClient(r.client.HTTP)), sdk.WithCustomRetry(checkRetry), sdk.WithCustomRetryMax(limitRetries)}
	return sdk.NewClient("", options...)
}

func endpoint(instance, name string) (string, string, error) {
	parsed, err := url.Parse(instance)
	if err != nil || !forge.ValidGitLabInstance(parsed) {
		return "", "", fmt.Errorf("gitlab: invalid instance %q", instance)
	}
	if !forge.ValidGitLabProjectPath(name) {
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
