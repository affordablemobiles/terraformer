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
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/vpcaccess/v1"
)

// VpcAccessConnectorGenerator holds the logic for generating google_vpc_access_connector resources.
type VpcAccessConnectorGenerator struct {
	GCPService
}

// InitResources fetches all VPC Access Connectors for a given project and region.
func (g *VpcAccessConnectorGenerator) InitResources() error {
	project := g.GetArgs()["project"].(string)
	region := g.GetArgs()["region"].(compute.Region).Name
	if region == "" || region == "global" {
		// VPC Access Connectors are regional resources, so we skip the global region.
		return nil
	}

	ctx := context.Background()
	vpcaccessService, err := vpcaccess.NewService(ctx)
	if err != nil {
		return err
	}

	parent := "projects/" + project + "/locations/" + region
	req := vpcaccessService.Projects.Locations.Connectors.List(parent)

	var resources []terraformutils.Resource
	err = req.Pages(ctx, func(page *vpcaccess.ListConnectorsResponse) error {
		for _, connector := range page.Connectors {
			// The API returns the full resource name, so we need to extract the short name.
			t := strings.Split(connector.Name, "/")
			name := t[len(t)-1]

			resources = append(resources, terraformutils.NewResource(
				connector.Name,
				name,
				"google_vpc_access_connector",
				g.ProviderName,
				map[string]string{
					"name":    name,
					"project": project,
					"region":  region,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	})
	if err != nil {
		return err
	}

	g.Resources = resources
	return nil
}
