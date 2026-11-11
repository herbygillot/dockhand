package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
)

// errNotRecorded is discovery asking for something the replay hasn't: a
// replay miss, which says the port's fetch plan isn't what was captured,
// never a failure of dockhand's.
var errNotRecorded = errors.New("livecheck: not recorded")

// recording is what a port's replay answers from: its repository's tags
// and releases, and pages by URL.
type recording struct {
	Repository string            `json:"repository"`
	Tags       []string          `json:"tags"`
	Releases   []string          `json:"releases,omitempty"`
	Pages      map[string]string `json:"pages,omitempty"`
}

// catalog is a recording as a forge catalog: upstream.Catalog, and the
// documents it serves itself, as GitHub's client serves api.github.com.
type catalog struct{ recorded recording }

func (c catalog) Repository(_, name string) (forge.Repository, error) {
	if !strings.EqualFold(name, c.recorded.Repository) {
		return nil, fmt.Errorf("%w: repository %s", errNotRecorded, name)
	}
	return repository(c), nil
}

// Document serves a recorded page; one not recorded falls to the HTTP
// client, which refuses it.
func (c catalog) Document(_ context.Context, url string, _ http.Header) ([]byte, bool, error) {
	if page, ok := c.recorded.Pages[url]; ok {
		return []byte(page), true, nil
	}
	return nil, false, nil
}

// repository is a recording's repository: its tags, each at a commit of
// its own, and its releases.
type repository catalog

func (r repository) Name() string { return r.recorded.Repository }

func (r repository) Tag(_ context.Context, name string) (forge.Tag, error) {
	if !slices.Contains(r.recorded.Tags, name) {
		return forge.Tag{}, forge.ErrNotFound
	}
	return forge.Tag{Name: name, Commit: commitOf(name)}, nil
}

func (r repository) ListTags(context.Context) ([]forge.Tag, error) {
	tags := make([]forge.Tag, len(r.recorded.Tags))
	for i, name := range r.recorded.Tags {
		tags[i] = forge.Tag{Name: name, Commit: commitOf(name)}
	}
	return tags, nil
}

func (r repository) Releases(context.Context) ([]forge.Release, error) {
	names := r.recorded.Releases
	if names == nil {
		names = r.recorded.Tags
	}
	releases := make([]forge.Release, len(names))
	for i, name := range names {
		releases[i] = forge.Release{Tag: name}
	}
	return releases, nil
}

// commitOf is a tag's commit as the replay names it: forty hex digits
// of its own, the same for the same tag.
func commitOf(tag string) string {
	var b strings.Builder
	for b.Len() < 40 {
		for _, r := range tag {
			fmt.Fprintf(&b, "%x", r%16)
		}
		b.WriteString("0")
	}
	return b.String()[:40]
}

// pages is the HTTP client's transport: a recorded page by URL, and
// anything else refused as not recorded.
type pages map[string]string

func (p pages) RoundTrip(request *http.Request) (*http.Response, error) {
	page, ok := p[request.URL.String()]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNotRecorded, request.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": {"text/html"}},
		Body: io.NopCloser(bytes.NewReader([]byte(page))), Request: request}, nil
}
