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
	"log"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
	secretmanager "google.golang.org/api/secretmanager/v1"
	"google.golang.org/api/option"
)

// SecretManagerGenerator is a generator for Secret Manager resources.
type SecretManagerGenerator struct {
	GCPService
}

// createSecretsResources creates Terraformer resources for both global and regional secrets.
func (g *SecretManagerGenerator) createSecretsResources(ctx context.Context, service *secretmanager.Service, parent string, isRegional bool) error {
	processPage := func(page *secretmanager.ListSecretsResponse) error {
		for _, secret := range page.Secrets {
			secretID := g.getSecretID(secret.Name)
			project := g.GetArgs()["project"].(string)

			attributes := map[string]string{
				"project":   project,
				"secret_id": secretID,
			}

			var resourceType, resourceID string
			if isRegional {
				resourceType = "google_secret_manager_regional_secret"
				parts := strings.Split(parent, "/")
				region := parts[3] // parent is projects/{project}/locations/{region}
				attributes["location"] = region
				resourceID = fmt.Sprintf("projects/%s/locations/%s/secrets/%s", project, region, secretID)
			} else {
				resourceType = "google_secret_manager_secret"
				resourceID = fmt.Sprintf("projects/%s/secrets/%s", project, secretID)
			}

			resource := terraformutils.NewResource(
				resourceID,
				secretID,
				resourceType,
				g.ProviderName,
				attributes,
				[]string{},
				map[string]interface{}{},
			)

			g.Resources = append(g.Resources, resource)
			g.addIamMemberResourcesWithPolicyCheck(service, resourceID, secretID, isRegional)
		}
		return nil
	}

	if isRegional {
		req := service.Projects.Locations.Secrets.List(parent)
		if err := req.Pages(ctx, processPage); err != nil {
			return fmt.Errorf("failed to list regional secrets for %s: %w", parent, err)
		}
	} else {
		req := service.Projects.Secrets.List(parent)
		if err := req.Pages(ctx, processPage); err != nil {
			return fmt.Errorf("failed to list global secrets for %s: %w", parent, err)
		}
	}
	return nil
}

// createSecretIamMemberResources creates terraform resources for each member of a role binding.
func (g *SecretManagerGenerator) createSecretIamMemberResources(resourceID, resourceName string, isRegional bool, bindings []*secretmanager.Binding) []terraformutils.Resource {
	var resources []terraformutils.Resource
	var resourceType string
	if isRegional {
		resourceType = "google_secret_manager_regional_secret_iam_member"
	} else {
		resourceType = "google_secret_manager_secret_iam_member"
	}

	for _, binding := range bindings {
		for _, member := range binding.Members {
			attributes := map[string]string{
				"project":   g.GetArgs()["project"].(string),
				"secret_id": resourceName,
				"role":      binding.Role,
				"member":    member,
			}
			if isRegional {
				// Extract region from the resource ID
				parts := strings.Split(resourceID, "/")
				if len(parts) > 3 {
					attributes["location"] = parts[3]
				}
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
func (g *SecretManagerGenerator) addIamMemberResourcesWithPolicyCheck(service *secretmanager.Service, resourceID, resourceName string, isRegional bool) {
	log.Printf("Checking Secret Manager IAM for %s", resourceID)
	var policy *secretmanager.Policy
	var err error

	if isRegional {
		policy, err = service.Projects.Locations.Secrets.GetIamPolicy(resourceID).OptionsRequestedPolicyVersion(3).Do()
	} else {
		policy, err = service.Projects.Secrets.GetIamPolicy(resourceID).OptionsRequestedPolicyVersion(3).Do()
	}

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
		memberResources := g.createSecretIamMemberResources(resourceID, resourceName, isRegional, policy.Bindings)
		g.Resources = append(g.Resources, memberResources...)
	}
}

// getSecretID extracts the secret ID from the full secret name.
// The name format is projects/PROJECT_ID/secrets/SECRET_ID or projects/PROJECT_ID/locations/REGION/secrets/SECRET_ID
func (g *SecretManagerGenerator) getSecretID(name string) string {
	parts := strings.Split(name, "/")
	return parts[len(parts)-1]
}

// InitResources generates the GCP Secret Manager resources.
func (g *SecretManagerGenerator) InitResources() error {
	ctx := context.Background()

	project := g.GetArgs()["project"].(string)
	region := g.GetArgs()["region"].(compute.Region).Name

	var service *secretmanager.Service
	var err error

	if region == "global" || region == "" {
		// Global secrets
		service, err = secretmanager.NewService(ctx)
		if err != nil {
			return fmt.Errorf("failed to create global secret manager service: %w", err)
		}
		parent := "projects/" + project
		if err := g.createSecretsResources(ctx, service, parent, false); err != nil {
			return err
		}
	} else {
		// Regional secrets
		endpoint := fmt.Sprintf("secretmanager.%s.rep.googleapis.com", region)
		service, err = secretmanager.NewService(ctx, option.WithEndpoint(endpoint))
		if err != nil {
			return fmt.Errorf("failed to create regional secret manager service for %s: %w", region, err)
		}
		regionalParent := fmt.Sprintf("projects/%s/locations/%s", project, region)
		if err := g.createSecretsResources(ctx, service, regionalParent, true); err != nil {
			// Some regions might not have the Secret Manager API enabled, so we can ignore those errors.
			if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "LOCATION_UNAVAILABLE") {
				return err
			}
		}
	}

	return nil
}
