// Copyright 2024 The Terraformer Authors.
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
	"log"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/appengine/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/iap/v1"
	"google.golang.org/api/run/v1"
)

// IapGenerator holds the logic for generating IAP resources.
type IapGenerator struct {
	GCPService
}

// createIapBrandResources creates terraform resources for `google_iap_brand`
func (g *IapGenerator) createIapBrandResources(ctx context.Context, iapService *iap.Service, project string) ([]terraformutils.Resource, error) {
	parent := "projects/" + project
	brand, err := iapService.Projects.Brands.Get(parent).Do()
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			log.Printf("[INFO] No IAP brand found for project %s. Skipping.", project)
			return []terraformutils.Resource{}, nil
		}
		return nil, err
	}

	return []terraformutils.Resource{
		terraformutils.NewResource(
			brand.Name,
			brand.Name,
			"google_iap_brand",
			g.ProviderName,
			map[string]string{
				"project": project,
			},
			[]string{},
			map[string]interface{}{},
		),
	}, nil
}

// createIapClientResources creates terraform resources for `google_iap_client`
func (g *IapGenerator) createIapClientResources(ctx context.Context, iapService *iap.Service, project string) ([]terraformutils.Resource, error) {
	parent := "projects/" + project
	var resources []terraformutils.Resource

	brand, err := iapService.Projects.Brands.Get(parent).Do()
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			log.Printf("[INFO] No IAP brand found for project %s, so no IAP clients to import.", project)
			return nil, nil
		}
		return nil, err
	}

	clientParent := brand.Name
	err = iapService.Projects.Brands.IdentityAwareProxyClients.List(clientParent).Pages(ctx, func(page *iap.ListIdentityAwareProxyClientsResponse) error {
		for _, client := range page.IdentityAwareProxyClients {
			t := strings.Split(client.Name, "/")
			name := t[len(t)-1]
			resources = append(resources, terraformutils.NewResource(
				client.Name,
				name,
				"google_iap_client",
				g.ProviderName,
				map[string]string{
					"brand": brand.Name,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	})

	return resources, err
}

// createIapIamMemberResources creates terraform resources for each member of a role binding.
func (g *IapGenerator) createIapIamMemberResources(resourceID, resourceName, resourceType string, additionalAttributes map[string]string, bindings []*iap.Binding) []terraformutils.Resource {
	var resources []terraformutils.Resource
	for _, binding := range bindings {
		for _, member := range binding.Members {
			attributes := map[string]string{
				"project": g.GetArgs()["project"].(string),
				"role":    binding.Role,
				"member":  member,
			}
			for k, v := range additionalAttributes {
				attributes[k] = v
			}

			var memberResourceID string
			// The terraform provider expects the import ID for IAM members to be space-delimited.
			if binding.Condition != nil && binding.Condition.Title != "" {
				// For conditional bindings, the condition title is the fourth part of the ID.
				memberResourceID = fmt.Sprintf("%s %s %s %s", resourceID, binding.Role, member, binding.Condition.Title)
				attributes["condition.#"] = "1"
				attributes["condition.0.title"] = binding.Condition.Title
				attributes["condition.0.description"] = binding.Condition.Description
				attributes["condition.0.expression"] = binding.Condition.Expression
			} else {
				memberResourceID = fmt.Sprintf("%s %s %s", resourceID, binding.Role, member)
			}

			memberResourceName := fmt.Sprintf("%s-%s-%s", resourceName, terraformutils.TfSanitize(binding.Role), terraformutils.TfSanitize(member))
			if binding.Condition != nil && binding.Condition.Title != "" {
				memberResourceName = fmt.Sprintf("%s-%s", memberResourceName, terraformutils.TfSanitize(binding.Condition.Title))
			}

			resources = append(resources, terraformutils.NewResource(
				memberResourceID,
				memberResourceName,
				resourceType,
				g.ProviderName,
				attributes,
				[]string{},
				map[string]interface{}{},
			))
		}
	}
	return resources
}

// addIamMemberResourcesWithPolicyCheck fetches the IAM policy for a resource and adds member resources to the list if it has bindings.
func (g *IapGenerator) addIamMemberResourcesWithPolicyCheck(resources *[]terraformutils.Resource, iapService *iap.Service, resourceID, resourceName, resourceType string, additionalAttributes map[string]string) {
	log.Printf("Checking IAP IAM for %s", resourceID)
	getIamPolicyRequest := &iap.GetIamPolicyRequest{
		Options: &iap.GetPolicyOptions{
			RequestedPolicyVersion: 3,
		},
	}
	policy, err := iapService.V1.GetIamPolicy(resourceID, getIamPolicyRequest).Do()

	if err != nil {
		// It's common for aggregated lists to contain recently deleted resources, so we treat 404s as informational.
		if strings.Contains(err.Error(), "404") {
			log.Printf("[INFO] IAM policy not found for %s, skipping.", resourceID)
		} else {
			log.Printf("[ERROR] Failed to get IAM policy for %s: %v", resourceID, err)
		}
		return
	}

	if policy != nil && len(policy.Bindings) > 0 {
		memberResources := g.createIapIamMemberResources(resourceID, resourceName, resourceType, additionalAttributes, policy.Bindings)
		*resources = append(*resources, memberResources...)
	}
}

func (g *IapGenerator) addIamMemberAndSettingsResourcesWithPolicyCheck(resources *[]terraformutils.Resource, iapService *iap.Service, resourceID, resourceName, iamResourceType string, iamAdditionalAttributes map[string]string) {
	g.addIamMemberResourcesWithPolicyCheck(resources, iapService, resourceID, resourceName, iamResourceType, iamAdditionalAttributes)
	g.addIapSettingsResourceWithCheck(resources, iapService, resourceID, resourceName, map[string]string{})
}

// addIapSettingsResourceWithCheck fetches the IAP settings for a resource and adds a settings resource if customizations exist.
func (g *IapGenerator) addIapSettingsResourceWithCheck(resources *[]terraformutils.Resource, iapService *iap.Service, resourceID, resourceName string, additionalAttributes map[string]string) {
	log.Printf("Checking IAP settings for %s", resourceID)
	settings, err := iapService.V1.GetIapSettings(resourceID).Do()
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			log.Printf("[INFO] IAP settings not found for %s, skipping.", resourceID)
		} else {
			log.Printf("[ERROR] Failed to get IAP settings for %s: %v", resourceID, err)
		}
		return
	}

	// Only create a settings resource if there are actual customizations.
	if settings != nil && (settings.AccessSettings != nil || settings.ApplicationSettings != nil) {
		attributes := map[string]string{
			"project": g.GetArgs()["project"].(string),
			"name":    resourceID,
		}
		for k, v := range additionalAttributes {
			attributes[k] = v
		}

		*resources = append(*resources, terraformutils.NewResource(
			resourceID,
			resourceName+"_settings", // Append suffix to avoid name collisions
			"google_iap_settings",
			g.ProviderName,
			attributes,
			[]string{},
			map[string]interface{}{},
		))
	}
}

// initGlobalIapResources initializes IAP resources that are global.
func (g *IapGenerator) initGlobalIapResources(ctx context.Context, iapService *iap.Service, project string) ([]terraformutils.Resource, error) {
	var globalResources []terraformutils.Resource

	brandResources, err := g.createIapBrandResources(ctx, iapService, project)
	if err != nil {
		log.Printf("[ERROR] Failed to list IAP brands: %v", err)
	}
	globalResources = append(globalResources, brandResources...)

	clientResources, err := g.createIapClientResources(ctx, iapService, project)
	if err != nil {
		log.Printf("[ERROR] Failed to list IAP clients: %v", err)
	}
	globalResources = append(globalResources, clientResources...)

	appengineService, err := appengine.NewService(ctx)
	if err == nil {
		app, err := appengineService.Apps.Get(project).Do()
		if err == nil {
			appID := app.Id
			g.addIamMemberAndSettingsResourcesWithPolicyCheck(&globalResources, iapService,
				fmt.Sprintf("projects/%s/iap_web/appengine-%s", project, appID),
				fmt.Sprintf("appengine-%s", appID),
				"google_iap_web_type_app_engine_iam_member",
				map[string]string{"app_id": appID})

			_ = appengineService.Apps.Services.List(project).Pages(ctx, func(page *appengine.ListServicesResponse) error {
				for _, service := range page.Services {
					g.addIamMemberAndSettingsResourcesWithPolicyCheck(&globalResources, iapService,
						fmt.Sprintf("projects/%s/iap_web/appengine-%s/services/%s", project, appID, service.Id),
						fmt.Sprintf("%s-%s", project, service.Id),
						"google_iap_app_engine_service_iam_member",
						map[string]string{
							"app_id":  appID,
							"service": service.Id,
						})

					_ = appengineService.Apps.Services.Versions.List(project, service.Id).Pages(ctx, func(page *appengine.ListVersionsResponse) error {
						for _, version := range page.Versions {
							g.addIamMemberAndSettingsResourcesWithPolicyCheck(&globalResources, iapService,
								fmt.Sprintf("projects/%s/iap_web/appengine-%s/services/%s/versions/%s", project, appID, service.Id, version.Id),
								fmt.Sprintf("%s-%s-%s", project, service.Id, version.Id),
								"google_iap_app_engine_version_iam_member",
								map[string]string{
									"app_id":     appID,
									"service":    service.Id,
									"version_id": version.Id,
								})
						}
						return nil
					})
				}
				return nil
			})
		}
	}

	computeService, err := compute.NewService(ctx)
	if err == nil {
		g.addIamMemberAndSettingsResourcesWithPolicyCheck(&globalResources, iapService,
			fmt.Sprintf("projects/%s/iap_web/compute", project),
			"compute",
			"google_iap_web_type_compute_iam_member", nil)

		_ = computeService.BackendServices.AggregatedList(project).Pages(ctx, func(page *compute.BackendServiceAggregatedList) error {
			for scope, backendServicesScopedList := range page.Items {
				if scope != "global" {
					continue
				}
				for _, backendService := range backendServicesScopedList.BackendServices {
					g.addIamMemberAndSettingsResourcesWithPolicyCheck(&globalResources, iapService,
						fmt.Sprintf("projects/%s/iap_web/compute/services/%s", project, backendService.Name),
						backendService.Name,
						"google_iap_web_backend_service_iam_member",
						map[string]string{
							"web_backend_service": backendService.Name,
						})
				}
			}
			return nil
		})
	}

	g.addIamMemberAndSettingsResourcesWithPolicyCheck(&globalResources, iapService,
		fmt.Sprintf("projects/%s/iap_web", project),
		"iap_web",
		"google_iap_web_iam_member", nil)

	g.addIapSettingsResourceWithCheck(&globalResources, iapService,
		fmt.Sprintf("projects/%s", project),
		"project",
		map[string]string{})

	g.addIamMemberResourcesWithPolicyCheck(&globalResources, iapService,
		fmt.Sprintf("projects/%s/iap_tunnel", project),
		"iap_tunnel",
		"google_iap_tunnel_iam_member", nil)

	return globalResources, nil
}

// initRegionalIapResources initializes IAP resources that are regional or zonal.
func (g *IapGenerator) initRegionalIapResources(ctx context.Context, iapService *iap.Service, project, region string) ([]terraformutils.Resource, error) {
	var regionalResources []terraformutils.Resource
	var parent string

	computeService, err := compute.NewService(ctx)
	if err == nil {
		_ = computeService.RegionBackendServices.List(project, region).Pages(ctx, func(page *compute.BackendServiceList) error {
			for _, backendService := range page.Items {
				g.addIamMemberAndSettingsResourcesWithPolicyCheck(&regionalResources, iapService,
					fmt.Sprintf("projects/%s/iap_web/compute-%s/services/%s", project, region, backendService.Name),
					fmt.Sprintf("%s-%s", region, backendService.Name),
					"google_iap_web_region_backend_service_iam_member",
					map[string]string{
						"region":                     region,
						"web_region_backend_service": backendService.Name,
					})
			}
			return nil
		})

		_ = computeService.Instances.AggregatedList(project).Pages(ctx, func(page *compute.InstanceAggregatedList) error {
			for zone, instancesScopedList := range page.Items {
				zoneName := zone[strings.LastIndex(zone, "/")+1:]
				if !strings.HasPrefix(zoneName, region) {
					continue
				}
				g.addIamMemberResourcesWithPolicyCheck(&regionalResources, iapService,
					fmt.Sprintf("projects/%s/iap_tunnel/zones/%s", project, zoneName),
					zoneName,
					"google_iap_tunnel_iam_member",
					map[string]string{"zone": zoneName})

				for _, instance := range instancesScopedList.Instances {
					g.addIamMemberResourcesWithPolicyCheck(&regionalResources, iapService,
						fmt.Sprintf("projects/%s/iap_tunnel/zones/%s/instances/%s", project, zoneName, instance.Name),
						instance.Name,
						"google_iap_tunnel_instance_iam_member",
						map[string]string{
							"zone":     zoneName,
							"instance": instance.Name,
						})
				}
			}
			return nil
		})

	}

	runService, err := run.NewService(ctx)
	if err == nil {
		parent = "projects/" + project + "/locations/" + region
		listCall := runService.Projects.Locations.Services.List(parent)
		// Manual pagination for Cloud Run v1
		for {
			resp, err := listCall.Do()
			if err != nil {
				log.Printf("[ERROR] Failed to list Cloud Run services: %v", err)
				break
			}
			for _, service := range resp.Items {
				if service.Metadata == nil {
					continue
				}
				g.addIamMemberAndSettingsResourcesWithPolicyCheck(&regionalResources, iapService,
					fmt.Sprintf("projects/%s/iap_web/cloud_run-%s/services/%s", project, region, service.Metadata.Name),
					service.Metadata.Name,
					"google_iap_web_cloud_run_service_iam_member",
					map[string]string{
						"location": region,
						"service":  service.Metadata.Name,
					})
			}
			if resp.Metadata == nil || resp.Metadata.Continue == "" {
				break
			}
			listCall.Continue(resp.Metadata.Continue)
		}
	}

	parent = fmt.Sprintf("projects/%s/iap_tunnel/locations/%s", project, region)
	g.addIamMemberResourcesWithPolicyCheck(&regionalResources, iapService,
		parent,
		region,
		"google_iap_tunnel_iam_member",
		map[string]string{"region": region})

	_ = iapService.Projects.IapTunnel.Locations.DestGroups.List(parent).Pages(ctx, func(page *iap.ListTunnelDestGroupsResponse) error {
		for _, destGroup := range page.TunnelDestGroups {
			t := strings.Split(destGroup.Name, "/")
			name := t[len(t)-1]
			resourceID := fmt.Sprintf("projects/%s/iap_tunnel/locations/%s/destGroups/%s", project, region, name)
			regionalResources = append(regionalResources, terraformutils.NewResource(
				resourceID,
				name,
				"google_iap_tunnel_dest_group",
				g.ProviderName,
				map[string]string{
					"project":    project,
					"region":     region,
					"group_name": name,
				},
				[]string{},
				map[string]interface{}{},
			))

			g.addIamMemberResourcesWithPolicyCheck(&regionalResources, iapService,
				resourceID,
				name,
				"google_iap_tunnel_dest_group_iam_member",
				map[string]string{
					"region":     region,
					"dest_group": name,
				})
		}
		return nil
	})

	return regionalResources, nil
}

// InitResources fetches all IAP resources, dispatching to global or regional handlers.
func (g *IapGenerator) InitResources() error {
	project := g.GetArgs()["project"].(string)
	regionName := g.GetArgs()["region"].(compute.Region).Name
	ctx := context.Background()
	iapService, err := iap.NewService(ctx)
	if err != nil {
		return err
	}

	if regionName == "global" || regionName == "" {
		resources, err := g.initGlobalIapResources(ctx, iapService, project)
		if err != nil {
			return err
		}
		g.Resources = resources
	} else {
		resources, err := g.initRegionalIapResources(ctx, iapService, project, regionName)
		if err != nil {
			return err
		}
		g.Resources = resources
	}

	return nil
}
