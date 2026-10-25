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
	name := strings.TrimSuffix(strings.Trim(address.Path, "/"), ".git")
	instance := "https://" + address.Host
	var kind portsource.Forge
	switch spec, err := portsource.Interpret(port, portsource.Edit); {
	case err == nil && spec.Forge != "" && strings.EqualFold(spec.Instance, instance):
		kind = spec.Forge
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
