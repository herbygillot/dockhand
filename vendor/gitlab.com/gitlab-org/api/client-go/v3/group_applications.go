//
// Copyright 2026, Miguel Sacristan
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
)

var (
	routeGroupsIDApplications   = route("groups/%s/applications")
	routeGroupsIDApplicationsID = route("groups/%s/applications/%d")
)

type (
	// GroupApplicationsServiceInterface defines all the API methods for the
	// GroupApplicationsService.
	GroupApplicationsServiceInterface interface {
		// CreateGroupApplication creates a new OAuth application owned by the
		// specified group. The secret is only returned by this call.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_applications/#create-a-group-application
		CreateGroupApplication(gid any, opt *CreateGroupApplicationOptions, options ...RequestOptionFunc) (*Application, *Response, error)

		// ListGroupApplications lists all OAuth applications owned by the
		// specified group.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_applications/#list-all-group-applications
		ListGroupApplications(gid any, opt *ListGroupApplicationsOptions, options ...RequestOptionFunc) ([]*Application, *Response, error)

		// GetGroupApplication retrieves a specific OAuth application owned by
		// the specified group.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_applications/#retrieve-a-specific-group-application
		GetGroupApplication(gid any, application int64, options ...RequestOptionFunc) (*Application, *Response, error)

		// UpdateGroupApplication updates an existing OAuth application owned by
		// the specified group. Only the name and scopes can be updated.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_applications/#update-a-group-application
		UpdateGroupApplication(gid any, application int64, opt *UpdateGroupApplicationOptions, options ...RequestOptionFunc) (*Application, *Response, error)

		// DeleteGroupApplication deletes a specific OAuth application owned by
		// the specified group.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_applications/#delete-a-group-application
		DeleteGroupApplication(gid any, application int64, options ...RequestOptionFunc) (*Response, error)
	}

	// GroupApplicationsService handles communication with the group-owned
	// OAuth applications of the GitLab API.
	//
	// GitLab API docs: https://docs.gitlab.com/api/group_applications/
	GroupApplicationsService struct {
		client *Client
	}
)

var _ GroupApplicationsServiceInterface = (*GroupApplicationsService)(nil)

// CreateGroupApplicationOptions represents the available
// CreateGroupApplication() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/group_applications/#create-a-group-application
type CreateGroupApplicationOptions struct {
	Name         *string `url:"name,omitempty" json:"name,omitempty"`
	RedirectURI  *string `url:"redirect_uri,omitempty" json:"redirect_uri,omitempty"`
	Scopes       *string `url:"scopes,omitempty" json:"scopes,omitempty"`
	Confidential *bool   `url:"confidential,omitempty" json:"confidential,omitempty"`
}

func (s *GroupApplicationsService) CreateGroupApplication(gid any, opt *CreateGroupApplicationOptions, options ...RequestOptionFunc) (*Application, *Response, error) {
	return do[*Application](s.client,
		withMethod(http.MethodPost),
		withPath(routeGroupsIDApplications, GroupID{gid}),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

// ListGroupApplicationsOptions represents the available
// ListGroupApplications() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/group_applications/#list-all-group-applications
type ListGroupApplicationsOptions struct {
	ListOptions
}

func (s *GroupApplicationsService) ListGroupApplications(gid any, opt *ListGroupApplicationsOptions, options ...RequestOptionFunc) ([]*Application, *Response, error) {
	return do[[]*Application](s.client,
		withMethod(http.MethodGet),
		withPath(routeGroupsIDApplications, GroupID{gid}),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

func (s *GroupApplicationsService) GetGroupApplication(gid any, application int64, options ...RequestOptionFunc) (*Application, *Response, error) {
	return do[*Application](s.client,
		withMethod(http.MethodGet),
		withPath(routeGroupsIDApplicationsID, GroupID{gid}, application),
		withRequestOpts(options...),
	)
}

// UpdateGroupApplicationOptions represents the available
// UpdateGroupApplication() options. The redirect URI and confidential flag of
// an existing application cannot be updated.
//
// GitLab API docs:
// https://docs.gitlab.com/api/group_applications/#update-a-group-application
type UpdateGroupApplicationOptions struct {
	Name   *string `url:"name,omitempty" json:"name,omitempty"`
	Scopes *string `url:"scopes,omitempty" json:"scopes,omitempty"`
}

func (s *GroupApplicationsService) UpdateGroupApplication(gid any, application int64, opt *UpdateGroupApplicationOptions, options ...RequestOptionFunc) (*Application, *Response, error) {
	return do[*Application](s.client,
		withMethod(http.MethodPut),
		withPath(routeGroupsIDApplicationsID, GroupID{gid}, application),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

func (s *GroupApplicationsService) DeleteGroupApplication(gid any, application int64, options ...RequestOptionFunc) (*Response, error) {
	_, resp, err := do[none](s.client,
		withMethod(http.MethodDelete),
		withPath(routeGroupsIDApplicationsID, GroupID{gid}, application),
		withRequestOpts(options...),
	)
	return resp, err
}
