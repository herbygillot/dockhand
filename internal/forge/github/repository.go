package github

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

const publicInstance = "https://github.com"

// pageSize is how many tags or releases a page of a listing holds: the most
// GitHub's API gives, where it would give 30 unasked. A project with a
// thousand tags is read in ten requests rather than thirty-four.
const pageSize = 100

// A repository is bound for one question, such as one port's newest
// release, and remembers the tags it has read for as long: a tag asked for
// twice, as the newest release and again to date it, is read once.
type repository struct {
	client *Client
	name   string

	mu   sync.Mutex
	tags map[string]forge.Tag
	// answered is the name GitHub answered a listing as, from the URLs it
	// gave (AnswersAs); unread is true where its tags came from its git
	// service, which answers a moved repository without saying so, and
	// no listing of the API's has named it since (ConfirmName).
	answered string
	unread   bool
}

// ConfirmName reads the name GitHub answers the repository by, one request
// of the API's, where its tags came from git, which serves a moved
// repository under its old name: acorn-io/acorn listed obot-platform/obot's
// tags over git with no word of the move. A listing of the API's has said
// it already otherwise.
func (r *repository) ConfirmName(ctx context.Context) error {
	r.mu.Lock()
	unread := r.unread && r.answered == ""
	r.mu.Unlock()
	if !unread {
		return nil
	}
	described, err := r.Describe(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unread = false
	if r.answered == "" && githubapi.ValidRepositoryName(described.Name) {
		r.answered = described.Name
	}
	return nil
}

// AnswersAs is the owner/name GitHub answered the repository's tags or
// releases as, where that isn't the name asked: acorn-io/acorn's listing
// answered from obot-platform/obot, another project (field testing's
// outdated-ports session).
func (r *repository) AnswersAs() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.EqualFold(r.answered, r.name) {
		return ""
	}
	return r.answered
}

// answer notes the owner/name a URL GitHub gave names, the first time:
// https://github.com/<owner>/<name>/… or https://api.github.com/repos/<owner>/<name>/….
func (r *repository) answer(url string) {
	rest, ok := strings.CutPrefix(url, "https://api.github.com/repos/")
	if !ok {
		if rest, ok = strings.CutPrefix(url, "https://github.com/"); !ok {
			return
		}
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || !githubapi.ValidRepositoryName(parts[0]+"/"+parts[1]) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.answered == "" {
		r.answered = parts[0] + "/" + parts[1]
	}
}

// Repository validates a GitHub owner/name without making a network request.
func (c *Client) Repository(instance, name string) (forge.Repository, error) {
	if c == nil || instance != publicInstance || !githubapi.ValidRepositoryName(name) {
		return nil, fmt.Errorf("github: invalid repository %q", name)
	}
	return &repository{client: c, name: name}, nil
}

func (r *repository) Name() string { return r.name }

// cloneURL is where git reads the repository: the configured clone base, or
// GitHub itself.
func (r *repository) cloneURL() string {
	base := publicInstance
	if r.client.Client != nil && r.client.Config.CloneURL != "" {
		base = strings.TrimSuffix(r.client.Config.CloneURL, "/")
	}
	return base + "/" + r.name + ".git"
}
