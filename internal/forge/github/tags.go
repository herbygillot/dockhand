package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

type HTTPError struct {
	StatusCode int
	Resource   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github: GET %s returned HTTP %d", e.Resource, e.StatusCode)
}

type gitObject struct{ Type, SHA string }

func (c *Client) Tag(ctx context.Context, repository, name string) (upstream.Tag, error) {
	if !upstream.RepositoryName(repository) || !git.ValidRefName("refs/tags/"+name) {
		return upstream.Tag{}, fmt.Errorf("github: invalid repository or tag")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var ref struct {
		Ref    string
		Object gitObject
	}
	resource := "repos/" + repository + "/git/ref/tags/" + url.PathEscape(name)
	if err := c.get(ctx, resource, &ref); err != nil {
		return upstream.Tag{}, err
	}
	if ref.Ref != "refs/tags/"+name {
		return upstream.Tag{}, fmt.Errorf("github: response identifies a different ref")
	}
	object := ref.Object
	seen := map[string]bool{}
	for depth := 0; depth < 8; depth++ {
		if !git.ValidObjectID(object.SHA) || seen[object.SHA] {
			return upstream.Tag{}, fmt.Errorf("github: invalid or cyclic tag object")
		}
		seen[object.SHA] = true
		if object.Type == "commit" {
			return upstream.Tag{Name: name, Commit: object.SHA}, nil
		}
		if object.Type != "tag" {
			return upstream.Tag{}, fmt.Errorf("github: tag does not identify a commit")
		}
		var annotated struct {
			SHA    string
			Object gitObject
		}
		if err := c.get(ctx, "repos/"+repository+"/git/tags/"+object.SHA, &annotated); err != nil {
			return upstream.Tag{}, err
		}
		if annotated.SHA != object.SHA {
			return upstream.Tag{}, fmt.Errorf("github: response identifies a different tag object")
		}
		object = annotated.Object
	}
	return upstream.Tag{}, fmt.Errorf("github: annotated tag nesting exceeds supported depth")
}

func (c *Client) get(ctx context.Context, resource string, result any) error {
	base := c.Config.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	origin, err := url.Parse(base)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Scheme != "http" && origin.Scheme != "https" {
		return fmt.Errorf("github: invalid API base URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+resource, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "dockhand/2")
	if c.Config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Config.Token)
	}
	client := http.DefaultClient
	if c.HTTP != nil {
		client = c.HTTP
	}
	configured := *client
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host || req.URL.User != nil {
			return fmt.Errorf("github: redirect left configured API origin")
		}
		if len(via) >= 5 {
			return fmt.Errorf("github: too many redirects")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	response, err := configured.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", upstream.ErrTagMissing, resource)
	}
	if response.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: response.StatusCode, Resource: resource}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return err
	}
	if len(body) > 1<<20 {
		return fmt.Errorf("github: response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("github: invalid response: %w", err)
	}
	return nil
}

var _ upstream.TagReader = (*Client)(nil)
