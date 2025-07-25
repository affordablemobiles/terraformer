// Copyright 2023 The Terraformer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/run/v2"
)

// CloudRunGenerator generates Terraform resources for Cloud Run.
type CloudRunGenerator struct {
	GCPService
}

// InitResources initializes the Cloud Run resources.
func (g *CloudRunGenerator) InitResources() error {
	if g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global" {
		return nil
	}

	project := g.GetArgs()["project"].(string)
	location := g.GetArgs()["region"].(compute.Region).Name

	ctx := context.Background()

	runService, err := run.NewService(ctx)
	if err != nil {
		return fmt.Errorf("failed to create cloud run service: %w", err)
	}

	if err := g.initServices(ctx, runService, project, location); err != nil {
		return err
	}
	if err := g.initJobs(ctx, runService, project, location); err != nil {
		return err
	}
	if err := g.initWorkerPools(ctx, runService, project, location); err != nil {
		return err
	}

	return nil
}

func (g *CloudRunGenerator) initServices(ctx context.Context, runService *run.Service, project, location string) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	req := runService.Projects.Locations.Services.List(parent)
	if err := req.Pages(ctx, func(page *run.GoogleCloudRunV2ListServicesResponse) error {
		for _, service := range page.Services {
			parts := strings.Split(service.Name, "/")
			serviceName := parts[len(parts)-1]
			g.Resources = append(g.Resources, terraformutils.NewResource(
				service.Name,
				serviceName,
				"google_cloud_run_v2_service",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"location": location,
					"name":     serviceName,
				},
				[]string{},
				map[string]interface{}{},
			))

			if err := g.initServiceIamPolicy(ctx, runService, service.Name, serviceName, project, location); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list cloud run services: %w", err)
	}
	return nil
}

func (g *CloudRunGenerator) initServiceIamPolicy(ctx context.Context, runService *run.Service, serviceFullName, serviceName, project, location string) error {
	policy, err := runService.Projects.Locations.Services.GetIamPolicy(serviceFullName).Do()
	if err != nil {
		return fmt.Errorf("failed to get iam policy for cloud run service %s: %w", serviceName, err)
	}

	for _, binding := range policy.Bindings {
		for _, member := range binding.Members {
			g.Resources = append(g.Resources, terraformutils.NewResource(
				fmt.Sprintf("%s/%s/%s", serviceFullName, binding.Role, member),
				fmt.Sprintf("%s-%s-%s", serviceName, binding.Role, member),
				"google_cloud_run_v2_service_iam_member",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"location": location,
					"name":     serviceName,
					"role":     binding.Role,
					"member":   member,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
	}
	return nil
}

func (g *CloudRunGenerator) initJobs(ctx context.Context, runService *run.Service, project, location string) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	req := runService.Projects.Locations.Jobs.List(parent)
	if err := req.Pages(ctx, func(page *run.GoogleCloudRunV2ListJobsResponse) error {
		for _, job := range page.Jobs {
			parts := strings.Split(job.Name, "/")
			jobName := parts[len(parts)-1]
			g.Resources = append(g.Resources, terraformutils.NewResource(
				job.Name,
				jobName,
				"google_cloud_run_v2_job",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"location": location,
					"name":     jobName,
				},
				[]string{},
				map[string]interface{}{},
			))

			if err := g.initJobIamPolicy(ctx, runService, job.Name, jobName, project, location); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list cloud run jobs: %w", err)
	}
	return nil
}

func (g *CloudRunGenerator) initJobIamPolicy(ctx context.Context, runService *run.Service, jobFullName, jobName, project, location string) error {
	policy, err := runService.Projects.Locations.Jobs.GetIamPolicy(jobFullName).Do()
	if err != nil {
		return fmt.Errorf("failed to get iam policy for cloud run job %s: %w", jobName, err)
	}

	for _, binding := range policy.Bindings {
		for _, member := range binding.Members {
			g.Resources = append(g.Resources, terraformutils.NewResource(
				fmt.Sprintf("%s/%s/%s", jobFullName, binding.Role, member),
				fmt.Sprintf("%s-%s-%s", jobName, binding.Role, member),
				"google_cloud_run_v2_job_iam_member",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"location": location,
					"name":     jobName,
					"role":     binding.Role,
					"member":   member,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
	}
	return nil
}

func (g *CloudRunGenerator) initWorkerPools(ctx context.Context, runService *run.Service, project, location string) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	req := runService.Projects.Locations.WorkerPools.List(parent)
	if err := req.Pages(ctx, func(page *run.GoogleCloudRunV2ListWorkerPoolsResponse) error {
		for _, pool := range page.WorkerPools {
			parts := strings.Split(pool.Name, "/")
			poolName := parts[len(parts)-1]
			g.Resources = append(g.Resources, terraformutils.NewResource(
				pool.Name,
				poolName,
				"google_cloud_run_v2_worker_pool",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"location": location,
					"name":     poolName,
				},
				[]string{},
				map[string]interface{}{},
			))

			if err := g.initWorkerPoolIamPolicy(ctx, runService, pool.Name, poolName, project, location); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list cloud run worker pools: %w", err)
	}
	return nil
}

func (g *CloudRunGenerator) initWorkerPoolIamPolicy(ctx context.Context, runService *run.Service, poolFullName, poolName, project, location string) error {
	policy, err := runService.Projects.Locations.WorkerPools.GetIamPolicy(poolFullName).Do()
	if err != nil {
		return fmt.Errorf("failed to get iam policy for cloud run worker pool %s: %w", poolName, err)
	}

	for _, binding := range policy.Bindings {
		for _, member := range binding.Members {
			g.Resources = append(g.Resources, terraformutils.NewResource(
				fmt.Sprintf("%s/%s/%s", poolFullName, binding.Role, member),
				fmt.Sprintf("%s-%s-%s", poolName, binding.Role, member),
				"google_cloud_run_v2_worker_pool_iam_member",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"location": location,
					"name":     poolName,
					"role":     binding.Role,
					"member":   member,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
	}
	return nil
}

func (g *CloudRunGenerator) PostConvertHook() error {
	for i, resource := range g.Resources {
		switch resource.InstanceInfo.Type {
		case "google_cloud_run_v2_service", "google_cloud_run_v2_worker_pool":
			g.Resources[i].PreserveOrder = []string{"template.containers.args"}
		case "google_cloud_run_v2_job":
			g.Resources[i].PreserveOrder = []string{"template.template.containers.args"}
		default:
		}
	}

	return nil
}
