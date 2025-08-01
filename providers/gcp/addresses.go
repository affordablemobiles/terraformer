// Copyright 2018 The Terraformer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package gcp

import (
	"context"
	"log"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"

	"google.golang.org/api/compute/v1"
)

var addressesAllowEmptyValues = []string{""}

var addressesAdditionalFields = map[string]interface{}{}

type AddressesGenerator struct {
	GCPService
}

// Run on addressesList and create for each TerraformResource
func (g AddressesGenerator) createResources(ctx context.Context, addressesList *compute.AddressesListCall) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	if err := addressesList.Pages(ctx, func(page *compute.AddressList) error {
		for _, obj := range page.Items {
			// Ignore serverless IPs in use by Cloud Run.
			if obj.Purpose == "SERVERLESS" {
				continue
			}

			resources = append(resources, terraformutils.NewResource(
				obj.Name,
				obj.Name,
				"google_compute_address",
				g.ProviderName,
				map[string]string{
					"name":    obj.Name,
					"project": g.GetArgs()["project"].(string),
					"region":  g.GetArgs()["region"].(compute.Region).Name,
				},
				addressesAllowEmptyValues,
				addressesAdditionalFields,
			))
		}
		return nil
	}); err != nil {
		log.Println(err)
	}
	return resources
}

// Generate TerraformResources from GCP API,
// from each addresses create 1 TerraformResource
// Need addresses name as ID for terraform resource
func (g *AddressesGenerator) InitResources() error {

	if g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global" {
		return nil
	}

	ctx := context.Background()
	computeService, err := compute.NewService(ctx)
	if err != nil {
		return err
	}

	addressesList := computeService.Addresses.List(g.GetArgs()["project"].(string), g.GetArgs()["region"].(compute.Region).Name)
	g.Resources = g.createResources(ctx, addressesList)

	return nil

}
