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
	"bytes"
	"io"
	"net/http"
	"time"
)

var (
	routeProjectsIDExport         = route("projects/%s/export")
	routeProjectsIDExportDownload = route("projects/%s/export/download")
	routeProjectsIDImport         = route("projects/%s/import")
	routeProjectsImport           = route("projects/import")
)

type (
	ProjectImportExportServiceInterface interface {
		// ScheduleExport schedules a project export.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/project_import_export/#export-a-project
		ScheduleExport(pid any, opt *ScheduleExportOptions, options ...RequestOptionFunc) (*Response, error)
		// ExportStatus gets the status of export.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-export
		ExportStatus(pid any, options ...RequestOptionFunc) (*ExportStatus, *Response, error)
		// ExportDownload downloads the finished export.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/project_import_export/#download-a-project-export
		ExportDownload(pid any, options ...RequestOptionFunc) ([]byte, *Response, error)
		// ImportFromFile imports a project from an archive file.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/project_import_export/#import-a-project-from-a-local-archive
		ImportFromFile(archive io.Reader, opt *ImportFileOptions, options ...RequestOptionFunc) (*ImportStatus, *Response, error)
		// ImportStatus gets the status of an import.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-import
		ImportStatus(pid any, options ...RequestOptionFunc) (*ImportStatus, *Response, error)
	}

	// ProjectImportExportService handles communication with the project
	// import/export related methods of the GitLab API.
	//
	// GitLab API docs:
	// https://docs.gitlab.com/api/project_import_export/
	ProjectImportExportService struct {
		client *Client
	}
)

var _ ProjectImportExportServiceInterface = (*ProjectImportExportService)(nil)

// ImportStatus represents a project import status.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-import
type ImportStatus struct {
	ID                int64  `json:"id"`
	Description       string `json:"description"`
	Name              string `json:"name"`
	NameWithNamespace string `json:"name_with_namespace"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`

	// Deprecated: use CreatedAt instead. GitLab sends created_at, not
	// create_at, so this field is always nil.
	CreateAt *time.Time `json:"create_at"`

	CreatedAt       *time.Time                     `json:"created_at"`
	ImportStatus    string                         `json:"import_status"`
	ImportType      string                         `json:"import_type"`
	CorrelationID   string                         `json:"correlation_id"`
	FailedRelations []*ProjectImportFailedRelation `json:"failed_relations"`
	ImportError     string                         `json:"import_error"`
	// Stats counts the objects fetched from GitHub and imported so far. It
	// is nil for every other kind of import.
	Stats *ProjectImportStats `json:"stats"`
}

func (s ImportStatus) String() string {
	return Stringify(s)
}

// ProjectImportFailedRelation represents a relation an import could not
// bring over. GitLab lists at most 100 of them. It also sends
// exception_message, always as null, which is why this struct leaves it out.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-import
type ProjectImportFailedRelation struct {
	ID             int64      `json:"id"`
	CreatedAt      *time.Time `json:"created_at"`
	ExceptionClass string     `json:"exception_class"`
	Source         string     `json:"source"`
	RelationName   string     `json:"relation_name"`
	LineNumber     int64      `json:"line_number"`
}

// ProjectImportStats represents the counts of a GitHub import: the objects
// fetched from GitHub and those imported so far. Each map is keyed by the
// object type GitLab counted, such as "issue", "pull_request", or "note".
// The set of keys is open: GitLab adds one only once it counts an object of
// that type, and a finished import keeps the keys of the GitLab release that
// ran it.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-import
type ProjectImportStats struct {
	Fetched  map[string]int64 `json:"fetched"`
	Imported map[string]int64 `json:"imported"`
}

// ExportStatus represents a project export status.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-export
type ExportStatus struct {
	ID                int64             `json:"id"`
	Description       string            `json:"description"`
	Name              string            `json:"name"`
	NameWithNamespace string            `json:"name_with_namespace"`
	Path              string            `json:"path"`
	PathWithNamespace string            `json:"path_with_namespace"`
	CreatedAt         *time.Time        `json:"created_at"`
	ExportStatus      string            `json:"export_status"`
	Message           string            `json:"message"`
	Links             ExportStatusLinks `json:"_links"`
}

func (s ExportStatus) String() string {
	return Stringify(s)
}

// ExportStatusLinks represents the project export status links.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#retrieve-the-status-of-a-project-export
type ExportStatusLinks struct {
	APIURL string `json:"api_url"`
	WebURL string `json:"web_url"`
}

func (l ExportStatusLinks) String() string {
	return Stringify(l)
}

// ScheduleExportOptions represents the available ScheduleExport() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#export-a-project
type ScheduleExportOptions struct {
	Description *string                     `url:"description,omitempty" json:"description,omitempty"`
	Upload      ScheduleExportUploadOptions `url:"upload,omitempty" json:"upload,omitempty"`
}

type ScheduleExportUploadOptions struct {
	URL        *string `url:"url,omitempty" json:"url,omitempty"`
	HTTPMethod *string `url:"http_method,omitempty" json:"http_method,omitempty"`
}

func (s *ProjectImportExportService) ScheduleExport(pid any, opt *ScheduleExportOptions, options ...RequestOptionFunc) (*Response, error) {
	_, resp, err := do[none](s.client,
		withMethod(http.MethodPost),
		withPath(routeProjectsIDExport, ProjectID{pid}),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
	return resp, err
}

func (s *ProjectImportExportService) ExportStatus(pid any, options ...RequestOptionFunc) (*ExportStatus, *Response, error) {
	return do[*ExportStatus](s.client,
		withPath(routeProjectsIDExport, ProjectID{pid}),
		withRequestOpts(options...),
	)
}

func (s *ProjectImportExportService) ExportDownload(pid any, options ...RequestOptionFunc) ([]byte, *Response, error) {
	buf, resp, err := do[bytes.Buffer](s.client,
		withPath(routeProjectsIDExportDownload, ProjectID{pid}),
		withRequestOpts(options...),
	)
	if err != nil {
		return nil, resp, err
	}
	return buf.Bytes(), resp, nil
}

// ImportFileOptions represents the available ImportFile() options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/project_import_export/#import-a-project-from-a-local-archive
type ImportFileOptions struct {
	Namespace      *string               `url:"namespace,omitempty" json:"namespace,omitempty"`
	Name           *string               `url:"name,omitempty" json:"name,omitempty"`
	Path           *string               `url:"path,omitempty" json:"path,omitempty"`
	Overwrite      *bool                 `url:"overwrite,omitempty" json:"overwrite,omitempty"`
	OverrideParams *CreateProjectOptions `url:"override_params,omitempty" json:"override_params,omitempty"`
}

func (s *ProjectImportExportService) ImportFromFile(archive io.Reader, opt *ImportFileOptions, options ...RequestOptionFunc) (*ImportStatus, *Response, error) {
	return do[*ImportStatus](s.client,
		withMethod(http.MethodPost),
		withPath(routeProjectsImport),
		withUpload(archive, "archive.tar.gz", UploadFile),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

func (s *ProjectImportExportService) ImportStatus(pid any, options ...RequestOptionFunc) (*ImportStatus, *Response, error) {
	return do[*ImportStatus](s.client,
		withPath(routeProjectsIDImport, ProjectID{pid}),
		withRequestOpts(options...),
	)
}
