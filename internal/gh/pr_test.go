package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestOpenPortPRsWalksTheRestListAndFiltersByTitle(t *testing.T) {
	// The duplicate check is off the search quota and onto the list
	// endpoint, which answers with the same object QueryPR already reads
	// — so the timestamps come free and the prefix filter is now exact
	// rather than a narrowing of what in:title matched.
	var argv []string
	run := func(_ context.Context, args ...string) (string, error) {
		argv = args
		return `[{"number":3,"title":"jq: update to 2.5","state":"open",
		  "html_url":"https://x/3","created_at":"2026-08-30T10:00:00Z",
		  "updated_at":"2026-08-31T10:00:00Z","head":{"ref":"dockhand/jq-2.5","sha":"abc"}},
		 {"number":4,"title":"jqdata: update to 1.0","state":"open","html_url":"https://x/4"},
		 {"number":5,"title":"gnutls: mention jq: in passing","state":"open","html_url":"https://x/5"}]`, nil
	}
	prs, err := OpenPortPRs(context.Background(), run, "macports/macports-ports", "jq")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"api", "repos/macports/macports-ports/pulls?state=open&per_page=100&page=1",
	}, argv, "a plain GET: no -X, no -f, and nothing that costs the search quota")
	require.Len(t, prs, 1, "jqdata: is another port and a mention is not a claim")
	assert.Equal(t, 3, prs[0].Number)
	assert.Equal(t, "2026-08-30T10:00:00Z", prs[0].CreatedAt)
	// The head ref is read off GitHub's nested object; what the name
	// means is the engine's reading, not this package's.
	assert.Equal(t, "dockhand/jq-2.5", prs[0].Head.Ref)
}

func TestOpenPortPRsPagesUntilAShortPage(t *testing.T) {
	// The whole reason paging is worth writing: a truncated walk reads as
	// "no duplicate found", and that answer opens a second pull request
	// beside somebody's first.
	var pages []string
	run := func(_ context.Context, args ...string) (string, error) {
		pages = append(pages, args[1])
		switch len(pages) {
		case 1:
			return fullPage(t, 1, "curl: update to 8.0"), nil
		case 2:
			return fullPage(t, 101, "openssl: update to 3.5"), nil
		}
		return `[{"number":900,"title":"jq: update to 2.5","state":"open","html_url":"https://x/900"}]`, nil
	}
	prs, err := OpenPortPRs(context.Background(), run, "macports/macports-ports", "jq")
	require.NoError(t, err)
	require.Len(t, prs, 1, "the match was on the third page and the walk reached it")
	assert.Equal(t, 900, prs[0].Number)
	assert.Equal(t, []string{
		"repos/macports/macports-ports/pulls?state=open&per_page=100&page=1",
		"repos/macports/macports-ports/pulls?state=open&per_page=100&page=2",
		"repos/macports/macports-ports/pulls?state=open&per_page=100&page=3",
	}, pages)
}

func TestOpenPortPRsRefusesRatherThanAnswerShort(t *testing.T) {
	// A page the forge would not serve, and a walk with no end, are the
	// two ways the answer could be silently incomplete. Both are errors,
	// because the caller that must not guess — the unattended pass —
	// turns an error into a refusal and a short answer into a duplicate.
	broken := func(context.Context, ...string) (string, error) {
		return "", errors.New("gh api: HTTP 502 from api.github.com")
	}
	_, err := OpenPortPRs(context.Background(), broken, "macports/macports-ports", "jq")
	require.Error(t, err)

	endless := func(_ context.Context, args ...string) (string, error) {
		return fullPage(t, 1, "curl: update to 8.0"), nil
	}
	_, err = OpenPortPRs(context.Background(), endless, "macports/macports-ports", "jq")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than 100 pages")

	// A port nobody named searches for nothing, which is a nil answer and
	// not a call.
	var called bool
	_, err = OpenPortPRs(context.Background(), func(context.Context, ...string) (string, error) {
		called = true
		return "[]", nil
	}, "macports/macports-ports", "")
	require.NoError(t, err)
	assert.False(t, called)
}

// fullPage is a page of exactly openPRPageSize pull requests, which is
// what tells the walk there may be another one.
func fullPage(t *testing.T, first int, title string) string {
	t.Helper()
	prs := make([]PullRequest, 0, openPRPageSize)
	for i := 0; i < openPRPageSize; i++ {
		prs = append(prs, PullRequest{Number: first + i, Title: title, State: "open"})
	}
	// Marshalled through a shape that keeps every field, since
	// PullRequest's own MarshalJSON publishes the document rather than
	// what this package reads.
	type wire struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
	}
	out := make([]wire, 0, len(prs))
	for _, pr := range prs {
		out = append(out, wire{pr.Number, pr.Title, pr.State})
	}
	b, err := json.Marshal(out)
	require.NoError(t, err)
	return string(b)
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
	assert.Equal(t, 9, reflect.TypeOf(PullRequest{}).NumField(),
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
