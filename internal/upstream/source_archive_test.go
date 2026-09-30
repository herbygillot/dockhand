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

// A Git-fetched port's archive of a commit comes from its forge
// PortGroup's repository, or, where it has none, from the repository its
// git.url names on GitHub or GitLab.com; another host is no forge to ask.
func TestASourceArchiveIsTheForgesOfTheCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, test := range []struct {
		options        map[string]string
		instance, name string
		fails          string
	}{
		{githubPort().Options, "https://github.com", "owner/project", ""},
		{map[string]string{"fetch.type": "git", "git.url": "https://github.com/harbor/harbor.git"}, "https://github.com", "harbor/harbor", ""},
		{map[string]string{"fetch.type": "git", "git.url": "https://gitlab.com/group/tool"}, "https://gitlab.com", "group/tool", ""},
		{map[string]string{"fetch.type": "git", "git.url": "https://git.example.org/tool.git"}, "", "", "no forge dockhand reads archives from"},
		{map[string]string{"fetch.type": "git", "git.url": "git://github.com/harbor/harbor.git"}, "", "", "no forge's https address"},
	} {
		catalog := &archiving{}
		service := &upstream.Service{Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: catalog, portsource.GitLab: catalog}}
		path, err := service.SourceArchive(t.Context(), macports.PortInfo{Name: "tool", Version: "1.0", Options: test.options}, commit, t.TempDir())
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
