// Copyright 2018 The Terraformer Authors.
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

// AUTO-GENERATED CODE. DO NOT EDIT.
package gcp

import (
	"context"
	"log"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"

	"google.golang.org/api/compute/v1"
)

var instanceGroupsAllowEmptyValues = []string{""}

var instanceGroupsAdditionalFields = map[string]interface{}{}

type InstanceGroupsGenerator struct {
	GCPService
}

// Run on instanceGroupsList and create for each TerraformResource
func (g InstanceGroupsGenerator) createResources(ctx context.Context, instanceGroupsList *compute.InstanceGroupsListCall, zone string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	if err := instanceGroupsList.Pages(ctx, func(page *compute.InstanceGroupList) error {
		for _, obj := range page.Items {
			resources = append(resources, terraformutils.NewResource(
				zone+"/"+obj.Name,
				zone+"/"+obj.Name,
				"google_compute_instance_group",
				g.ProviderName,
				map[string]string{
					"name":    obj.Name,
					"project": g.GetArgs()["project"].(string),
					"region":  g.GetArgs()["region"].(compute.Region).Name,
					"zone":    zone,
				},
				instanceGroupsAllowEmptyValues,
				instanceGroupsAdditionalFields,
			))
		}
		return nil
	}); err != nil {
		log.Println(err)
	}
	return resources
}

// Generate TerraformResources from GCP API,
// from each instanceGroups create 1 TerraformResource
// Need instanceGroups name as ID for terraform resource
func (g *InstanceGroupsGenerator) InitResources() error {

	if g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global" {
		return nil
	}

	ctx := context.Background()
	computeService, err := compute.NewService(ctx)
	if err != nil {
		return err
	}

	for _, zoneLink := range g.GetArgs()["region"].(compute.Region).Zones {
		t := strings.Split(zoneLink, "/")
		zone := t[len(t)-1]
		instanceGroupsList := computeService.InstanceGroups.List(g.GetArgs()["project"].(string), zone)
		g.Resources = append(g.Resources, g.createResources(ctx, instanceGroupsList, zone)...)
	}

	return nil

}
