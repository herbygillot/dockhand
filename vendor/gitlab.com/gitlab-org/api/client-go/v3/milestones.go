//
// Copyright 2021, Sander van Harmelen
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package gitlab

import (
	"net/http"
	"time"
)

var (
	routeProjectsIDMilestones                = route("projects/%s/milestones")
	routeProjectsIDMilestonesID              = route("projects/%s/milestones/%d")
	routeProjectsIDMilestonesIDBurndown      = route("projects/%s/milestones/%d/burndown_events")
	routeProjectsIDMilestonesIDIssues        = route("projects/%s/milestones/%d/issues")
	routeProjectsIDMilestonesIDMergeRequests = route("projects/%s/milestones/%d/merge_requests")
	routeProjectsIDMilestonesIDPromote       = route("projects/%s/milestones/%d/promote")
)

type (
	MilestonesServiceInterface interface {
		// ListMilestones returns a list of project milestones.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#list-all-project-milestones
		ListMilestones(pid any, opt *ListMilestonesOptions, options ...RequestOptionFunc) ([]*Milestone, *Response, error)
		// GetMilestone gets a single project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#retrieve-a-milestone
		GetMilestone(pid any, milestone int64, options ...RequestOptionFunc) (*Milestone, *Response, error)
		// CreateMilestone creates a new project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#create-a-milestone
		CreateMilestone(pid any, opt *CreateMilestoneOptions, options ...RequestOptionFunc) (*Milestone, *Response, error)
		// UpdateMilestone updates an existing project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#update-a-milestone
		UpdateMilestone(pid any, milestone int64, opt *UpdateMilestoneOptions, options ...RequestOptionFunc) (*Milestone, *Response, error)
		// DeleteMilestone deletes a specified project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#delete-a-milestone
		DeleteMilestone(pid any, milestone int64, options ...RequestOptionFunc) (*Response, error)
		// GetMilestoneIssues gets all issues assigned to a single project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#list-all-issues-for-a-milestone
		GetMilestoneIssues(pid any, milestone int64, opt *GetMilestoneIssuesOptions, options ...RequestOptionFunc) ([]*Issue, *Response, error)
		// GetMilestoneMergeRequests gets all merge requests assigned to a single
		// project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#list-all-merge-requests-for-a-milestone
		GetMilestoneMergeRequests(pid any, milestone int64, opt *GetMilestoneMergeRequestsOptions, options ...RequestOptionFunc) ([]*BasicMergeRequest, *Response, error)
		// PromoteMilestone promotes a project milestone to a group milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#promote-a-milestone-to-group-milestone
		PromoteMilestone(pid any, milestone int64, options ...RequestOptionFunc) (*Response, error)
		// GetMilestoneBurndownChartEvents lists all burndown chart events for a
		// single project milestone.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/milestones/#list-all-burndown-chart-events-for-a-milestone
		GetMilestoneBurndownChartEvents(pid any, milestone int64, opt *GetMilestoneBurndownChartEventsOptions, options ...RequestOptionFunc) ([]*BurndownChartEvent, *Response, error)
	}

	// MilestonesService handles communication with the milestone related methods
	// of the GitLab API.
	//
	// GitLab API docs: https://docs.gitlab.com/api/milestones/
	MilestonesService struct {
		client *Client
	}
)

var _ MilestonesServiceInterface = (*MilestonesService)(nil)

// Milestone represents a GitLab milestone.
//
// GitLab API docs: https://docs.gitlab.com/api/milestones/
type Milestone struct {
	ID          int64      `json:"id"`
	IID         int64      `json:"iid"`
	GroupID     int64      `json:"group_id"`
	ProjectID   int64      `json:"project_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	StartDate   *ISOTime   `json:"start_date"`
	DueDate     *ISOTime   `json:"due_date"`
	State       string     `json:"state"`
	WebURL      string     `json:"web_url"`
	UpdatedAt   *time.Time `json:"updated_at"`
	CreatedAt   *time.Time `json:"created_at"`
	Expired     *bool      `json:"expired"`
}

func (m Milestone) String() string {
	return Stringify(m)
}

// ListMilestonesOptions represents the available ListMilestones() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/milestones/#list-all-project-milestones
type ListMilestonesOptions struct {
	ListOptions
	IIDs             *[]int64 `url:"iids[],omitempty" json:"iids,omitempty"`
	Title            *string  `url:"title,omitempty" json:"title,omitempty"`
	State            *string  `url:"state,omitempty" json:"state,omitempty"`
	Search           *string  `url:"search,omitempty" json:"search,omitempty"`
	IncludeAncestors *bool    `url:"include_ancestors,omitempty" json:"include_ancestors,omitempty"`

	// Deprecated: in GitLab 16,7, use IncludeAncestors instead
	IncludeParentMilestones *bool `url:"include_parent_milestones,omitempty" json:"include_parent_milestones,omitempty"`
}

func (s *MilestonesService) ListMilestones(pid any, opt *ListMilestonesOptions, options ...RequestOptionFunc) ([]*Milestone, *Response, error) {
	return do[[]*Milestone](s.client,
		withPath(routeProjectsIDMilestones, ProjectID{pid}),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

func (s *MilestonesService) GetMilestone(pid any, milestone int64, options ...RequestOptionFunc) (*Milestone, *Response, error) {
	return do[*Milestone](s.client,
		withPath(routeProjectsIDMilestonesID, ProjectID{pid}, milestone),
		withRequestOpts(options...),
	)
}

// CreateMilestoneOptions represents the available CreateMilestone() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/milestones/#create-a-milestone
type CreateMilestoneOptions struct {
	Title       *string  `url:"title,omitempty" json:"title,omitempty"`
	Description *string  `url:"description,omitempty" json:"description,omitempty"`
	StartDate   *ISOTime `url:"start_date,omitempty" json:"start_date,omitempty"`
	DueDate     *ISOTime `url:"due_date,omitempty" json:"due_date,omitempty"`
}

func (s *MilestonesService) CreateMilestone(pid any, opt *CreateMilestoneOptions, options ...RequestOptionFunc) (*Milestone, *Response, error) {
	return do[*Milestone](s.client,
		withMethod(http.MethodPost),
		withPath(routeProjectsIDMilestones, ProjectID{pid}),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

// UpdateMilestoneOptions represents the available UpdateMilestone() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/milestones/#update-a-milestone
type UpdateMilestoneOptions struct {
	Title       *string  `url:"title,omitempty" json:"title,omitempty"`
	Description *string  `url:"description,omitempty" json:"description,omitempty"`
	StartDate   *ISOTime `url:"start_date,omitempty" json:"start_date,omitempty"`
	DueDate     *ISOTime `url:"due_date,omitempty" json:"due_date,omitempty"`
	StateEvent  *string  `url:"state_event,omitempty" json:"state_event,omitempty"`
}

func (s *MilestonesService) UpdateMilestone(pid any, milestone int64, opt *UpdateMilestoneOptions, options ...RequestOptionFunc) (*Milestone, *Response, error) {
	return do[*Milestone](s.client,
		withMethod(http.MethodPut),
		withPath(routeProjectsIDMilestonesID, ProjectID{pid}, milestone),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

func (s *MilestonesService) DeleteMilestone(pid any, milestone int64, options ...RequestOptionFunc) (*Response, error) {
	_, resp, err := do[none](s.client,
		withMethod(http.MethodDelete),
		withPath(routeProjectsIDMilestonesID, ProjectID{pid}, milestone),
		withRequestOpts(options...),
	)
	return resp, err
}

// GetMilestoneIssuesOptions represents the available GetMilestoneIssues() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/milestones/#list-all-issues-for-a-milestone
type GetMilestoneIssuesOptions struct {
	ListOptions
}

func (s *MilestonesService) GetMilestoneIssues(pid any, milestone int64, opt *GetMilestoneIssuesOptions, options ...RequestOptionFunc) ([]*Issue, *Response, error) {
	return do[[]*Issue](s.client,
		withPath(routeProjectsIDMilestonesIDIssues, ProjectID{pid}, milestone),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

// GetMilestoneMergeRequestsOptions represents the available
// GetMilestoneMergeRequests() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/milestones/#list-all-merge-requests-for-a-milestone
type GetMilestoneMergeRequestsOptions struct {
	ListOptions
}

func (s *MilestonesService) GetMilestoneMergeRequests(pid any, milestone int64, opt *GetMilestoneMergeRequestsOptions, options ...RequestOptionFunc) ([]*BasicMergeRequest, *Response, error) {
	return do[[]*BasicMergeRequest](s.client,
		withPath(routeProjectsIDMilestonesIDMergeRequests, ProjectID{pid}, milestone),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

func (s *MilestonesService) PromoteMilestone(pid any, milestone int64, options ...RequestOptionFunc) (*Response, error) {
	_, resp, err := do[none](s.client,
		withMethod(http.MethodPost),
		withPath(routeProjectsIDMilestonesIDPromote, ProjectID{pid}, milestone),
		withRequestOpts(options...),
	)
	return resp, err
}

// GetMilestoneBurndownChartEventsOptions represents the available
// GetMilestoneBurndownChartEvents() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/milestones/#list-all-burndown-chart-events-for-a-milestone
type GetMilestoneBurndownChartEventsOptions struct {
	ListOptions
}

func (s *MilestonesService) GetMilestoneBurndownChartEvents(pid any, milestone int64, opt *GetMilestoneBurndownChartEventsOptions, options ...RequestOptionFunc) ([]*BurndownChartEvent, *Response, error) {
	return do[[]*BurndownChartEvent](s.client,
		withPath(routeProjectsIDMilestonesIDBurndown, ProjectID{pid}, milestone),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}
