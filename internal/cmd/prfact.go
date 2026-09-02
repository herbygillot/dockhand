package cmd

import (
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// prFact maps the forge's answer about a pull request into the fact a
// judgment weighs. The forge's own spellings — a merge timestamp being
// present, a state word reading "open" — are read here, at the boundary
// where its JSON is already being read, so that clean, status and
// promote all reach the same judgment from the same shape and no
// decision has to know what gh prints.
//
// A lookup that found nothing is the zero fact, which is what "no pull
// request" means.
func prFact(pr forge.PullRequest, found bool) verdict.PRFact {
	if !found {
		return verdict.PRFact{}
	}
	return verdict.PRFact{
		Found:  true,
		Number: pr.Number,
		Title:  pr.Title,
		URL:    pr.HTMLURL,
		Merged: pr.MergedAt != "",
		Open:   pr.State == "open",
	}
}

// prFacts maps a list the forge returned. Every entry exists, so every
// one of them is Found.
func prFacts(prs []forge.PullRequest) []verdict.PRFact {
	out := make([]verdict.PRFact, 0, len(prs))
	for _, pr := range prs {
		out = append(out, prFact(pr, true))
	}
	return out
}
