package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
)

// readingChunk is how many pull requests one GraphQL request reads.
const readingChunk = 25

// pullRequestFields are what a reading takes of each pull request: its
// state and head, and its latest opinion from each reviewer and its
// head's checks and statuses, as Observe and Inspect read them, a hundred
// of each; one with more is Partial, and Inspect reads it.
const pullRequestFields = `number state isDraft headRefOid
latestOpinionatedReviews(first: 100) { totalCount nodes { state submittedAt author { login } } }
commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { totalCount nodes {
  __typename
  ... on CheckRun { name status conclusion }
  ... on StatusContext { context state }
} } } } } }`

type graphReview struct {
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
	Author      *struct {
		Login string `json:"login"`
	} `json:"author"`
}

type graphContext struct {
	Type       string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Context    string `json:"context"`
	State      string `json:"state"`
}

type graphPullRequest struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	IsDraft    bool   `json:"isDraft"`
	HeadRefOID string `json:"headRefOid"`
	Reviews    struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []graphReview `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					Contexts struct {
						TotalCount int            `json:"totalCount"`
						Nodes      []graphContext `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// ReadPullRequests reads the pull requests with GitHub's GraphQL API,
// twenty-five a request, where Observe and Inspect take about five
// requests each: the pull-request half of spending less of GitHub's
// allowance (the SQL study's O8). One GitHub hasn't is not Found; one with
// more reviews or checks than a reading takes is Partial.
func (c *Client) ReadPullRequests(ctx context.Context, refs []forge.PullRequestRef) ([]forge.PullRequestReading, error) {
	for _, ref := range refs {
		if ref.Forge != forge.GitHub || !githubapi.ValidRepositoryName(ref.Repository) || ref.Number <= 0 {
			return nil, fmt.Errorf("github: invalid pull-request reference")
		}
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	var readings []forge.PullRequestReading
	for chunk := range slices.Chunk(refs, readingChunk) {
		var query strings.Builder
		query.WriteString("query {\n")
		for i, ref := range chunk {
			owner, name, _ := strings.Cut(ref.Repository, "/")
			fmt.Fprintf(&query, "p%d: repository(owner: %q, name: %q) { pullRequest(number: %d) { ...fields } }\n", i, owner, name, ref.Number)
		}
		query.WriteString("}\nfragment fields on PullRequest { " + pullRequestFields + " }")
		request, err := client.NewRequest(ctx, "POST", "graphql", map[string]any{"query": query.String()})
		if err != nil {
			return nil, err
		}
		var result struct {
			Data map[string]*struct {
				PullRequest *graphPullRequest `json:"pullRequest"`
			} `json:"data"`
			Errors []struct {
				Type    string   `json:"type"`
				Path    []string `json:"path"`
				Message string   `json:"message"`
			} `json:"errors"`
		}
		if _, err := client.Do(request, &result); err != nil {
			return nil, githubapi.RateLimitError(err)
		}
		// A pull request GitHub hasn't is an error at its path, and null;
		// any other error is the read's.
		for _, e := range result.Errors {
			if e.Type != "NOT_FOUND" {
				return nil, fmt.Errorf("github: reading pull requests: %s", e.Message)
			}
		}
		for i, ref := range chunk {
			reading := forge.PullRequestReading{Ref: ref}
			if found := result.Data[fmt.Sprintf("p%d", i)]; found != nil && found.PullRequest != nil {
				if reading, err = readingOf(ref, *found.PullRequest); err != nil {
					return nil, err
				}
			}
			readings = append(readings, reading)
		}
	}
	return readings, nil
}

// readingOf is a pull request as GraphQL gave it, its reviews and checks
// counted as Inspect counts them.
func readingOf(ref forge.PullRequestRef, pr graphPullRequest) (forge.PullRequestReading, error) {
	if pr.Number != ref.Number || !git.ValidObjectID(pr.HeadRefOID) {
		return forge.PullRequestReading{}, fmt.Errorf("github: invalid pull-request reading")
	}
	state := map[string]forge.PullRequestState{"OPEN": forge.PullRequestOpen, "CLOSED": forge.PullRequestClosed, "MERGED": forge.PullRequestMerged}[pr.State]
	if state == "" {
		return forge.PullRequestReading{}, fmt.Errorf("github: pull request #%d is %q", pr.Number, pr.State)
	}
	reading := forge.PullRequestReading{Ref: ref, Found: true, PullRequest: forge.PullRequest{Ref: ref, State: state, RemoteHead: model.ObjectID(pr.HeadRefOID)}}
	status := forge.PullRequestStatus{Draft: pr.IsDraft, Mergeable: "unknown", Review: "none", ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	reading.Partial = pr.Reviews.TotalCount > len(pr.Reviews.Nodes)
	var opinions []opinion
	for _, r := range pr.Reviews.Nodes {
		if r.Author != nil {
			opinions = append(opinions, opinion{Login: r.Author.Login, State: r.State, At: r.SubmittedAt})
		}
	}
	review(&status, opinions)
	if nodes := pr.Commits.Nodes; len(nodes) > 0 && nodes[0].Commit.Rollup != nil {
		contexts := nodes[0].Commit.Rollup.Contexts
		reading.Partial = reading.Partial || contexts.TotalCount > len(contexts.Nodes)
		for _, c := range contexts.Nodes {
			if c.Type == "CheckRun" {
				check(&status, true, c.Name, c.Status, c.Conclusion)
			} else {
				check(&status, false, c.Context, c.State, "")
			}
		}
	}
	slices.Sort(status.Checks.Failing)
	status.Checks.Failing = slices.Compact(status.Checks.Failing)
	reading.Status = status
	return reading, nil
}

var _ forge.PullRequestReader = (*Client)(nil)
