package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// The person's voice (Then's item 13, the direction synthesis's B): a
// decision on one concern, with its reason, which lapses when its port's
// own files change. "Acceptable because…" lets the concern go for a
// submission nobody looks over, still said, and carries the reason into
// the pull request; "awaiting…" blocks the submission until decided
// otherwise.

// DecideRequest is a decision asked of a branch's concern: the rule that
// raised it, the port it's about, the branch's one port where empty, the
// answer, and the person's words for it.
type DecideRequest struct {
	Branch model.Branch
	Rule   string
	Port   string
	Answer model.DecisionAnswer
	Words  string
}

// Decide records a decision on a concern of the branch's files as they
// are, its evidence the port's directory there, so a change to the port's
// Portfile or files lapses it and a change to another port's doesn't (the
// prioritization thread's word, 2026-10-27). A decision made again on the
// same evidence replaces it. One on a moving source names the commits of
// the concern status found, so a later move is a new concern
// (model.MovingSource).
func (e *Engine) Decide(ctx context.Context, request DecideRequest) (model.Decision, error) {
	if !slices.Contains(HoldRules(), request.Rule) {
		return model.Decision{}, fmt.Errorf("%q is not a hold dockhand raises; the rules are %s", request.Rule, strings.Join(HoldRules(), ", "))
	}
	if strings.TrimSpace(request.Words) == "" {
		return model.Decision{}, fmt.Errorf("a decision says why: --because \"<reason>\" or --waiting \"<what for>\"")
	}
	status, err := e.BranchStatus(ctx, request.Branch)
	if err != nil {
		return model.Decision{}, err
	}
	port := request.Port
	if port == "" {
		names := status.Scope.PortNames()
		if len(names) != 1 {
			return model.Decision{}, fmt.Errorf("%s changes %s; name the port after the rule", request.Branch.ShortName(), map[bool]string{true: "no port", false: strings.Join(names, ", ")}[len(names) == 0])
		}
		port = names[0]
	}
	evidence := e.portEvidence(ctx, status.Tree, port)
	if evidence == "" {
		return model.Decision{}, fmt.Errorf("%s has no port %s in its files", request.Branch.ShortName(), port)
	}
	subject := model.ConcernSubject(model.Concern{Port: port, Rule: request.Rule})
	if model.MovingSource(request.Rule) {
		if subject = movedSubject(status, port, request.Rule); subject == "" {
			return model.Decision{}, fmt.Errorf("no %s of %s stands on %s: a decision on a moving source answers the commits a submission's plan found, which status lists among what holds it", request.Rule, port, request.Branch.ShortName())
		}
	}
	decision := model.Decision{Branch: request.Branch.ID, Finding: model.FindingConcern, Subject: subject,
		Evidence: evidence, Answer: request.Answer, Words: strings.TrimSpace(request.Words), At: e.now()}
	return decision, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.AddDecision(decision) })
}

// movedSubject is the subject of the branch's concern of a moving source,
// with its commits, as status found it: the one that holds, else the
// latest a standing decision answers; empty where there's none.
func movedSubject(status BranchStatus, port, rule string) string {
	for _, held := range slices.Concat(status.Held, status.Moved) {
		if held.Port == port && held.Rule == rule {
			return model.ConcernSubject(held)
		}
	}
	prefix := model.ConcernSubject(model.Concern{Port: port, Rule: rule}) + " "
	var latest model.Decision
	for _, decision := range status.Decided {
		if strings.HasPrefix(decision.Subject, prefix) && !decision.At.Before(latest.At) {
			latest = decision
		}
	}
	return latest.Subject
}

// portEvidence is what a decision on a port's concern rests on: the tree
// object of the port's directory in tree; empty where tree has none.
func (e *Engine) portEvidence(ctx context.Context, tree, port string) string {
	directory, err := e.findDirectory(ctx, tree, port)
	if err != nil {
		return ""
	}
	objects, err := e.Repo.Directories(ctx, tree, []string{directory})
	if err != nil {
		return ""
	}
	return objects[directory]
}

// decided are the decisions on a branch's concerns that stand for its
// files at tree, by their subject: each whose port's directory is still
// the one it was decided on, the latest of each.
func (e *Engine) decided(ctx context.Context, branch model.BranchID, tree string) map[string]model.Decision {
	var all []model.Decision
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		all, err = r.BranchDecisions(branch, model.FindingConcern)
		return err
	}); err != nil || len(all) == 0 {
		return nil
	}
	stands := map[string]model.Decision{}
	evidence := map[string]string{}
	for _, decision := range all {
		port, _, _ := strings.Cut(decision.Subject, " ")
		if _, read := evidence[port]; !read {
			evidence[port] = e.portEvidence(ctx, tree, port)
		}
		if decision.Evidence == evidence[port] && !decision.At.Before(stands[decision.Subject].At) {
			stands[decision.Subject] = decision
		}
	}
	return stands
}

// undecided are the concerns no standing acceptance answers, which hold;
// one awaiting something still holds.
func undecided(concerns []model.Concern, decided map[string]model.Decision) []model.Concern {
	return slices.DeleteFunc(slices.Clone(concerns), func(c model.Concern) bool {
		return c.Rule != "" && decided[model.ConcernSubject(c)].Answer == model.Accepted
	})
}

// DecisionWords are a decision on a concern as a person reads it: "jq
// license-changed: acceptable because the new license is still MIT".
func DecisionWords(d model.Decision) string {
	if d.Answer == model.Waiting {
		return d.Subject + ": awaiting " + d.Words
	}
	return d.Subject + ": acceptable because " + d.Words
}

// DecideCommand is the command that answers a concern, as where it holds
// says it: dockhand decide license-changed -p jq --because "…".
func DecideCommand(c model.Concern) string {
	return fmt.Sprintf("dockhand decide %s -p %s --because \"…\"", c.Rule, c.Port)
}
