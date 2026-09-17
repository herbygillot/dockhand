package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/record"
)

// Missing runs remain uncertain: current workflow settings cannot prove whether
// an earlier push triggered execution. Recovery keeps the accepted workflow ID.
func missingRunDetail(ctx context.Context, api actionsAPI, row record.ProviderExecution, saved payload, push string) (string, error) {
	spec := saved.Request.Spec
	detail := fmt.Sprintf("No matching GitHub Actions run observed for %s:%s at %s; submission recorded at %s; %s",
		saved.Config.Destination.HeadRepository, spec.PushBranch(), spec.Source.Commit, row.CreatedAt.UTC().Format(time.RFC3339), push)
	workflow, err := api.Workflow(ctx, "main.yml")
	var response *gh.ErrorResponse
	switch {
	case ctx.Err() != nil:
		return detail, ctx.Err()
	case errors.As(err, &response) && response.Response != nil && response.Response.StatusCode == http.StatusNotFound:
		detail += "; main.yml is missing or inaccessible in the fork; check Actions settings and credential access"
	case err != nil:
		return detail, fmt.Errorf("github verification: inspecting missing-run workflow: %w", err)
	case workflow.GetID() != saved.Config.WorkflowID || workflow.GetPath() != WorkflowPath:
		detail += "; main.yml no longer matches the accepted workflow; observation remains bound to the original workflow ID"
	case workflow.GetState() != "active":
		detail += fmt.Sprintf("; main.yml state is %q; enable the workflow in the fork's Actions settings", workflow.GetState())
	default:
		detail += "; main.yml is active; check the fork's Actions page and push-trigger eligibility"
	}
	if workflow.GetHTMLURL() != "" {
		detail += "; " + workflow.GetHTMLURL()
	}
	detail += "; keep observing with dockhand wait <job-id> --trace, or stop tracking with dockhand cancel <job-id> --wait (remote Actions may still run)"
	return detail, nil
}
