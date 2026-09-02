package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The PR object is read wider than it is published. These pin both
// halves: the three fields that come free out of responses the tool
// already asks for, and the five-key document `status --json` has
// always emitted, which reading them must not widen.

// listPulls is one element of what `gh api repos/o/r/pulls?head=...`
// answers, trimmed to the keys this package names.
const listPulls = `[{
  "number": 77,
  "title": "jq: update to 2.5",
  "state": "closed",
  "merged_at": "2026-09-01T00:00:00Z",
  "html_url": "https://x/77",
  "merge_commit_sha": "0123456789abcdef0123456789abcdef01234567",
  "created_at": "2026-08-30T10:00:00Z",
  "updated_at": "2026-09-01T00:00:01Z"
}]`

func TestQueryPRReadsWhatTheListResponseAlreadyCarries(t *testing.T) {
	var argv []string
	run := func(_ context.Context, args ...string) (string, error) {
		argv = args
		return listPulls, nil
	}
	pr, found, err := QueryPR(context.Background(), run, "macports/macports-ports", "herbygillot", "dockhand/jq-open")
	require.NoError(t, err)
	require.True(t, found)

	// The three new fields cost nothing because the query is unchanged:
	// two arguments, no --jq and no extra field selection.
	assert.Equal(t, []string{
		"api",
		"repos/macports/macports-ports/pulls?head=herbygillot:dockhand/jq-open&state=all",
	}, argv)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", pr.MergeSha)
	assert.Equal(t, "2026-08-30T10:00:00Z", pr.CreatedAt)
	assert.Equal(t, "2026-09-01T00:00:01Z", pr.UpdatedAt)
}

func TestOpenPortPRsGetsTimestampsButNoMergeCommit(t *testing.T) {
	// A search result is an issue, not a pull: it carries the two
	// timestamps and keeps the same silence about the merge that it
	// already keeps about merged_at.
	run := func(context.Context, ...string) (string, error) {
		return `{"items":[{"number":3,"title":"jq: update to 2.5","state":"open",
		  "html_url":"https://x/3","created_at":"2026-08-30T10:00:00Z",
		  "updated_at":"2026-08-31T10:00:00Z"}]}`, nil
	}
	prs, err := OpenPortPRs(context.Background(), run, "macports/macports-ports", "jq")
	require.NoError(t, err)
	require.Len(t, prs, 1)
	assert.Equal(t, "2026-08-30T10:00:00Z", prs[0].CreatedAt)
	assert.Empty(t, prs[0].MergeSha)
	assert.Empty(t, prs[0].MergedAt)
}

func TestPullRequestPublishesFiveKeys(t *testing.T) {
	pr := PullRequest{
		Number: 77, Title: "jq: update to 2.5", State: "open",
		HTMLURL:  "https://x/77",
		MergeSha: "0123456789ab", CreatedAt: "2026-08-30T10:00:00Z", UpdatedAt: "2026-08-31T10:00:00Z",
	}
	b, err := json.Marshal(pr)
	require.NoError(t, err)
	assert.JSONEq(t, `{"number":77,"title":"jq: update to 2.5","state":"open","merged_at":"","html_url":"https://x/77"}`, string(b))
	// Field order and the absence of omitempty are part of the
	// document too — a key that moves or disappears when it is empty is
	// a diff in every recorded status.
	assert.Equal(t, []string{"number", "title", "state", "merged_at", "html_url"}, keysInOrder(t, b))

	// The published site marshals a pointer inside a document; the
	// method has to reach it there too.
	doc := struct {
		PR *PullRequest `json:"pr,omitempty"`
	}{PR: &pr}
	b, err = json.Marshal(doc)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "merge_commit_sha")
	assert.NotContains(t, string(b), "created_at")

	// published is hand-maintained, so a field added to PullRequest
	// without a decision about the document would just vanish from
	// `status --json` with nothing failing. Counting the read fields
	// makes that decision the compiler's business: widening PullRequest
	// fails here until someone edits this number, which is the moment to
	// ask whether the new key belongs in published too.
	assert.Equal(t, 8, reflect.TypeOf(PullRequest{}).NumField(),
		"PullRequest gained or lost a field; decide whether published gains it too")
	assert.Equal(t, 5, reflect.TypeOf(published{}).NumField(),
		"published is the document status --json emits; widening it is a change to what scripts parse")
}

// keysInOrder reads an object's keys off the token stream, which is
// the only way to see the order encoding/json wrote them in.
func keysInOrder(t *testing.T, b []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	open, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), open)
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		require.NoError(t, err)
		keys = append(keys, k.(string))
		var v any
		require.NoError(t, dec.Decode(&v))
	}
	return keys
}
