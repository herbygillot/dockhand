package github

import (
	"context"
	"encoding/json"

	gh "github.com/google/go-github/v93/github"
)

// GraphQLError is one of the errors a GraphQL answer gives, with the path
// of the field it's at: an aliased field's error names its alias first.
type GraphQLError struct {
	Type    string `json:"type"`
	Path    []any  `json:"path"`
	Message string `json:"message"`
}

// GraphQL sends a query or a mutation to GitHub's GraphQL API, decodes its
// data into data, and returns the errors it answered with beside it. What
// it cost is counted (SpendPoints): what its rateLimit { cost } says,
// where the query asks for it, and otherwise one, the least a query or a
// mutation costs.
func GraphQL(ctx context.Context, api *gh.Client, query string, variables map[string]any, data any) ([]GraphQLError, error) {
	body := map[string]any{"query": query}
	if variables != nil {
		body["variables"] = variables
	}
	request, err := api.NewRequest(ctx, "POST", "graphql", body)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []GraphQLError  `json:"errors"`
	}
	if _, err := api.Do(request, &answer); err != nil {
		return nil, RateLimitError(err)
	}
	var cost struct {
		RateLimit *struct {
			Cost int `json:"cost"`
		} `json:"rateLimit"`
	}
	points := 1
	if json.Unmarshal(answer.Data, &cost) == nil && cost.RateLimit != nil {
		points = cost.RateLimit.Cost
	}
	SpendPoints(points)
	if data != nil && len(answer.Data) > 0 && string(answer.Data) != "null" {
		if err := json.Unmarshal(answer.Data, data); err != nil {
			return answer.Errors, err
		}
	}
	return answer.Errors, nil
}
