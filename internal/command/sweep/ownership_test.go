package sweep

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
)

// ownership holds each write in the effect log to the first three lines
// of the rule on remote writes (remotewrite's rule, #144), read from the
// stand-in world's own records and the database at the write, never from
// the gate's table: the sweep is a check on the gate (docs/designs/
// fault-sweep.md, invariant 5; #129's fix list, 1). A write made and then
// undone shows here, where the final world alone would miss it.
type ownership struct {
	github   *forgetest.GitHub
	database string
}

// push is what a git push breaks of the rule, by its destination and its
// refspecs.
func (o ownership) push(args []string) []string {
	at := slices.Index(args, "--")
	if at < 0 || at+1 >= len(args) {
		return nil
	}
	destination, refspecs := args[at+1], args[at+2:]
	deleting := slices.Contains(args[:at], "--delete")
	repository, err := o.github.NameFromRemote(destination)
	if err != nil {
		repository = destination
	}
	var broken []string
	for _, refspec := range refspecs {
		source, ref, _ := strings.Cut(strings.TrimPrefix(refspec, "+"), ":")
		if ref == "" {
			ref = source
		}
		deletes := deleting || source == ""
		branch := strings.TrimPrefix(ref, "refs/heads/")
		switch {
		case strings.EqualFold(repository, forgetest.UpstreamRepository):
			broken = append(broken, fmt.Sprintf("rule 1: a push to MacPorts' repository changes %s", ref))
		case !strings.EqualFold(repository, forgetest.ForkRepository) && deletes:
			broken = append(broken, fmt.Sprintf("rule 2: a push deletes %s in %s, another person's repository", ref, repository))
		case !strings.EqualFold(repository, forgetest.ForkRepository) && !o.invited(repository, branch):
			broken = append(broken, fmt.Sprintf("rule 2: a push changes %s in %s, which no pull request of theirs with maintainer edits on has as its head", ref, repository))
		case deletes && !o.made(repository, branch):
			broken = append(broken, fmt.Sprintf("rule 3: a push deletes %s in %s, which no record says dockhand made", ref, repository))
		}
	}
	return broken
}

// invited reports whether a branch of another person's repository is the
// head of a pull request they opened with maintainer edits on.
func (o ownership) invited(repository, branch string) bool {
	for _, pr := range o.github.Theirs {
		if strings.EqualFold(pr.HeadRepository, repository) && pr.HeadBranch == branch && pr.MaintainerCanModify {
			return true
		}
	}
	return false
}

// made reports whether the database records dockhand making a branch: a
// branch's pull request it pushed, not one it adopted.
func (o ownership) made(repository, branch string) bool {
	db, err := sql.Open("sqlite", o.database)
	if err != nil {
		return false
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT count(*) FROM branches WHERE pr_head = ? AND pr_adopted = 0`, repository+":"+branch).Scan(&n)
	return err == nil && n > 0
}

// workflowVerb names what each workflow write does to a run.
var workflowVerb = map[string]string{"RerunFailed": "run again", "Cancel": "canceled"}

// forge is what a forge write breaks of the rule: a workflow run run
// again or canceled anywhere but the person's own fork, MacPorts'
// repository by its first line and another person's by its second.
func (o ownership) forge(name, repository string) []string {
	verb, workflow := workflowVerb[name]
	switch {
	case !workflow || strings.EqualFold(repository, forgetest.ForkRepository):
		return nil
	case strings.EqualFold(repository, forgetest.UpstreamRepository):
		return []string{"rule 1: a workflow run in MacPorts' repository is " + verb}
	default:
		return []string{fmt.Sprintf("rule 2: a workflow run in %s, another person's repository, is %s", repository, verb)}
	}
}

// Ownership is read from the stand-in world's records: MacPorts'
// repository takes no push; another person's only a push to the head of a
// pull request they opened with maintainer edits on, never a deletion;
// and a branch is deleted only where a record says dockhand made it
// (#129's fix list, 1).
func TestOwnershipIsReadFromTheWorldsRecords(t *testing.T) {
	t.Parallel()
	w, g := updated(t)
	g.Repos = map[string]string{"grace/macports-ports": "/repos/grace.git"}
	g.Theirs = map[int]forge.PullRequest{99: {HeadRepository: "grace/macports-ports", HeadBranch: "jq", MaintainerCanModify: true},
		98: {HeadRepository: "grace/macports-ports", HeadBranch: "curl"}}
	o := ownership{github: g, database: w.Home + "/.dockhand/dockhand.db"}
	push := func(destination string, refspecs ...string) []string {
		return o.push(append([]string{"push", "--porcelain", "--", destination}, refspecs...))
	}
	require.Len(t, push(w.Upstream, "abc:refs/heads/master"), 1, "MacPorts' repository")
	require.Empty(t, push("/repos/grace.git", "abc:refs/heads/jq"), "an invited head")
	require.Len(t, push("/repos/grace.git", "abc:refs/heads/curl"), 1, "a head without maintainer edits")
	require.Len(t, push("/repos/grace.git", ":refs/heads/jq"), 1, "nothing deleted in another person's repository")
	fork := g.Fork
	require.Empty(t, push(fork, "abc:refs/heads/dockhand/jq-update"), "the person's fork")
	require.Len(t, push(fork, ":refs/heads/dockhand/jq-update"), 1, "a deletion no record says dockhand made: the branch has no pull request yet")
	require.Len(t, o.forge("RerunFailed", forgetest.UpstreamRepository), 1)
	require.Empty(t, o.forge("Create", forgetest.UpstreamRepository), "pull requests are opened there")

	// Once dockhand has opened the branch's pull request, its record says
	// dockhand made the branch.
	opened := cmdtest.NewWorld(t)
	g = submitted(t, opened)
	o = ownership{github: g, database: opened.Home + "/.dockhand/dockhand.db"}
	require.Empty(t, o.push(append([]string{"push", "--porcelain", "--"}, g.Fork, ":refs/heads/dockhand/jq-update")), "a branch dockhand made")
}

// A workflow run is run again or canceled only in the person's own fork:
// in MacPorts' repository that breaks the rule's first line, and in
// another person's, where only a push to an invited head is allowed, its
// second (#129's second round, 1).
func TestConfirm129OwnershipForeignWorkflow(t *testing.T) {
	t.Parallel()
	o := ownership{github: &forgetest.GitHub{}}
	for _, write := range []string{"RerunFailed", "Cancel"} {
		require.Equal(t, []string{"rule 1: a workflow run in MacPorts' repository is " + workflowVerb[write]}, o.forge(write, forgetest.UpstreamRepository), write)
		require.Equal(t, []string{"rule 2: a workflow run in grace/macports-ports, another person's repository, is " + workflowVerb[write]}, o.forge(write, "grace/macports-ports"), write)
		require.Empty(t, o.forge(write, forgetest.ForkRepository), "%s in the person's fork", write)
	}
	require.Empty(t, o.forge("Create", "grace/macports-ports"), "not a workflow write")
}
