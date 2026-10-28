package upstream_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// archiving is a catalog whose repositories make archives, recording
// which it was asked for.
type archiving struct {
	instance, name string
}

func (a *archiving) Repository(instance, name string) (forge.Repository, error) {
	a.instance, a.name = instance, name
	return a, nil
}
func (a *archiving) Name() string                                   { return a.name }
func (a *archiving) Tag(context.Context, string) (forge.Tag, error) { return forge.Tag{}, nil }
func (a *archiving) ListTags(context.Context) ([]forge.Tag, error)  { return nil, nil }
func (a *archiving) Archive(_ context.Context, commit string, into io.Writer, _ int64) error {
	_, err := fmt.Fprintf(into, "%s at %s", a.name, commit)
	return err
}

// A Git-fetched port's archive of a commit comes from the repository its
// commit was resolved in, the one git.url names, on its forge PortGroup's
// instance or on GitHub or GitLab.com; another host is no forge to ask. A
// port that keeps github.setup owner project with git.url pointed at a fork
// asks the fork (C8-4, the architecture review's probe).
func TestASourceArchiveIsTheForgesOfTheCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	forked := githubPort().Options
	forked["git.url"] = "https://github.com/fork/project.git"
	own := githubPort().Options
	own["git.url"] = "https://github.com/owner/project.git"
	for _, test := range []struct {
		options        map[string]string
		instance, name string
		fails          string
	}{
		{own, "https://github.com", "owner/project", ""},
		{forked, "https://github.com", "fork/project", ""},
		{map[string]string{"fetch.type": "git", "git.url": "https://github.com/harbor/harbor.git"}, "https://github.com", "harbor/harbor", ""},
		{map[string]string{"fetch.type": "git", "git.url": "https://gitlab.com/group/tool"}, "https://gitlab.com", "group/tool", ""},
		{map[string]string{"fetch.type": "git", "git.url": "https://git.example.org/tool.git"}, "", "", "downloading source archives from git.example.org is not supported"},
		{map[string]string{"fetch.type": "git", "git.url": "git://github.com/harbor/harbor.git"}, "", "", "must be an HTTPS repository URL on a supported forge"},
	} {
		catalog := &archiving{}
		service := &upstream.Service{Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: catalog, portsource.GitLab: catalog}}
		source := model.GitSource{URL: test.options["git.url"], Commit: model.ObjectID(commit)}
		path, err := service.SourceArchive(t.Context(), macports.PortInfo{Name: "tool", Version: "1.0", Options: test.options}, source, t.TempDir())
		if test.fails != "" {
			require.ErrorContains(t, err, test.fails)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, [2]string{test.instance, test.name}, [2]string{catalog.instance, catalog.name})
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, test.name+" at "+commit, string(data))
	}
}

// A self-hosted GitLab whose instance URL has a path is the endpoint its
// repositories are below: git.url's path past it is the repository. The
// URL's host alone was compared with the whole instance, and refused it as
// "not supported" (Codex's review of 8e314b96, finding 8). A git.url
// outside the instance's path is no repository of it.
func TestASelfHostedGitLabKeepsItsInstancesPath(t *testing.T) {
	t.Parallel()
	options := func(gitURL string) map[string]string {
		return map[string]string{"fetch.type": "git", "git.url": gitURL, "gitlab.author": "group", "gitlab.project": "tool", "gitlab.version": "1.0",
			"gitlab.tag_prefix": "v", "gitlab.tag_suffix": "", "gitlab.instance": "https://gitlab.example.com/root", "git.branch": "v1.0"}
	}
	archive := func(gitURL string) (*archiving, error) {
		catalog := &archiving{}
		service := &upstream.Service{Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitLab: catalog}}
		_, err := service.SourceArchive(t.Context(), macports.PortInfo{Name: "tool", Version: "1.0", Options: options(gitURL)},
			model.GitSource{URL: gitURL, Commit: model.ObjectID(strings.Repeat("a", 40))}, t.TempDir())
		return catalog, err
	}
	catalog, err := archive("https://gitlab.example.com/root/group/tool.git")
	require.NoError(t, err)
	require.Equal(t, "https://gitlab.example.com/root", catalog.instance)
	require.Equal(t, "group/tool", catalog.name)

	_, err = archive("https://gitlab.example.com/other/group/tool.git")
	require.ErrorContains(t, err, "not supported", "outside the instance's path")
}
