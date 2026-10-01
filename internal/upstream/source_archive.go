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
)

// sourceArchiveLimit is a forge's archive of a commit unbounded in size, as
// a distfile's download is, at the person's word (2026-10-01).
const sourceArchiveLimit = math.MaxInt64

// SourceArchive writes the forge's archive of a Git-fetched port's
// repository at a commit into a directory, and gives its path, for
// assessing what the port's change from one commit to another means (the
// assessment design, D), since a Git fetch downloads no archive to read.
func (s *Service) SourceArchive(ctx context.Context, port macports.PortInfo, commit, directory string) (string, error) {
	repository, err := s.gitRepository(port)
	if err != nil {
		return "", err
	}
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
// from: its forge PortGroup's, or, where it has none, the one its git.url
// names on GitHub or GitLab.com, which say by their addresses which
// repository a URL is.
func (s *Service) gitRepository(port macports.PortInfo) (forge.Repository, error) {
	spec, err := portsource.Interpret(port, portsource.Edit)
	if err == nil && spec.Forge != "" {
		_, repository, err := s.repository(port, false)
		return repository, err
	}
	address, err := url.Parse(port.Options["git.url"])
	if err != nil || address.Scheme != "https" {
		return nil, errors.New("upstream: the port's git.url is no forge's https address")
	}
	name := strings.TrimSuffix(strings.Trim(address.Path, "/"), ".git")
	var kind portsource.Forge
	switch address.Host {
	case "github.com":
		kind = portsource.GitHub
	case "gitlab.com":
		kind = portsource.GitLab
	default:
		return nil, fmt.Errorf("upstream: %s is no forge dockhand reads archives from", address.Host)
	}
	if s == nil || s.Catalogs[kind] == nil {
		return nil, fmt.Errorf("upstream: no catalog supports %s", kind)
	}
	return s.Catalogs[kind].Repository("https://"+address.Host, name)
}
