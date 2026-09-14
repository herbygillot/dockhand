package prepare_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

type releaseTagFunc func(context.Context, string, string) (forge.Tag, error)

func (f releaseTagFunc) Repository(_ string, name string) (forge.Repository, error) {
	return &releaseRepository{name: name, tag: f}, nil
}

type releaseRepository struct {
	forge.Repository
	name string
	tag  releaseTagFunc
}

func (r *releaseRepository) Name() string { return r.name }
func (r *releaseRepository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	return r.tag(ctx, r.name, name)
}

func versionFixture(t *testing.T, style, extra string, handler http.HandlerFunc) (*prepare.Service, prepare.Request) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, request := preparationFixture(t, "")
	declaration := "version 1.0\ngithub.setup owner fixture $version v\n"
	if style == "setup" {
		declaration = "github.setup owner fixture 1.0 v\n"
	}
	if style == "gitlab-setup" {
		declaration = "gitlab.setup group/subgroup fixture 1.0 v\n"
	}
	if strings.HasPrefix(style, "go-") {
		declaration = "PortGroup dockhand-go 1.0\ngo.setup github.com/owner/fixture 1.0 v\n"
		if style == "go-version" {
			declaration = "PortGroup dockhand-go 1.0\nversion 1.0\ngo.setup github.com/owner/fixture ${version} v\n"
		}
	}
	if style == "calculated" {
		declaration = "version [format %s 1.0]\ngithub.setup owner fixture $version v\n"
	}
	contents := `PortSystem 1.0
name fixture
categories devel
options github.author github.project github.version github.tag_prefix github.tag_suffix
options gitlab.author gitlab.project gitlab.version gitlab.tag_prefix gitlab.tag_suffix gitlab.instance
options git.branch
proc github.setup {owner project value prefix} {
 github.author $owner
 github.project $project
 github.version $value
 github.tag_prefix $prefix
 github.tag_suffix ""
 version $value
	git.branch ${prefix}${value}
}
proc gitlab.setup {owner project value prefix} {
 gitlab.author $owner
 gitlab.project $project
 gitlab.version $value
 gitlab.tag_prefix $prefix
 gitlab.tag_suffix ""
 gitlab.instance https://gitlab.example
 version $value
 git.branch ${prefix}${value}
}
` + declaration + fmt.Sprintf("revision 3\nmaster_sites %s/releases/${version}\nchecksums rmd160 %s \\\n    sha256 %s \\\n    size 1\n", server.URL, strings.Repeat("0", 40), strings.Repeat("0", 64)) + extra
	before, _, err := service.Repo.File(t.Context(), string(request.Source.Tree), "devel/fixture/Portfile")
	require.NoError(t, err)
	edits := []git.FileEdit{{Path: "devel/fixture/Portfile", Before: before, After: []byte(contents), Mode: before.Mode}}
	if strings.HasPrefix(style, "go-") {
		group := `options go.package go.domain go.version
proc go.setup {package value {prefix ""} {suffix ""}} {
 go.package $package
 go.domain github.com
 go.version $value
 github.setup owner fixture $value $prefix
}
`
		if style == "go-check" {
			group += `proc go_toolchain.ceiling {} { return 1.27 }
set go.toolchain_unmet ""
pre-fetch {
 global go.toolchain_unmet
 set ceiling [go_toolchain.ceiling]
 if {${ceiling} eq "none"} {
  ui_error "No supported toolchain for ${subport}"
  return -code error "unsupported platform"
 }
 if {${go.toolchain_unmet} ne ""} {
  ui_error "Needs Go ${go.toolchain_unmet}; newest is ${ceiling}"
  return -code error "unsupported toolchain"
 }
}
`
		}
		edits = append(edits, git.FileEdit{Path: "_resources/port1.0/group/dockhand-go-1.0.tcl", After: []byte(group), Mode: 0o100644})
	}
	tree, err := service.Repo.EditTree(t.Context(), string(request.Source.Tree), edits)
	require.NoError(t, err)
	request.Source = record.Source{Tree: record.ObjectID(tree)}
	request.Action = record.Bump
	request.Version = "2.0"
	resolver := releaseTagFunc(func(_ context.Context, repo, name string) (forge.Tag, error) {
		if name != "v2.0" {
			return forge.Tag{}, forge.ErrNotFound
		}
		return forge.Tag{Name: name, Commit: strings.Repeat("a", 40)}, nil
	})
	service.Upstream = &upstream.Service{Catalogs: map[portsource.Forge]upstream.Catalog{portsource.GitHub: resolver, portsource.GitLab: resolver}}
	release, err := service.ResolveRelease(t.Context(), request)
	require.NoError(t, err)
	request.Release = &release
	return service, request
}
func TestVersionPreparationUpdatesSourceAndChecksumsWithFidelity(t *testing.T) {
	for _, style := range []string{"literal", "setup", "gitlab-setup", "go-setup", "go-version", "go-check"} {
		t.Run(style, func(t *testing.T) {
			body := "fixture archive bytes"
			var requests atomic.Int64
			service, request := versionFixture(t, style, "", func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/releases/2.0/fixture-2.0.tar.gz" {
					w.WriteHeader(404)
					return
				}
				fmt.Fprint(w, body)
			})
			result, err := service.Prepare(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, int64(1), requests.Load())
			require.NotEmpty(t, result.PreparedTree)
			require.Len(t, result.Commits, 1)
			require.Equal(t, "fixture: update to 2.0", result.Commits[0].Subject)
			require.Equal(t, "2.0", result.Fidelity[1].After.Ports["fixture"].Version)
			require.Zero(t, result.Fidelity[1].After.Ports["fixture"].Revision)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(body))), result.Downloads[0].SHA256)
			require.Contains(t, string(result.Files[0].After), "revision 0")
			require.Contains(t, string(result.Files[0].After), "size 21")
			require.Empty(t, result.Fidelity[0].UnexpectedChanges)
			require.Empty(t, result.Fidelity[1].UnexpectedChanges)
			if strings.HasPrefix(style, "go-") {
				info := result.Fidelity[1].After.Ports["fixture"]
				require.Equal(t, "2.0", info.Options["go.version"])
				require.Equal(t, "1", info.Options["fetch.archive_compatible"])
				require.NotContains(t, info.Options, "fetch_details")
			}
		})
	}
}
func TestVersionPreparationRefusesCollateralChangesBeforeDownloading(t *testing.T) {
	for _, test := range []struct {
		name, style, extra string
		expected           error
	}{
		{"calculated", "calculated", "", prepare.ErrUnsupported},
		{"dependency", "literal", "if {$version eq {2.0}} {depends_lib port:other}\n", prepare.ErrFidelity},
		{"sibling", "setup", "subport fixture-child {}\n", prepare.ErrFidelity},
		{"fetch hook", "literal", "pre-fetch {error custom}\n", prepare.ErrUnsupported},
		{"conditional hook", "literal", "if {1} { pre-fetch {error custom} }\n", prepare.ErrUnsupported},
		{"custom hook after Go check", "go-check", "if {1} { pre-fetch {error custom} }\n", prepare.ErrUnsupported},
		{"post-fetch after Go check", "go-check", "if {1} { post-fetch {error custom} }\n", prepare.ErrUnsupported},
		{"Go dependency", "go-check", "if {$version eq {2.0}} {depends_lib port:other}\n", prepare.ErrFidelity},
		{"credentials", "literal", "fetch.password secret-test-value\n", prepare.ErrUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int64
			service, request := versionFixture(t, test.style, test.extra, func(w http.ResponseWriter, r *http.Request) { requests.Add(1); fmt.Fprint(w, "archive") })
			result, err := service.Prepare(t.Context(), request)
			require.ErrorIs(t, err, test.expected)
			require.Empty(t, result.PreparedTree)
			require.Empty(t, result.Commits)
			require.Zero(t, requests.Load())
		})
	}
}
func TestVersionPreparationRejectsTagMutationDuringDownload(t *testing.T) {
	var moved atomic.Bool
	service, request := versionFixture(t, "setup", "", func(w http.ResponseWriter, r *http.Request) { moved.Store(true); fmt.Fprint(w, "archive") })
	service.Upstream.Catalogs[portsource.GitHub] = releaseTagFunc(func(_ context.Context, _ string, name string) (forge.Tag, error) {
		commit := strings.Repeat("a", 40)
		if moved.Load() {
			commit = strings.Repeat("b", 40)
		}
		return forge.Tag{Name: name, Commit: commit}, nil
	})
	result, err := service.Prepare(t.Context(), request)
	require.ErrorIs(t, err, upstream.ErrSourceChanged)
	require.Empty(t, result.PreparedTree)
	require.Empty(t, result.Commits)
}
