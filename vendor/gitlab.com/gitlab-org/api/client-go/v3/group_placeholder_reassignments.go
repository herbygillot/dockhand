package gitlab

import (
	"io"
	"net/http"
)

var routeGroupsIDPlaceholderReassignments = route("groups/%s/placeholder_reassignments")

type (
	GroupPlaceholderReassignmentsServiceInterface interface {
		// DownloadPendingReassignments retrieves a CSV file with a list of
		// pending placeholder reassignments of a group.
		//
		// The returned io.ReadCloser must be closed by the caller to avoid
		// leaking the underlying response body.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_placeholder_reassignments/#retrieve-pending-reassignments
		DownloadPendingReassignments(gid any, options ...RequestOptionFunc) (io.ReadCloser, *Response, error)
		// ReassignPlaceholders reassigns placeholder users of a group with
		// an uploaded CSV file. The file is processed asynchronously.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/group_placeholder_reassignments/#reassign-placeholders
		ReassignPlaceholders(gid any, content io.Reader, filename string, options ...RequestOptionFunc) (*PlaceholderReassignmentsResult, *Response, error)
	}

	// GroupPlaceholderReassignmentsService handles communication with the
	// group placeholder reassignments related methods of the GitLab API.
	//
	// GitLab API docs:
	// https://docs.gitlab.com/api/group_placeholder_reassignments/
	GroupPlaceholderReassignmentsService struct {
		client *Client
	}
)

var _ GroupPlaceholderReassignmentsServiceInterface = (*GroupPlaceholderReassignmentsService)(nil)

// PlaceholderReassignmentsResult represents the response of a placeholder
// reassignments CSV upload.
//
// GitLab API docs:
// https://docs.gitlab.com/api/group_placeholder_reassignments/#reassign-placeholders
type PlaceholderReassignmentsResult struct {
	Message string `json:"message"`
}

func (s *GroupPlaceholderReassignmentsService) DownloadPendingReassignments(gid any, options ...RequestOptionFunc) (io.ReadCloser, *Response, error) {
	r, resp, err := do[bodyReader](s.client,
		withMethod(http.MethodGet),
		withPath(routeGroupsIDPlaceholderReassignments, GroupID{gid}),
		withRequestOpts(options...),
	)
	if err != nil {
		return nil, resp, err
	}

	return &r, resp, nil
}

func (s *GroupPlaceholderReassignmentsService) ReassignPlaceholders(gid any, content io.Reader, filename string, options ...RequestOptionFunc) (*PlaceholderReassignmentsResult, *Response, error) {
	return do[*PlaceholderReassignmentsResult](s.client,
		withMethod(http.MethodPost),
		withPath(routeGroupsIDPlaceholderReassignments, GroupID{gid}),
		withUpload(content, filename, UploadFile),
		withRequestOpts(options...),
	)
}
