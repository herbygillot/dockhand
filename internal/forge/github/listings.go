package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
)

// listingBatch is how many repositories one GraphQL query reads the tags
// of. GraphQL is no answer that costs nothing: GitHub spends about a tenth
// of a second reading a repository's page of tags, and twice that with
// its releases beside them, so fifty repositories with both took past the
// ten seconds GitHub gives a query, and it answered 502 (the measured look
// over 1,115 ports, 2026-10-31). Twenty-five, tags alone, took 3 to 4.5 s.
// By GitHub's documented cost, a request for each connection, summed and
// divided by 100, such a query costs one point of GraphQL's 5,000 an hour.
const listingBatch = 25

// listingsAtOnce is how many of those queries are asked at once. GitHub
// limits the time its servers spend on a login's GraphQL queries too (its
// documented secondary rate limit, 60 seconds a minute), and four at once,
// each timing out, met it, which held the look's REST reads for minutes;
// two at once didn't.
const listingsAtOnce = 2

// listingWait is how long one of those queries is waited for, past
// GitHub's own ten seconds, before its repositories are listed by
// themselves.
const listingWait = 20 * time.Second

// listingFields are what a query reads of each repository: the name it
// answers as, and the first page of its tags, each with the commit it
// names, an annotated tag's through its tag object. The order is a name's,
// which costs GitHub least; a listing is used only where the page holds
// every tag, and then the order is nothing discovery reads.
const listingFields = `nameWithOwner
refs(refPrefix: "refs/tags/", first: 100, orderBy: {field: ALPHABETICAL, direction: DESC}) {
  pageInfo { hasNextPage }
  nodes { name target { __typename oid ... on Tag { target { __typename oid } } } }
}`

// A listing is one repository's tags, read ahead. done is closed once
// it's read, or given up on; only then are its fields set.
type listing struct {
	asked string
	done  chan struct{}
	// read says GitHub answered for the repository, as name, its
	// nameWithOwner; whole, that tags are all of its tags, as the REST
	// listing would give them.
	read  bool
	name  string
	tags  []forge.Tag
	whole bool
}

type graphObject struct {
	Type string `json:"__typename"`
	OID  string `json:"oid"`
}

type graphRepository struct {
	NameWithOwner string `json:"nameWithOwner"`
	Refs          *struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Nodes []*struct {
			Name   string `json:"name"`
			Target *struct {
				graphObject
				Target *graphObject `json:"target"`
			} `json:"target"`
		} `json:"nodes"`
	} `json:"refs"`
}

// PrefetchListings reads the tags of the repositories named with GitHub's
// GraphQL API, twenty-five a query, where a look's discoveries asked
// GitHub's REST API about each, a request or more a repository, paced
// 80 ms apart: GitHub's 138 s of a 148 s look over 1,115 ports (the
// roadmap's v0.8.0). A repository's ListTags lists from what was read,
// waiting for it where it's still being read, and its Releases remembers
// the tags read, for the release it chooses. One GitHub didn't answer for,
// or one with more than a page of tags, which the REST listing reads to
// its end, lists through REST and its conditional cache, as before; so do
// releases, which few ports read. GraphQL takes only a signed-in request,
// so without a login nothing is read ahead.
func (c *Client) PrefetchListings(ctx context.Context, instance string, names []string) (done func()) {
	nothing := func() {}
	if c == nil || c.Client == nil || instance != publicInstance {
		return nothing
	}
	api, signedIn := c.SignedInAPI(ctx)
	if !signedIn {
		return nothing
	}
	var mine []*listing
	var keys []string
	c.listingsMu.Lock()
	if c.listings == nil {
		c.listings = map[string]*listing{}
	}
	for _, name := range names {
		key := strings.ToLower(name)
		if !githubapi.ValidRepositoryName(name) || c.listings[key] != nil {
			continue
		}
		entry := &listing{asked: name, done: make(chan struct{})}
		c.listings[key] = entry
		mine, keys = append(mine, entry), append(keys, key)
	}
	c.listingsMu.Unlock()
	if len(mine) == 0 {
		return nothing
	}
	ctx, cancel := context.WithCancel(ctx)
	var reading sync.WaitGroup
	turns := make(chan struct{}, listingsAtOnce)
	// The queries go in the order the repositories were named, as the
	// look's lookups do, so the first lookups' answers come first.
	// A query that fails, as one GitHub timed out or a secondary rate
	// limit refused, ends the reading ahead: what's left is listed by
	// itself, rather than asking again what GitHub just refused.
	var stopped atomic.Bool
	reading.Go(func() {
		for chunk := range slices.Chunk(mine, listingBatch) {
			select {
			case turns <- struct{}{}:
			case <-ctx.Done():
			}
			if ctx.Err() != nil || stopped.Load() {
				for _, entry := range chunk {
					close(entry.done)
				}
				continue
			}
			reading.Go(func() {
				defer func() { <-turns }()
				if !c.readListings(ctx, api, chunk) {
					stopped.Store(true)
				}
			})
		}
	})
	return func() {
		cancel()
		reading.Wait()
		c.listingsMu.Lock()
		defer c.listingsMu.Unlock()
		for i, key := range keys {
			if c.listings[key] == mine[i] {
				delete(c.listings, key)
			}
		}
	}
}

// readListings reads one query's repositories, and closes each one's
// listing, read or not; false where the query failed.
func (c *Client) readListings(ctx context.Context, api *gh.Client, chunk []*listing) bool {
	defer func() {
		for _, entry := range chunk {
			close(entry.done)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, listingWait)
	defer cancel()
	var query strings.Builder
	query.WriteString("query {\nrateLimit { cost }\n")
	for i, entry := range chunk {
		owner, name, _ := strings.Cut(entry.asked, "/")
		fmt.Fprintf(&query, "r%d: repository(owner: %q, name: %q) { ...listing }\n", i, owner, name)
	}
	query.WriteString("}\nfragment listing on Repository { " + listingFields + " }")
	var data map[string]json.RawMessage
	errs, err := githubapi.GraphQL(ctx, api, query.String(), nil, &data)
	if err != nil {
		progress.VerboseReport(ctx, "GitHub's GraphQL API couldn't read the tags of %d repositories ahead (%v); each is listed by itself", len(chunk), err)
		return false
	}
	// A repository GitHub hasn't, or couldn't read, is an error at its
	// alias, and is listed by itself; an error at no repository's alias
	// is the query's, and each is.
	failed := map[string]bool{}
	for _, e := range errs {
		alias, ok := "", false
		if len(e.Path) > 0 {
			alias, ok = e.Path[0].(string)
		}
		if !ok {
			progress.VerboseReport(ctx, "GitHub's GraphQL API couldn't read the tags of %d repositories ahead (%s); each is listed by itself", len(chunk), e.Message)
			return false
		}
		failed[alias] = true
	}
	for i, entry := range chunk {
		var repository *graphRepository
		alias := fmt.Sprintf("r%d", i)
		if failed[alias] || json.Unmarshal(data[alias], &repository) != nil || repository == nil || !githubapi.ValidRepositoryName(repository.NameWithOwner) {
			continue
		}
		entry.tags, entry.whole = tagsOf(*repository)
		entry.name, entry.read = repository.NameWithOwner, true
	}
	return true
}

// tagsOf are the tags GraphQL listed, as the REST listing gives them
// (ListTags): each with the commit it names, an annotated tag's through its
// tag object, and one naming no commit, as git/git's key blob, or with an
// unusable name, left out. False where it listed a page of more, or
// anything the REST listing might give otherwise than read here, as a tag
// of a tag or an annotated tag of a blob: REST lists those.
func tagsOf(repository graphRepository) ([]forge.Tag, bool) {
	refs := repository.Refs
	if refs == nil || refs.PageInfo.HasNextPage {
		return nil, false
	}
	seen := map[string]bool{}
	var tags []forge.Tag
	for _, node := range refs.Nodes {
		if node == nil || node.Target == nil || seen[node.Name] {
			return nil, false
		}
		seen[node.Name] = true
		object := node.Target.graphObject
		switch {
		case object.Type == "Blob" || object.Type == "Tree":
			continue
		case object.Type == "Tag" && node.Target.Target != nil:
			object = *node.Target.Target
		}
		if object.Type != "Commit" || !git.ValidObjectID(object.OID) {
			return nil, false
		}
		if !git.ValidRefName("refs/tags/" + node.Name) {
			continue
		}
		tags = append(tags, forge.Tag{Name: node.Name, Commit: object.OID})
	}
	return tags, true
}

// listing is what was read ahead of the repository named, once it's read,
// and false where nothing was, or GitHub didn't answer for it.
func (c *Client) listing(ctx context.Context, name string) (*listing, bool) {
	c.listingsMu.Lock()
	entry := c.listings[strings.ToLower(name)]
	c.listingsMu.Unlock()
	if entry == nil {
		return nil, false
	}
	select {
	case <-entry.done:
	case <-ctx.Done():
		return nil, false
	}
	return entry, entry.read
}

var _ forge.ListingPrefetcher = (*Client)(nil)
