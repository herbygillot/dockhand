// EXPERIMENTAL(#2213): The Work Items REST API is a work in progress and may introduce breaking changes even between minor versions.
//
// The work items REST API is behind the work_item_rest_api feature flag and
// returns 404 while the flag is disabled.
//
// GitLab has not published documentation for the work items REST API yet.
// Replace the "not yet published" API docs references once it does.

package gitlab

import "time"

var routeNamespacesIDWorkItemsID = route("namespaces/%s/-/work_items/%d")

type (
	// WorkItemsRESTServiceInterface is commented out while work on the work
	// items REST API is on hold, so the service isn't offered as part of the
	// client. To restore it, uncomment it and Client.WorkItemsREST, and export
	// workItemsRESTService again as WorkItemsRESTService. See
	// https://gitlab.com/gitlab-org/api/client-go/-/merge_requests/3078.
	//
	// // WorkItemsRESTServiceInterface defines the work item methods that use the
	// // work items REST API. The GraphQL-based methods are in
	// // WorkItemsServiceInterface.
	// //
	// // Experimental: The Work Items REST API is a work in progress and may
	// // introduce breaking changes even between minor versions.
	// WorkItemsRESTServiceInterface interface {
	// 	// GetWorkItem gets a single work item in a project or group namespace.
	// 	//
	// 	// GitLab API docs: not yet published
	// 	GetWorkItem(nid any, iid int64, opt *GetWorkItemRESTOptions, options ...RequestOptionFunc) (*WorkItemREST, *Response, error)
	// }

	// workItemsRESTService handles communication with the work items REST API.
	// The API is behind the work_item_rest_api feature flag, and returns 404
	// while the flag is disabled.
	//
	// GitLab API docs: not yet published
	//
	// Experimental: The Work Items REST API is a work in progress and may
	// introduce breaking changes even between minor versions.
	workItemsRESTService struct {
		client *Client
	}
)

// var _ WorkItemsRESTServiceInterface = (*WorkItemsRESTService)(nil)

// WorkItemFieldValue represents a work item field that can be requested with
// GetWorkItemRESTOptions.Fields.
//
// GitLab API docs: not yet published
type WorkItemFieldValue string

// These constants represent the work item fields that WorkItemREST models.
// ID, IID, GlobalID and Title are always returned and need no constant.
const (
	WorkItemFieldTitleHTML               WorkItemFieldValue = "title_html"
	WorkItemFieldState                   WorkItemFieldValue = "state"
	WorkItemFieldConfidential            WorkItemFieldValue = "confidential"
	WorkItemFieldImported                WorkItemFieldValue = "imported"
	WorkItemFieldHidden                  WorkItemFieldValue = "hidden"
	WorkItemFieldLockVersion             WorkItemFieldValue = "lock_version"
	WorkItemFieldCreatedAt               WorkItemFieldValue = "created_at"
	WorkItemFieldUpdatedAt               WorkItemFieldValue = "updated_at"
	WorkItemFieldClosedAt                WorkItemFieldValue = "closed_at"
	WorkItemFieldAuthor                  WorkItemFieldValue = "author"
	WorkItemFieldWorkItemType            WorkItemFieldValue = "work_item_type"
	WorkItemFieldNamespace               WorkItemFieldValue = "namespace"
	WorkItemFieldReference               WorkItemFieldValue = "reference"
	WorkItemFieldWebURL                  WorkItemFieldValue = "web_url"
	WorkItemFieldWebPath                 WorkItemFieldValue = "web_path"
	WorkItemFieldCreateNoteEmail         WorkItemFieldValue = "create_note_email"
	WorkItemFieldDuplicatedToWorkItemURL WorkItemFieldValue = "duplicated_to_work_item_url"
	WorkItemFieldMovedToWorkItemURL      WorkItemFieldValue = "moved_to_work_item_url"
	WorkItemFieldUserPermissions         WorkItemFieldValue = "user_permissions"
	WorkItemFieldUserDiscussionsCount    WorkItemFieldValue = "user_discussions_count"
)

// AllWorkItemFields returns every work item field that WorkItemREST models,
// for callers that want the full work item rather than choosing fields.
//
// Every field it returns is available on GitLab Community Edition. Keep it
// that way, so the result is safe to request from any GitLab instance.
func AllWorkItemFields() []WorkItemFieldValue {
	return []WorkItemFieldValue{
		WorkItemFieldTitleHTML,
		WorkItemFieldState,
		WorkItemFieldConfidential,
		WorkItemFieldImported,
		WorkItemFieldHidden,
		WorkItemFieldLockVersion,
		WorkItemFieldCreatedAt,
		WorkItemFieldUpdatedAt,
		WorkItemFieldClosedAt,
		WorkItemFieldAuthor,
		WorkItemFieldWorkItemType,
		WorkItemFieldNamespace,
		WorkItemFieldReference,
		WorkItemFieldWebURL,
		WorkItemFieldWebPath,
		WorkItemFieldCreateNoteEmail,
		WorkItemFieldDuplicatedToWorkItemURL,
		WorkItemFieldMovedToWorkItemURL,
		WorkItemFieldUserPermissions,
		WorkItemFieldUserDiscussionsCount,
	}
}

// WorkItemFeatureValue represents a work item feature payload that can be
// requested with GetWorkItemRESTOptions.Features.
//
// GitLab API docs: not yet published
type WorkItemFeatureValue string

// These constants represent the work item features that WorkItemRESTFeatures
// models.
const (
	WorkItemFeatureAssignees       WorkItemFeatureValue = "assignees"
	WorkItemFeatureDescription     WorkItemFeatureValue = "description"
	WorkItemFeatureHierarchy       WorkItemFeatureValue = "hierarchy"
	WorkItemFeatureLabels          WorkItemFeatureValue = "labels"
	WorkItemFeatureMilestone       WorkItemFeatureValue = "milestone"
	WorkItemFeatureStartAndDueDate WorkItemFeatureValue = "start_and_due_date"
)

// AllWorkItemFeatures returns every work item feature that
// WorkItemRESTFeatures models, for callers that want the full work item
// rather than choosing features.
//
// Every feature it returns is available on GitLab Community Edition. Keep it
// that way, so the result is safe to request from any GitLab instance.
func AllWorkItemFeatures() []WorkItemFeatureValue {
	return []WorkItemFeatureValue{
		WorkItemFeatureAssignees,
		WorkItemFeatureDescription,
		WorkItemFeatureHierarchy,
		WorkItemFeatureLabels,
		WorkItemFeatureMilestone,
		WorkItemFeatureStartAndDueDate,
	}
}

// WorkItemREST represents a work item returned by the work items REST API.
//
// The API always returns ID, IID, GlobalID and Title. Every other field is
// returned only when requested with GetWorkItemRESTOptions.Fields, and
// Features only when requested with GetWorkItemRESTOptions.Features. A field
// that was not returned keeps its zero value.
//
// Most fields are plain values, because their zero value can't be mistaken
// for real data: a returned work item never has an empty State or WebURL.
// Fields whose zero value is also a valid answer, such as Confidential (false)
// or UserDiscussionsCount (0), are pointers, so nil means the field was not
// returned.
//
// Some fields can be null even when requested, and then look the same as a
// field that was not returned: ClosedAt (the work item is open),
// CreateNoteEmail (email replies are unavailable), DuplicatedToWorkItemURL
// (not a duplicate), MovedToWorkItemURL (not moved) and Features.Milestone
// (no milestone).
//
// The API ignores field and feature names it does not recognize, so a field
// that the GitLab instance does not support is not returned rather than
// causing an error.
//
// Fields that were not returned are omitted when the struct is encoded as
// JSON, so encoded output contains only what the API returned.
//
// GitLab API docs: not yet published
type WorkItemREST struct {
	ID        int64  `json:"id"`
	IID       int64  `json:"iid"`
	GlobalID  string `json:"global_id"`
	Title     string `json:"title"`
	TitleHTML string `json:"title_html,omitzero"`
	State     string `json:"state,omitzero"`
	// Confidential is a pointer because false is a valid value. It is nil
	// when the field was not requested.
	Confidential *bool `json:"confidential,omitzero"`
	// Imported is a pointer because false is a valid value. It is nil when
	// the field was not requested.
	Imported *bool `json:"imported,omitzero"`
	// Hidden is a pointer because false is a valid value. It is nil when the
	// field was not requested.
	Hidden *bool `json:"hidden,omitzero"`
	// LockVersion is a pointer because 0 is a valid value. It is nil when the
	// field was not requested.
	LockVersion *int64     `json:"lock_version,omitzero"`
	CreatedAt   *time.Time `json:"created_at,omitzero"`
	UpdatedAt   *time.Time `json:"updated_at,omitzero"`
	// ClosedAt is nil when the field was not requested, and also when the
	// work item is open.
	ClosedAt     *time.Time             `json:"closed_at,omitzero"`
	Author       *WorkItemRESTUser      `json:"author,omitzero"`
	WorkItemType *WorkItemRESTType      `json:"work_item_type,omitzero"`
	Namespace    *WorkItemRESTNamespace `json:"namespace,omitzero"`
	Reference    string                 `json:"reference,omitzero"`
	WebURL       string                 `json:"web_url,omitzero"`
	WebPath      string                 `json:"web_path,omitzero"`
	// CreateNoteEmail is empty when the field was not requested, and also
	// when the user can't reply to the work item by email.
	CreateNoteEmail string `json:"create_note_email,omitzero"`
	// DuplicatedToWorkItemURL is empty when the field was not requested, and
	// also when the work item is not a duplicate.
	DuplicatedToWorkItemURL string `json:"duplicated_to_work_item_url,omitzero"`
	// MovedToWorkItemURL is empty when the field was not requested, and also
	// when the work item has not been moved.
	MovedToWorkItemURL string          `json:"moved_to_work_item_url,omitzero"`
	UserPermissions    map[string]bool `json:"user_permissions,omitzero"`
	// UserDiscussionsCount is a pointer because 0 is a valid value. It is nil
	// when the field was not requested.
	UserDiscussionsCount *int64                `json:"user_discussions_count,omitzero"`
	Features             *WorkItemRESTFeatures `json:"features,omitzero"`
}

func (w WorkItemREST) String() string {
	return Stringify(w)
}

// WorkItemRESTUser represents a user in a work items REST API response.
//
// GitLab API docs: not yet published
type WorkItemRESTUser struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	PublicEmail string `json:"public_email"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Locked      bool   `json:"locked"`
	AvatarURL   string `json:"avatar_url"`
	WebURL      string `json:"web_url"`
	WebPath     string `json:"web_path"`
}

// WorkItemRESTType represents the type of a work item in a work items REST
// API response.
//
// GitLab API docs: not yet published
type WorkItemRESTType struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IconName string `json:"icon_name"`
}

// WorkItemRESTNamespace represents the project or group a work item belongs
// to, in a work items REST API response.
//
// GitLab API docs: not yet published
type WorkItemRESTNamespace struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	FullPath  string `json:"full_path"`
	ParentID  int64  `json:"parent_id"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
}

// WorkItemRESTFeatures represents the feature payloads of a work item in a
// work items REST API response. A feature is nil unless it was requested and
// the work item type supports it.
//
// GitLab API docs: not yet published
type WorkItemRESTFeatures struct {
	// Assignees is nil when the feature was not requested, and empty when the
	// work item has no assignees.
	Assignees   []WorkItemRESTUser       `json:"assignees,omitzero"`
	Description *WorkItemRESTDescription `json:"description,omitzero"`
	Hierarchy   *WorkItemRESTHierarchy   `json:"hierarchy,omitzero"`
	Labels      *WorkItemRESTLabels      `json:"labels,omitzero"`
	// Milestone is nil when the feature was not requested, and also when the
	// work item has no milestone.
	Milestone       *Milestone                   `json:"milestone,omitzero"`
	StartAndDueDate *WorkItemRESTStartAndDueDate `json:"start_and_due_date,omitzero"`
}

// WorkItemRESTDescription represents the description feature of a work item.
//
// GitLab API docs: not yet published
type WorkItemRESTDescription struct {
	Description          string                 `json:"description"`
	DescriptionHTML      string                 `json:"description_html"`
	Edited               bool                   `json:"edited"`
	LastEditedAt         *time.Time             `json:"last_edited_at"`
	LastEditedBy         *WorkItemRESTUser      `json:"last_edited_by"`
	TaskCompletionStatus *TasksCompletionStatus `json:"task_completion_status"`
}

// WorkItemRESTHierarchy represents the hierarchy feature of a work item.
//
// GitLab API docs: not yet published
type WorkItemRESTHierarchy struct {
	Parent    *WorkItemRESTReference `json:"parent"`
	HasParent bool                   `json:"has_parent"`
}

// WorkItemRESTReference represents another work item referenced from a work
// item, such as its parent.
//
// GitLab API docs: not yet published
type WorkItemRESTReference struct {
	ID           int64                  `json:"id"`
	GlobalID     string                 `json:"global_id"`
	IID          int64                  `json:"iid"`
	Title        string                 `json:"title"`
	TitleHTML    string                 `json:"title_html"`
	State        string                 `json:"state"`
	Confidential bool                   `json:"confidential"`
	WorkItemType *WorkItemRESTType      `json:"work_item_type"`
	Reference    string                 `json:"reference"`
	WebURL       string                 `json:"web_url"`
	WebPath      string                 `json:"web_path"`
	Namespace    *WorkItemRESTNamespace `json:"namespace"`
}

// WorkItemRESTLabels represents the labels feature of a work item.
//
// GitLab API docs: not yet published
type WorkItemRESTLabels struct {
	AllowsScopedLabels bool                 `json:"allows_scoped_labels"`
	Labels             []*WorkItemRESTLabel `json:"labels"`
}

// WorkItemRESTLabel represents a label on a work item.
//
// GitLab API docs: not yet published
type WorkItemRESTLabel struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       string `json:"color"`
	TextColor   string `json:"text_color"`
}

// WorkItemRESTStartAndDueDate represents the start and due date feature of a
// work item.
//
// GitLab API docs: not yet published
type WorkItemRESTStartAndDueDate struct {
	StartDate *ISOTime `json:"start_date"`
	DueDate   *ISOTime `json:"due_date"`
	RollUp    bool     `json:"roll_up"`
	IsFixed   bool     `json:"is_fixed"`
}

// GetWorkItemRESTOptions represents the available GetWorkItem() options.
//
// Fields and Features select what the API returns beyond ID, IID, GlobalID
// and Title. Use AllWorkItemFields and AllWorkItemFeatures to request
// everything this client models.
//
// GitLab API docs: not yet published
type GetWorkItemRESTOptions struct {
	Fields   []WorkItemFieldValue   `url:"fields,comma,omitempty" json:"fields,omitempty"`
	Features []WorkItemFeatureValue `url:"features,comma,omitempty" json:"features,omitempty"`
}

// GetWorkItem gets a single work item in a project or group namespace.
//
// nid is the ID or full path of the project or group namespace.
// iid is the internal ID of the work item.
//
// GitLab API docs: not yet published
func (s *workItemsRESTService) GetWorkItem(nid any, iid int64, opt *GetWorkItemRESTOptions, options ...RequestOptionFunc) (*WorkItemREST, *Response, error) {
	namespace, err := parseID(nid)
	if err != nil {
		return nil, nil, err
	}
	return do[*WorkItemREST](s.client,
		withPath(routeNamespacesIDWorkItemsID, namespace, iid),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}
