package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/fetch"
)

// Registry reads an image's digest through the OCI distribution API, the
// registries' documented interface: what a tag names at the moment it is
// asked. Setup reads it on either side of pulling the tag, so the image
// it pulled is known by content, not by a tag that moves.
type Registry struct {
	HTTP *http.Client
	// Scheme is how the registry is reached, https unless a test says
	// otherwise.
	Scheme string
}

// ErrNoDigest reports a registry that couldn't say what a tag names.
var ErrNoDigest = errors.New("the registry didn't say what the tag names")

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// challengeParameter is one key="value" of a WWW-Authenticate challenge.
	challengeParameter = regexp.MustCompile(`(\w+)="([^"]*)"`)
)

// manifestTypes are the manifests a tag can name, as the distribution API
// asks them to be accepted.
var manifestTypes = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

// Digest is the digest a reference names now: the one it carries, as in
// ghcr.io/owner/image@sha256:…, or the one its tag names at the registry,
// latest when it has none. A registry that asks for a token gets an
// anonymous one, as the distribution API's token flow gives.
func (r Registry) Digest(ctx context.Context, reference string) (string, error) {
	host, repository, tag, digest, err := parseReference(reference)
	if err != nil {
		return "", err
	}
	if digest != "" {
		return digest, nil
	}
	scheme := r.Scheme
	if scheme == "" {
		scheme = "https"
	}
	client := r.HTTP
	if client == nil {
		client = fetch.Client
	}
	manifest := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, host, repository, tag)
	response, err := r.head(ctx, client, manifest, "")
	if err != nil {
		return "", err
	}
	if response.StatusCode == http.StatusUnauthorized {
		token, err := r.token(ctx, client, response.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", fmt.Errorf("%s: %w", reference, err)
		}
		if response, err = r.head(ctx, client, manifest, token); err != nil {
			return "", err
		}
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %s: %s", ErrNoDigest, reference, response.Status)
	}
	if digest := response.Header.Get("Docker-Content-Digest"); digestPattern.MatchString(digest) {
		return digest, nil
	}
	return "", fmt.Errorf("%w: %s: no digest in its reply", ErrNoDigest, reference)
}

// head asks for a manifest's headers, with a token when one is given.
func (r Registry) head(ctx context.Context, client *http.Client, manifest, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, manifest, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", manifestTypes)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return response, response.Body.Close()
}

// token is an anonymous token for a Bearer challenge's realm, service, and
// scope.
func (r Registry) token(ctx context.Context, client *http.Client, challenge string) (string, error) {
	scheme, parameters, ok := strings.Cut(challenge, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", fmt.Errorf("%w: it asked for %q, not a bearer token", ErrNoDigest, challenge)
	}
	values := map[string]string{}
	for _, parameter := range challengeParameter.FindAllStringSubmatch(parameters, -1) {
		values[parameter[1]] = parameter[2]
	}
	realm, err := url.Parse(values["realm"])
	if err != nil || realm.Scheme == "" || realm.Host == "" {
		return "", fmt.Errorf("%w: its token realm %q is unusable", ErrNoDigest, values["realm"])
	}
	query := realm.Query()
	for _, key := range []string{"service", "scope"} {
		if values[key] != "" {
			query.Set(key, values[key])
		}
	}
	realm.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: its token service said %s", ErrNoDigest, response.Status)
	}
	var reply struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil {
		return "", fmt.Errorf("%w: its token service's reply: %w", ErrNoDigest, err)
	}
	if reply.Token != "" {
		return reply.Token, nil
	}
	if reply.AccessToken != "" {
		return reply.AccessToken, nil
	}
	return "", fmt.Errorf("%w: its token service gave no token", ErrNoDigest)
}

// parseReference splits an image reference, host/repository[:tag] or
// host/repository@sha256:…, as the distribution API names it.
func parseReference(reference string) (host, repository, tag, digest string, err error) {
	name, digest, pinned := strings.Cut(reference, "@")
	if pinned && !digestPattern.MatchString(digest) {
		return "", "", "", "", fmt.Errorf("image %q: %q is not a sha256 digest", reference, digest)
	}
	host, path, ok := strings.Cut(name, "/")
	if !ok || !strings.ContainsAny(host, ".:") || path == "" {
		return "", "", "", "", fmt.Errorf("image %q names no registry host", reference)
	}
	repository, tag = path, "latest"
	if at := strings.LastIndex(path, ":"); at > strings.LastIndex(path, "/") {
		repository, tag = path[:at], path[at+1:]
	}
	if repository == "" || tag == "" {
		return "", "", "", "", fmt.Errorf("image %q names no repository or tag", reference)
	}
	return host, repository, tag, digest, nil
}
