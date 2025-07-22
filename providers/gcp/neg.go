// Copyright 2021 The Terraformer Authors.
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
	"google.golang.org/api/compute/v1"
)

// NEGGenerator is a custom generator for Network Endpoint Groups.
// It handles both Zonal and Regional NEGs, as well as their respective endpoints.
type NEGGenerator struct {
	GCPService
}

// InitResources fetches all NEG-related resources for the specified project and regions/zones.
func (g *NEGGenerator) InitResources() error {
	if g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global" {
		return nil
	}

	project := g.GetArgs()["project"].(string)
	region := g.GetArgs()["region"].(compute.Region).Name

	ctx := context.Background()
	computeService, err := compute.NewService(ctx)
	if err != nil {
		return fmt.Errorf("failed to create compute service: %w", err)
	}

	// Handle Regional NEGs for the current region
	if err := g.initRegionalNEGs(ctx, computeService, project, region); err != nil {
		log.Printf("Error initializing regional NEGs in %s: %v", region, err)
	}

	// Handle Zonal NEGs for each zone in the current region.
	for _, zoneLink := range g.GetArgs()["region"].(compute.Region).Zones {
		t := strings.Split(zoneLink, "/")
		zone := t[len(t)-1]
		if err := g.initZonalNEGs(ctx, computeService, project, zone); err != nil {
			log.Printf("Error initializing zonal NEGs in %s: %v", zone, err)
		}
	}

	return nil
}

// initRegionalNEGs fetches regional NEGs and their endpoints.
func (g *NEGGenerator) initRegionalNEGs(ctx context.Context, computeService *compute.Service, project, region string) error {
	req := computeService.RegionNetworkEndpointGroups.List(project, region)
	if err := req.Pages(ctx, func(page *compute.NetworkEndpointGroupList) error {
		for _, neg := range page.Items {
			// Construct the correct ID format for import
			id := fmt.Sprintf("projects/%s/regions/%s/networkEndpointGroups/%s", project, region, neg.Name)

			g.Resources = append(g.Resources, terraformutils.NewResource(
				id,
				terraformutils.TfSanitize(neg.Name+"_"+region),
				"google_compute_region_network_endpoint_group",
				g.ProviderName,
				map[string]string{
					"name":    neg.Name,
					"region":  region,
					"project": project,
				},
				[]string{},
				map[string]interface{}{},
			))

			// Serverless NEGs do not support listing endpoints.
			if neg.NetworkEndpointType == "SERVERLESS" {
				continue
			}

			// Fetch and create resources for the endpoints within this NEG
			if err := g.initRegionalEndpoints(ctx, computeService, project, region, neg.Name); err != nil {
				log.Printf("Failed to initialize regional endpoints for NEG %s in region %s: %v", neg.Name, region, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list regional network endpoint groups for region %s: %w", region, err)
	}
	return nil
}

// initRegionalEndpoints fetches endpoints for a specific regional NEG.
func (g *NEGGenerator) initRegionalEndpoints(ctx context.Context, computeService *compute.Service, project, region, negName string) error {
	req := computeService.RegionNetworkEndpointGroups.ListNetworkEndpoints(project, region, negName)
	if err := req.Pages(ctx, func(page *compute.NetworkEndpointGroupsListNetworkEndpoints) error {
		for _, endpoint := range page.Items {
			if endpoint.NetworkEndpoint == nil {
				continue
			}

			ipAddress := endpoint.NetworkEndpoint.IpAddress
			fqdn := endpoint.NetworkEndpoint.Fqdn
			port := endpoint.NetworkEndpoint.Port

			// Construct a unique ID for the endpoint resource
			var id, name string
			if fqdn != "" {
				id = fmt.Sprintf("projects/%s/regions/%s/networkEndpointGroups/%s/%s/%d", project, region, negName, fqdn, port)
				name = fmt.Sprintf("%s-%s-%d", negName, fqdn, port)
			} else {
				id = fmt.Sprintf("projects/%s/regions/%s/networkEndpointGroups/%s/%s/%d", project, region, negName, ipAddress, port)
				name = fmt.Sprintf("%s-%s-%d", negName, ipAddress, port)
			}

			g.Resources = append(g.Resources, terraformutils.NewResource(
				id,
				terraformutils.TfSanitize(name),
				"google_compute_region_network_endpoint",
				g.ProviderName,
				map[string]string{
					"region_network_endpoint_group": negName,
					"port":                          fmt.Sprintf("%d", port),
					"ip_address":                    ipAddress,
					"fqdn":                          fqdn,
					"region":                        region,
					"project":                       project,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list regional network endpoints for NEG %s in region %s: %w", negName, region, err)
	}
	return nil
}

// initZonalNEGs fetches zonal NEGs and their endpoints.
func (g *NEGGenerator) initZonalNEGs(ctx context.Context, computeService *compute.Service, project, zone string) error {
	req := computeService.NetworkEndpointGroups.List(project, zone)
	if err := req.Pages(ctx, func(page *compute.NetworkEndpointGroupList) error {
		for _, neg := range page.Items {
			// Construct the correct ID format for import
			id := fmt.Sprintf("projects/%s/zones/%s/networkEndpointGroups/%s", project, zone, neg.Name)

			g.Resources = append(g.Resources, terraformutils.NewResource(
				id,
				terraformutils.TfSanitize(neg.Name+"_"+zone),
				"google_compute_network_endpoint_group",
				g.ProviderName,
				map[string]string{
					"name":    neg.Name,
					"zone":    zone,
					"project": project,
				},
				[]string{},
				map[string]interface{}{},
			))

			// Fetch and create resources for the endpoints within this NEG
			if err := g.initZonalEndpoints(ctx, computeService, project, zone, neg.Name); err != nil {
				log.Printf("Failed to initialize zonal endpoints for NEG %s in zone %s: %v", neg.Name, zone, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list zonal network endpoint groups for zone %s: %w", zone, err)
	}
	return nil
}

// initZonalEndpoints fetches endpoints for a specific zonal NEG.
func (g *NEGGenerator) initZonalEndpoints(ctx context.Context, computeService *compute.Service, project, zone, negName string) error {
	req := computeService.NetworkEndpointGroups.ListNetworkEndpoints(project, zone, negName, &compute.NetworkEndpointGroupsListEndpointsRequest{})
	if err := req.Pages(ctx, func(page *compute.NetworkEndpointGroupsListNetworkEndpoints) error {
		for _, endpoint := range page.Items {
			if endpoint.NetworkEndpoint == nil {
				continue
			}

			instanceURL := endpoint.NetworkEndpoint.Instance
			instanceName := instanceURL
			if parts := strings.Split(instanceURL, "/"); len(parts) > 0 {
				instanceName = parts[len(parts)-1]
			}

			// ignore GKE managed NEGs
			if strings.HasPrefix(instanceName, "gke-") {
				continue
			}

			ipAddress := endpoint.NetworkEndpoint.IpAddress
			port := endpoint.NetworkEndpoint.Port

			// Construct a unique ID and name for the endpoint resource
			id := fmt.Sprintf("projects/%s/zones/%s/networkEndpointGroups/%s/%s/%s/%d", project, zone, negName, instanceName, ipAddress, port)
			name := fmt.Sprintf("%s-%s-%s-%d", negName, instanceName, ipAddress, port)

			g.Resources = append(g.Resources, terraformutils.NewResource(
				id,
				terraformutils.TfSanitize(name),
				"google_compute_network_endpoint",
				g.ProviderName,
				map[string]string{
					"network_endpoint_group": negName,
					"instance":               instanceName,
					"port":                   fmt.Sprintf("%d", port),
					"ip_address":             ipAddress,
					"zone":                   zone,
					"project":                project,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list zonal network endpoints for NEG %s in zone %s: %w", negName, zone, err)
	}
	return nil
}
