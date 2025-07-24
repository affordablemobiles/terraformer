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
	"google.golang.org/api/appengine/v1"
	"google.golang.org/api/compute/v1"
)

// AppEngineGenerator generates Terraform resources for App Engine.
type AppEngineGenerator struct {
	GCPService
}

// InitResources initializes the App Engine resources.
func (g *AppEngineGenerator) InitResources() error {
	if g.GetArgs()["region"].(compute.Region).Name != "" && g.GetArgs()["region"].(compute.Region).Name != "global" {
		return nil
	}

	project := g.GetArgs()["project"].(string)
	ctx := context.Background()
	appengineService, err := appengine.NewService(ctx)
	if err != nil {
		return fmt.Errorf("failed to create appengine service: %w", err)
	}

	// First, check if an App Engine application exists. All other resources depend on this.
	app, err := appengineService.Apps.Get(project).Do()
	if err != nil {
		return fmt.Errorf("failed to get app engine application for project %s: %w", project, err)
	}

	g.initApplication(app, project)
	g.initApplicationURLDispatchRules(app, project)

	if err := g.initDomainMappings(ctx, appengineService, project); err != nil {
		return err
	}
	if err := g.initFirewallRules(ctx, appengineService, project); err != nil {
		return err
	}
	if err := g.initServiceNetworkSettings(ctx, appengineService, project); err != nil {
		return err
	}

	return nil
}

func (g *AppEngineGenerator) initApplication(app *appengine.Application, project string) {
	g.Resources = append(g.Resources, terraformutils.NewResource(
		project,
		project,
		"google_app_engine_application",
		g.ProviderName,
		map[string]string{
			"project":     project,
			"location_id": app.LocationId,
		},
		[]string{},
		map[string]interface{}{},
	))
}

func (g *AppEngineGenerator) initApplicationURLDispatchRules(app *appengine.Application, project string) {
	if len(app.DispatchRules) > 0 {
		id := fmt.Sprintf("apps/%s", project)
		g.Resources = append(g.Resources, terraformutils.NewResource(
			id,
			project,
			"google_app_engine_application_url_dispatch_rules",
			g.ProviderName,
			map[string]string{
				"project": project,
			},
			[]string{},
			map[string]interface{}{},
		))
	}
}

func (g *AppEngineGenerator) initDomainMappings(ctx context.Context, appengineService *appengine.APIService, project string) error {
	req := appengineService.Apps.DomainMappings.List(project)
	if err := req.Pages(ctx, func(page *appengine.ListDomainMappingsResponse) error {
		for _, mapping := range page.DomainMappings {
			// The `name` field is the full ID for the resource.
			// e.g., apps/my-project/domainMappings/example.com
			parts := strings.Split(mapping.Name, "/")
			domainName := parts[len(parts)-1]
			g.Resources = append(g.Resources, terraformutils.NewResource(
				mapping.Name,
				domainName, // Use the domain name as the resource name in the .tf file
				"google_app_engine_domain_mapping",
				g.ProviderName,
				map[string]string{
					"project":     project,
					"domain_name": domainName,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list app engine domain mappings: %w", err)
	}
	return nil
}

func (g *AppEngineGenerator) initFirewallRules(ctx context.Context, appengineService *appengine.APIService, project string) error {
	req := appengineService.Apps.Firewall.IngressRules.List(project)
	if err := req.Pages(ctx, func(page *appengine.ListIngressRulesResponse) error {
		for _, rule := range page.IngressRules {
			// The ID for terraform import is `apps/{project}/firewall/ingressRules/{priority}`
			id := fmt.Sprintf("apps/%s/firewall/ingressRules/%d", project, rule.Priority)
			resourceName := fmt.Sprintf("%s-%d", project, rule.Priority) // A friendly name for the file
			g.Resources = append(g.Resources, terraformutils.NewResource(
				id,
				resourceName,
				"google_app_engine_firewall_rule",
				g.ProviderName,
				map[string]string{
					"project":  project,
					"priority": fmt.Sprintf("%d", rule.Priority),
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list app engine firewall rules: %w", err)
	}
	return nil
}

func (g *AppEngineGenerator) initServiceNetworkSettings(ctx context.Context, appengineService *appengine.APIService, project string) error {
	servicesReq := appengineService.Apps.Services.List(project)
	if err := servicesReq.Pages(ctx, func(page *appengine.ListServicesResponse) error {
		for _, service := range page.Services {
			// The resource is only for standard environment, and only if network settings are configured.
			if service.NetworkSettings != nil {
				parts := strings.Split(service.Name, "/")
				serviceID := parts[len(parts)-1]
				resourceName := fmt.Sprintf("%s-%s-network-settings", project, serviceID)
				g.Resources = append(g.Resources, terraformutils.NewResource(
					service.Name, // This is the full ID for import
					resourceName,
					"google_app_engine_service_network_settings",
					g.ProviderName,
					map[string]string{
						"project": project,
						"service": serviceID,
					},
					[]string{},
					map[string]interface{}{},
				))
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list app engine services: %w", err)
	}
	return nil
}

func (g *AppEngineGenerator) PostConvertHook() error {
	for i, resource := range g.Resources {
		if resource.InstanceInfo.Type == "google_app_engine_application_url_dispatch_rules" {
			// Tell the HCL printer to preserve the order for both the list of 'step' blocks
			// and the 'args' list found within ANY of those steps.
			g.Resources[i].PreserveOrder = []string{"dispatch_rules"}
		}
	}

	return nil
}
