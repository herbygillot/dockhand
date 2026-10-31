package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// listingServer is GitHub as the read-ahead meets it: GraphQL answers
// each aliased repository from repositories, by the owner/name asked, or
// as GitHub answers one it hasn't, and a REST request is noted, and
// answered from rest by its path.
type listingServer struct {
	*httptest.Server
	mu           sync.Mutex
	repositories map[string]map[string]any
	rest         map[string]string
	// failQuery answers every GraphQL query with an error.
	failQuery bool
	cost      int
	queries   int
	asked     []string
}

func newListingServer(t *testing.T) *listingServer {
	s := &listingServer{repositories: map[string]map[string]any{}, rest: map[string]string{}, cost: 1}
	alias := regexp.MustCompile(`(r\d+): repository\(owner: "([^"]+)", name: "([^"]+)"\)`)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path != "/graphql" {
			s.asked = append(s.asked, r.URL.Path)
			body, ok := s.rest[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, body)
			return
		}
		require.Equal(t, "Bearer t", r.Header.Get("Authorization"), "GraphQL is asked signed in")
		s.queries++
		if s.failQuery {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		var request struct{ Query string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Contains(t, request.Query, "rateLimit { cost }")
		data := map[string]any{"rateLimit": map[string]any{"cost": s.cost}}
		var errs []any
		for _, match := range alias.FindAllStringSubmatch(request.Query, -1) {
			if repository, ok := s.repositories[match[2]+"/"+match[3]]; ok {
				data[match[1]] = repository
				continue
			}
			data[match[1]] = nil
			errs = append(errs, map[string]any{"type": "NOT_FOUND", "path": []any{match[1]}, "message": "Could not resolve to a Repository with the name '" + match[2] + "/" + match[3] + "'."})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "errors": errs})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *listingServer) restAsked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func commitObject(c byte) map[string]any {
	return map[string]any{"__typename": "Commit", "oid": strings.Repeat(string(c), 40)}
}

// graphRepository is a repository as the read-ahead's query answers it.
func graphRepository(name string, tags []any, more bool) map[string]any {
	return map[string]any{
		"nameWithOwner": name,
		"refs":          map[string]any{"pageInfo": map[string]any{"hasNextPage": more}, "nodes": tags},
	}
}

func signedIn(server *listingServer) *github.Client {
	return &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL, Token: "t"}}}
}

func bind(t *testing.T, client *github.Client, name string) githubRepository {
	t.Helper()
	repository, err := client.Repository("https://github.com", name)
	require.NoError(t, err)
	return repository.(githubRepository)
}

// The tags read ahead, many repositories a query, are listed as the REST
// listing gives them, without a request of their own: an annotated tag
// through its tag object, and a tag of a blob left out. A repository's
// releases are listed through REST, and the tags read ahead are remembered
// for the release it chooses. The query's points are counted, and no
// request is.
func TestReadAheadListsWithoutRequests(t *testing.T) {
	server := newListingServer(t)
	server.cost = 2
	server.repositories["owner/tags"] = graphRepository("owner/tags", []any{
		map[string]any{"name": "v2.0", "target": commitObject('a')},
		map[string]any{"name": "v1.0", "target": map[string]any{"__typename": "Tag", "oid": strings.Repeat("e", 40), "target": commitObject('b')}},
		map[string]any{"name": "key", "target": map[string]any{"__typename": "Blob", "oid": strings.Repeat("f", 40)}},
	}, false)
	server.repositories["owner/releases"] = graphRepository("owner/releases", []any{
		map[string]any{"name": "v3", "target": commitObject('c')},
	}, false)
	server.rest["/repos/owner/releases/releases"] = `[{"tag_name":"v3","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z"}]`
	client := signedIn(server)
	spend := githubapi.StartSpend()
	done := client.PrefetchListings(t.Context(), "https://github.com", []string{"owner/tags", "owner/releases"})
	defer done()

	tagged := bind(t, client, "owner/tags")
	tags, err := tagged.ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Tag{{Name: "v2.0", Commit: strings.Repeat("a", 40)}, {Name: "v1.0", Commit: strings.Repeat("b", 40)}}, tags)
	tag, err := tagged.Tag(t.Context(), "v1.0")
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("b", 40), tag.Commit)
	require.Empty(t, tagged.(forge.AnsweringRepository).AnswersAs())

	released := bind(t, client, "owner/releases")
	releases, err := released.Releases(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Release{{Tag: "v3", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}, releases)
	tag, err = released.Tag(t.Context(), "v3")
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("c", 40), tag.Commit)

	require.Equal(t, []string{"/repos/owner/releases/releases"}, server.restAsked(), "only the releases are asked of REST")
	require.Equal(t, 1, server.queries, "two repositories are one query")
	require.EqualValues(t, 2, spend.Points())
	require.Equal(t, int64(1), spend.Requests(), "a GraphQL query is counted in points, not requests")
}

// A repository the read-ahead couldn't read whole, as one with more than
// a page of tags, which the REST listing reads to its end, one GitHub
// hasn't, and one past what it reads the same as REST, lists through REST
// as before; so does every repository of a query that failed, and of the
// queries after it.
func TestReadAheadFallsBackToREST(t *testing.T) {
	restTags := `[{"name":"v9","commit":{"sha":"` + strings.Repeat("d", 40) + `"}}]`
	t.Run("past the first page, missing, or a tag of a tag", func(t *testing.T) {
		server := newListingServer(t)
		server.repositories["owner/many"] = graphRepository("owner/many", []any{map[string]any{"name": "v1", "target": commitObject('a')}}, true)
		server.repositories["owner/nested"] = graphRepository("owner/nested", []any{
			map[string]any{"name": "v1", "target": map[string]any{"__typename": "Tag", "oid": strings.Repeat("e", 40), "target": map[string]any{"__typename": "Tag", "oid": strings.Repeat("f", 40)}}},
		}, false)
		for _, name := range []string{"owner/many", "owner/missing", "owner/nested"} {
			server.rest["/repos/"+name+"/tags"] = restTags
		}
		client := signedIn(server)
		done := client.PrefetchListings(t.Context(), "https://github.com", []string{"owner/many", "owner/missing", "owner/nested"})
		defer done()
		for _, name := range []string{"owner/many", "owner/missing", "owner/nested"} {
			tags, err := bind(t, client, name).ListTags(t.Context())
			require.NoError(t, err)
			require.Equal(t, []forge.Tag{{Name: "v9", Commit: strings.Repeat("d", 40)}}, tags, name)
		}
		require.Equal(t, []string{"/repos/owner/many/tags", "/repos/owner/missing/tags", "/repos/owner/nested/tags"}, server.restAsked())
	})
	t.Run("a failed query", func(t *testing.T) {
		server := newListingServer(t)
		server.failQuery = true
		// Seventy-five repositories are three queries, two asked at once;
		// the first fails, and the third isn't asked.
		var names, asked []string
		for n := range 75 {
			name := fmt.Sprintf("owner/project-%d", n)
			server.repositories[name] = graphRepository(name, nil, false)
			server.rest["/repos/"+name+"/tags"] = restTags
			names, asked = append(names, name), append(asked, "/repos/"+name+"/tags")
		}
		client := signedIn(server)
		done := client.PrefetchListings(t.Context(), "https://github.com", names)
		defer done()
		for _, name := range names {
			tags, err := bind(t, client, name).ListTags(t.Context())
			require.NoError(t, err)
			require.Len(t, tags, 1)
		}
		require.Equal(t, asked, server.restAsked())
		server.mu.Lock()
		defer server.mu.Unlock()
		require.LessOrEqual(t, server.queries, 2, "at most the queries already asked when the first failed")
	})
}

// GraphQL's nameWithOwner is the name a repository answers by, as the
// acorn rule reads it: acorn-io/acorn answering as obot-platform/obot.
func TestReadAheadSaysTheNameARepositoryAnswersBy(t *testing.T) {
	server := newListingServer(t)
	server.repositories["acorn-io/acorn"] = graphRepository("obot-platform/obot", []any{map[string]any{"name": "v0.27.0", "target": commitObject('a')}}, false)
	client := signedIn(server)
	done := client.PrefetchListings(t.Context(), "https://github.com", []string{"acorn-io/acorn"})
	defer done()
	repository := bind(t, client, "acorn-io/acorn")
	_, err := repository.ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, "obot-platform/obot", repository.(forge.AnsweringRepository).AnswersAs())
	require.Empty(t, server.restAsked())
}

// Without a login nothing is read ahead, GraphQL taking only a signed-in
// request, and a repository lists through REST as before; once the look
// is done, nothing read ahead is listed from either.
func TestReadAheadNeedsALoginAndEndsWithTheLook(t *testing.T) {
	server := newListingServer(t)
	server.repositories["owner/project"] = graphRepository("owner/project", []any{map[string]any{"name": "v1", "target": commitObject('a')}}, false)
	server.rest["/repos/owner/project/tags"] = `[{"name":"v9","commit":{"sha":"` + strings.Repeat("d", 40) + `"}}]`
	anonymous := &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	done := anonymous.PrefetchListings(t.Context(), "https://github.com", []string{"owner/project"})
	tags, err := bind(t, anonymous, "owner/project").ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, "v9", tags[0].Name)
	done()
	require.Zero(t, server.queries)

	client := signedIn(server)
	client.PrefetchListings(t.Context(), "https://github.com", []string{"owner/project"})()
	tags, err = bind(t, client, "owner/project").ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, "v9", tags[0].Name)
	require.Equal(t, []string{"/repos/owner/project/tags", "/repos/owner/project/tags"}, server.restAsked())
}
