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
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/dns/v1"
)

var cloudDNSAllowEmptyValues = []string{}

var cloudDNSAdditionalFields = map[string]interface{}{}

type CloudDNSGenerator struct {
	GCPService
}

func (g *CloudDNSGenerator) createZonesResources(ctx context.Context, svc *dns.Service, project string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	managedZonesListCall := svc.ManagedZones.List(project)
	err := managedZonesListCall.Pages(ctx, func(listDNS *dns.ManagedZonesListResponse) error {
		for _, zone := range listDNS.ManagedZones {
			resources = append(resources, terraformutils.NewResource(
				zone.Name,
				zone.Name,
				"google_dns_managed_zone",
				g.ProviderName,
				map[string]string{
					"name":    zone.Name,
					"project": project,
				},
				cloudDNSAllowEmptyValues,
				cloudDNSAdditionalFields,
			))
			records := g.createRecordsResources(ctx, svc, project, zone.Name)
			resources = append(resources, records...)
		}
		return nil
	})
	if err != nil {
		log.Println(err)
		return []terraformutils.Resource{}
	}
	return resources
}

func (g *CloudDNSGenerator) createRecordsResources(ctx context.Context, svc *dns.Service, project, zoneName string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	managedRecordsListCall := svc.ResourceRecordSets.List(project, zoneName)
	err := managedRecordsListCall.Pages(ctx, func(listDNS *dns.ResourceRecordSetsListResponse) error {
		for _, record := range listDNS.Rrsets {
			resources = append(resources, terraformutils.NewResource(
				fmt.Sprintf("%s/%s/%s", zoneName, record.Name, record.Type),
				zoneName+"_"+strings.TrimSuffix(record.Name+"-"+record.Type, "."),
				"google_dns_record_set",
				g.ProviderName,
				map[string]string{
					"name":         record.Name,
					"managed_zone": zoneName,
					"type":         record.Type,
					"project":      project,
				},
				cloudDNSAllowEmptyValues,
				cloudDNSAdditionalFields,
			))
		}
		return nil
	})
	if err != nil {
		log.Println(err)
	}
	return resources
}

func (g *CloudDNSGenerator) createPoliciesResources(ctx context.Context, svc *dns.Service, project string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	policiesListCall := svc.Policies.List(project)
	err := policiesListCall.Pages(ctx, func(listDNS *dns.PoliciesListResponse) error {
		for _, policy := range listDNS.Policies {
			resources = append(resources, terraformutils.NewResource(
				policy.Name,
				policy.Name,
				"google_dns_policy",
				g.ProviderName,
				map[string]string{
					"name":    policy.Name,
					"project": project,
				},
				cloudDNSAllowEmptyValues,
				cloudDNSAdditionalFields,
			))
		}
		return nil
	})
	if err != nil {
		log.Println(err)
	}
	return resources
}

func (g *CloudDNSGenerator) createResponsePoliciesResources(ctx context.Context, svc *dns.Service, project string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	responsePoliciesListCall := svc.ResponsePolicies.List(project)
	err := responsePoliciesListCall.Pages(ctx, func(listDNS *dns.ResponsePoliciesListResponse) error {
		for _, responsePolicy := range listDNS.ResponsePolicies {
			resources = append(resources, terraformutils.NewResource(
				strconv.FormatInt(responsePolicy.Id, 10),
				responsePolicy.ResponsePolicyName,
				"google_dns_response_policy",
				g.ProviderName,
				map[string]string{
					"response_policy_name": responsePolicy.ResponsePolicyName,
					"project":              project,
				},
				cloudDNSAllowEmptyValues,
				cloudDNSAdditionalFields,
			))
			rules := g.createResponsePolicyRulesResources(ctx, svc, project, responsePolicy.ResponsePolicyName)
			resources = append(resources, rules...)
		}
		return nil
	})
	if err != nil {
		log.Println(err)
	}
	return resources
}

func (g *CloudDNSGenerator) createResponsePolicyRulesResources(ctx context.Context, svc *dns.Service, project, responsePolicyName string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	responsePolicyRulesListCall := svc.ResponsePolicyRules.List(project, responsePolicyName)
	err := responsePolicyRulesListCall.Pages(ctx, func(listDNS *dns.ResponsePolicyRulesListResponse) error {
		for _, rule := range listDNS.ResponsePolicyRules {
			resources = append(resources, terraformutils.NewResource(
				fmt.Sprintf("projects/%s/responsePolicies/%s/rules/%s", project, responsePolicyName, rule.RuleName),
				responsePolicyName+"_"+rule.RuleName,
				"google_dns_response_policy_rule",
				g.ProviderName,
				map[string]string{
					"response_policy": responsePolicyName,
					"rule_name":       rule.RuleName,
					"project":         project,
				},
				cloudDNSAllowEmptyValues,
				cloudDNSAdditionalFields,
			))
		}
		return nil
	})
	if err != nil {
		log.Println(err)
	}
	return resources
}

// Generate TerraformResources from GCP API,
// create terraform resource for each zone + each record
func (g *CloudDNSGenerator) InitResources() error {
	if g.GetArgs()["region"].(compute.Region).Name != "" && g.GetArgs()["region"].(compute.Region).Name != "global" {
		return nil
	}

	project := g.GetArgs()["project"].(string)
	ctx := context.Background()
	svc, err := dns.NewService(ctx)
	if err != nil {
		return err
	}

	g.Resources = append(g.Resources, g.createZonesResources(ctx, svc, project)...)
	g.Resources = append(g.Resources, g.createPoliciesResources(ctx, svc, project)...)
	g.Resources = append(g.Resources, g.createResponsePoliciesResources(ctx, svc, project)...)

	return nil
}

func (g *CloudDNSGenerator) PostConvertHook() error {
	for i, resourceRecord := range g.Resources {
		if resourceRecord.InstanceInfo.Type != "google_dns_record_set" {
			continue
		}
		item := resourceRecord.Item
		zoneID := item["managed_zone"].(string)
		for _, resourceZone := range g.Resources {
			if resourceZone.InstanceInfo.Type != "google_dns_managed_zone" {
				continue
			}
			if zoneID == resourceZone.InstanceState.ID {
				g.Resources[i].Item["managed_zone"] = "${google_dns_managed_zone." + resourceZone.ResourceName + ".name}"
				name := g.Resources[i].Item["name"].(string)
				name = strings.ReplaceAll(name, resourceZone.Item["dns_name"].(string), "")
				g.Resources[i].Item["name"] = name + "${google_dns_managed_zone." + resourceZone.ResourceName + ".dns_name}"
			}
		}
	}
	return nil
}
