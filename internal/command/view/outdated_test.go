package view

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
)

// outdated's JSON says a repository GitHub answers by another name as its
// text does, the repository and the name as fields beside the words
// (prioritization's, 2026-11-01, under the 2026-09-27 rule), and nothing
// of a port whose repository hasn't moved.
func TestOutdatedsJSONSaysARepositorysMove(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(OutdatedView(engine.OutdatedReport{Master: "1bb30d5", Ports: []engine.OutdatedPort{
		{Port: "wrangler", Current: "1.21.0", Newest: "1.21.0", RepositoryMove: &engine.RepositoryMove{From: "cloudflare/wrangler", To: "cloudflare/wrangler-legacy"}},
		{Port: "jq", Current: "1.8.1", Newest: "1.8.1"},
	}}))
	require.NoError(t, err)
	var viewed struct {
		Ports []map[string]any `json:"ports"`
	}
	require.NoError(t, json.Unmarshal(data, &viewed))
	require.Equal(t, map[string]any{
		"repository":  "cloudflare/wrangler",
		"answered_as": "cloudflare/wrangler-legacy",
		"words":       "GitHub answers cloudflare/wrangler as cloudflare/wrangler-legacy now; if that's the same project, github.setup could name it",
	}, viewed.Ports[0]["repository_move"])
	require.NotContains(t, viewed.Ports[1], "repository_move")
}
