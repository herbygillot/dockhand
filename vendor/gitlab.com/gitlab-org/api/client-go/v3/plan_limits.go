//
// Copyright 2021, Igor Varavko
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

import "net/http"

var routeApplicationPlanLimits = route("application/plan_limits")

type (
	PlanLimitsServiceInterface interface {
		// GetCurrentPlanLimits lists the current limits of a plan on the
		// GitLab instance.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/plan_limits/#retrieve-current-plan-limits
		GetCurrentPlanLimits(opt *GetCurrentPlanLimitsOptions, options ...RequestOptionFunc) (*PlanLimit, *Response, error)

		// ChangePlanLimits modifies the limits of a plan on the GitLab instance.
		//
		// GitLab API docs:
		// https://docs.gitlab.com/api/plan_limits/#update-plan-limits
		ChangePlanLimits(opt *ChangePlanLimitOptions, options ...RequestOptionFunc) (*PlanLimit, *Response, error)
	}

	// PlanLimitsService handles communication with the plan limits related
	// methods of the GitLab API.
	//
	// GitLab API docs: https://docs.gitlab.com/api/plan_limits/
	PlanLimitsService struct {
		client *Client
	}
)

var _ PlanLimitsServiceInterface = (*PlanLimitsService)(nil)

// PlanLimit represents the limits of a GitLab plan.
//
// Every instance returns WebHookCallsLow and WebHookCallsMid, but only
// GitLab.com reads them: it steps the webhook rate limit of a top-level
// namespace on a paid plan by its number of seats, and they are the two
// lower steps. WebHookCalls is the top step and the limit everywhere else.
//
// GitLab API docs: https://docs.gitlab.com/api/plan_limits/
type PlanLimit struct {
	CIInstanceLevelVariables         int64             `json:"ci_instance_level_variables,omitempty"`
	CIPipelineSize                   int64             `json:"ci_pipeline_size,omitempty"`
	CIActiveJobs                     int64             `json:"ci_active_jobs,omitempty"`
	CIProjectSubscriptions           int64             `json:"ci_project_subscriptions,omitempty"`
	CIPipelineSchedules              int64             `json:"ci_pipeline_schedules,omitempty"`
	CINeedsSizeLimit                 int64             `json:"ci_needs_size_limit,omitempty"`
	CIRegisteredGroupRunners         int64             `json:"ci_registered_group_runners,omitempty"`
	CIRegisteredProjectRunners       int64             `json:"ci_registered_project_runners,omitempty"`
	DotenvSize                       int64             `json:"dotenv_size,omitempty"`
	DotenvVariables                  int64             `json:"dotenv_variables,omitempty"`
	CargoMaxFileSize                 int64             `json:"cargo_max_file_size,omitempty"`
	ConanMaxFileSize                 int64             `json:"conan_max_file_size,omitempty"`
	EnforcementLimit                 int64             `json:"enforcement_limit,omitempty"`
	GenericPackagesMaxFileSize       int64             `json:"generic_packages_max_file_size,omitempty"`
	HelmMaxFileSize                  int64             `json:"helm_max_file_size,omitempty"`
	MavenMaxFileSize                 int64             `json:"maven_max_file_size,omitempty"`
	NotificationLimit                int64             `json:"notification_limit,omitempty"`
	NPMMaxFileSize                   int64             `json:"npm_max_file_size,omitempty"`
	NugetMaxFileSize                 int64             `json:"nuget_max_file_size,omitempty"`
	MaxPipelinesPerMergeTrain        int64             `json:"max_pipelines_per_merge_train,omitempty"`
	PipelineHierarchySize            int64             `json:"pipeline_hierarchy_size,omitempty"`
	PyPiMaxFileSize                  int64             `json:"pypi_max_file_size,omitempty"`
	ServiceDeskOutboundEmailsPerHour int64             `json:"service_desk_outbound_emails_per_hour,omitempty"`
	ServiceDeskOutboundEmailsPerDay  int64             `json:"service_desk_outbound_emails_per_day,omitempty"`
	TerraformModuleMaxFileSize       int64             `json:"terraform_module_max_file_size,omitempty"`
	StorageSizeLimit                 int64             `json:"storage_size_limit,omitempty"`
	WebHookCalls                     int64             `json:"web_hook_calls,omitempty"`
	WebHookCallsLow                  int64             `json:"web_hook_calls_low,omitempty"`
	WebHookCallsMid                  int64             `json:"web_hook_calls_mid,omitempty"`
	LimitsHistory                    PlanLimitsHistory `json:"limits_history"`
}

// PlanLimitsHistory represents the changes GitLab records to four of the
// limits of a plan, oldest first.
//
// GitLab API docs: https://docs.gitlab.com/api/plan_limits/
type PlanLimitsHistory struct {
	EnforcementLimit        []PlanLimitHistoryEntry `json:"enforcement_limit"`
	NotificationLimit       []PlanLimitHistoryEntry `json:"notification_limit"`
	StorageSizeLimit        []PlanLimitHistoryEntry `json:"storage_size_limit"`
	DashboardLimitEnabledAt []PlanLimitHistoryEntry `json:"dashboard_limit_enabled_at"`
}

// PlanLimitHistoryEntry represents one change to a plan limit: who made it,
// when, in seconds since the Unix epoch, and the value it set.
//
// GitLab API docs: https://docs.gitlab.com/api/plan_limits/
type PlanLimitHistoryEntry struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Timestamp int64  `json:"timestamp"`
	Value     int64  `json:"value"`
}

// GetCurrentPlanLimitsOptions represents the available GetCurrentPlanLimits()
// options.
//
// GitLab API docs:
// https://docs.gitlab.com/api/plan_limits/#retrieve-current-plan-limits
type GetCurrentPlanLimitsOptions struct {
	PlanName *string `url:"plan_name,omitempty" json:"plan_name,omitempty"`
}

func (s *PlanLimitsService) GetCurrentPlanLimits(opt *GetCurrentPlanLimitsOptions, options ...RequestOptionFunc) (*PlanLimit, *Response, error) {
	return do[*PlanLimit](s.client,
		withPath(routeApplicationPlanLimits),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}

// ChangePlanLimitOptions represents the available ChangePlanLimits() options.
//
// Every instance accepts WebHookCallsLow and WebHookCallsMid, but only
// GitLab.com reads them, as the doc comment of PlanLimit describes.
//
// GitLab API docs:
// https://docs.gitlab.com/api/plan_limits/#update-plan-limits
type ChangePlanLimitOptions struct {
	PlanName                         *string `url:"plan_name,omitempty" json:"plan_name,omitempty"`
	CIInstanceLevelVariables         *int64  `url:"ci_instance_level_variables,omitempty" json:"ci_instance_level_variables,omitempty"`
	CIPipelineSize                   *int64  `url:"ci_pipeline_size,omitempty" json:"ci_pipeline_size,omitempty"`
	CIActiveJobs                     *int64  `url:"ci_active_jobs,omitempty" json:"ci_active_jobs,omitempty"`
	CIProjectSubscriptions           *int64  `url:"ci_project_subscriptions,omitempty" json:"ci_project_subscriptions,omitempty"`
	CIPipelineSchedules              *int64  `url:"ci_pipeline_schedules,omitempty" json:"ci_pipeline_schedules,omitempty"`
	CINeedsSizeLimit                 *int64  `url:"ci_needs_size_limit,omitempty" json:"ci_needs_size_limit,omitempty"`
	CIRegisteredGroupRunners         *int64  `url:"ci_registered_group_runners,omitempty" json:"ci_registered_group_runners,omitempty"`
	CIRegisteredProjectRunners       *int64  `url:"ci_registered_project_runners,omitempty" json:"ci_registered_project_runners,omitempty"`
	DotenvSize                       *int64  `url:"dotenv_size,omitempty" json:"dotenv_size,omitempty"`
	DotenvVariables                  *int64  `url:"dotenv_variables,omitempty" json:"dotenv_variables,omitempty"`
	CargoMaxFileSize                 *int64  `url:"cargo_max_file_size,omitempty" json:"cargo_max_file_size,omitempty"`
	ConanMaxFileSize                 *int64  `url:"conan_max_file_size,omitempty" json:"conan_max_file_size,omitempty"`
	EnforcementLimit                 *int64  `url:"enforcement_limit,omitempty" json:"enforcement_limit,omitempty"`
	GenericPackagesMaxFileSize       *int64  `url:"generic_packages_max_file_size,omitempty" json:"generic_packages_max_file_size,omitempty"`
	HelmMaxFileSize                  *int64  `url:"helm_max_file_size,omitempty" json:"helm_max_file_size,omitempty"`
	MavenMaxFileSize                 *int64  `url:"maven_max_file_size,omitempty" json:"maven_max_file_size,omitempty"`
	NotificationLimit                *int64  `url:"notification_limit,omitempty" json:"notification_limit,omitempty"`
	NPMMaxFileSize                   *int64  `url:"npm_max_file_size,omitempty" json:"npm_max_file_size,omitempty"`
	NugetMaxFileSize                 *int64  `url:"nuget_max_file_size,omitempty" json:"nuget_max_file_size,omitempty"`
	MaxPipelinesPerMergeTrain        *int64  `url:"max_pipelines_per_merge_train,omitempty" json:"max_pipelines_per_merge_train,omitempty"`
	PipelineHierarchySize            *int64  `url:"pipeline_hierarchy_size,omitempty" json:"pipeline_hierarchy_size,omitempty"`
	PyPiMaxFileSize                  *int64  `url:"pypi_max_file_size,omitempty" json:"pypi_max_file_size,omitempty"`
	ServiceDeskOutboundEmailsPerHour *int64  `url:"service_desk_outbound_emails_per_hour,omitempty" json:"service_desk_outbound_emails_per_hour,omitempty"`
	ServiceDeskOutboundEmailsPerDay  *int64  `url:"service_desk_outbound_emails_per_day,omitempty" json:"service_desk_outbound_emails_per_day,omitempty"`
	TerraformModuleMaxFileSize       *int64  `url:"terraform_module_max_file_size,omitempty" json:"terraform_module_max_file_size,omitempty"`
	StorageSizeLimit                 *int64  `url:"storage_size_limit,omitempty" json:"storage_size_limit,omitempty"`
	WebHookCalls                     *int64  `url:"web_hook_calls,omitempty" json:"web_hook_calls,omitempty"`
	WebHookCallsLow                  *int64  `url:"web_hook_calls_low,omitempty" json:"web_hook_calls_low,omitempty"`
	WebHookCallsMid                  *int64  `url:"web_hook_calls_mid,omitempty" json:"web_hook_calls_mid,omitempty"`
}

func (s *PlanLimitsService) ChangePlanLimits(opt *ChangePlanLimitOptions, options ...RequestOptionFunc) (*PlanLimit, *Response, error) {
	return do[*PlanLimit](s.client,
		withMethod(http.MethodPut),
		withPath(routeApplicationPlanLimits),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}
