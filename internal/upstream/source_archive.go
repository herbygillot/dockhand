package upstream

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
)

// sourceArchiveLimit is a forge's archive of a commit unbounded in size, as
// a distfile's download is, at the person's word (2026-10-01).
const sourceArchiveLimit = math.MaxInt64

// SourceArchive writes the forge's archive of a Git-fetched port's
// repository at a commit into a directory, and gives its path, for
// assessing what the port's change from one commit to another means (the
// assessment design, D), since a Git fetch downloads no archive to read.
// The repository is the one the commit was resolved in, source's URL, as
// git.url names it, never the forge PortGroup's where they differ.
func (s *Service) SourceArchive(ctx context.Context, port macports.PortInfo, source model.GitSource, directory string) (string, error) {
	repository, err := s.gitRepository(port, source.URL)
	if err != nil {
		return "", err
	}
	commit := string(source.Commit)
	archives, ok := repository.(forge.ArchiveRepository)
	if !ok {
		return "", fmt.Errorf("upstream: %s's forge makes no archives", repository.Name())
	}
	path := filepath.Join(directory, commit+".tar.gz")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	err = archives.Archive(ctx, commit, file, sourceArchiveLimit)
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// gitRepository is the repository a Git-fetched port's commits are read
// from, the one gitURL names: on its forge PortGroup's instance where it's
// that one, a self-hosted GitLab's among them, else on GitHub or
// GitLab.com, which say by their addresses which repository a URL is. A
// port that keeps github.setup owner project and points git.url at a fork
// asked owner/project for the fork's commit (C8-4).
func (s *Service) gitRepository(port macports.PortInfo, gitURL string) (forge.Repository, error) {
	address, err := url.Parse(gitURL)
	if err != nil || address.Scheme != "https" || address.Host == "" {
		return nil, errors.New("upstream: the port's git.url must be an HTTPS repository URL on a supported forge")
	}
	path := strings.Trim(address.Path, "/")
	name := strings.TrimSuffix(path, ".git")
	instance := "https://" + address.Host
	var kind portsource.Forge
	spec, err := portsource.Interpret(port, portsource.Edit)
	// The forge PortGroup's instance is its endpoint, a path and all, as a
	// self-hosted GitLab under https://gitlab.example.com/root is, and the
	// repository is the URL's path below it: comparing the URL's host
	// alone with the instance refused it (Codex's review of 8e314b96,
	// finding 8). A URL outside the instance goes by its own host.
	if err == nil && spec.Forge != "" && spec.Instance != "" {
		if below, ok := underEndpoint(spec.Instance, address); ok {
			kind, instance, name = spec.Forge, strings.TrimSuffix(spec.Instance, "/"), strings.TrimSuffix(below, ".git")
		}
	}
	switch {
	case kind != "":
	case address.Host == "github.com":
		kind = portsource.GitHub
	case address.Host == "gitlab.com":
		kind = portsource.GitLab
	default:
		return nil, fmt.Errorf("upstream: downloading source archives from %s is not supported", address.Host)
	}
	if s == nil || s.Catalogs[kind] == nil {
		return nil, fmt.Errorf("upstream: no catalog supports %s", kind)
	}
	return s.Catalogs[kind].Repository(instance, name)
}

// underEndpoint is the path of a repository URL below a forge's endpoint,
// an instance URL whose path may be more than its root; false where the
// URL is on another host or outside the endpoint's path.
func underEndpoint(endpoint string, repository *url.URL) (string, bool) {
	base, err := url.Parse(endpoint)
	if err != nil || !strings.EqualFold(base.Host, repository.Host) {
		return "", false
	}
	prefix, path := strings.Trim(base.Path, "/"), strings.Trim(repository.Path, "/")
	if prefix == "" {
		return path, path != ""
	}
	below, ok := strings.CutPrefix(path, prefix+"/")
	return below, ok && below != ""
}
