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

package gcp

import (
	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
)

var projectAllowEmptyValues = []string{""}

var projectAdditionalFields = map[string]interface{}{}

type ProjectGenerator struct {
	GCPService
}

// Generate TerraformResources from GCP API,
func (g *ProjectGenerator) InitResources() error {
	if g.GetArgs()["region"].(compute.Region).Name != "" && g.GetArgs()["region"].(compute.Region).Name != "global" {
		return nil
	}

	g.Resources = append(g.Resources, terraformutils.NewResource(
		g.GetArgs()["project"].(string),
		g.GetArgs()["project"].(string),
		"google_project",
		g.ProviderName,
		map[string]string{
			"auto_create_network": "true",
		},
		projectAllowEmptyValues,
		projectAdditionalFields,
	))

	return nil
}
