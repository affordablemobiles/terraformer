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
	"log"
	"regexp"

	admin "cloud.google.com/go/iam/admin/apiv1"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/iterator"
	adminpb "google.golang.org/genproto/googleapis/iam/admin/v1"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
)

var IamAllowEmptyValues = []string{"tags."}

var IamAdditionalFields = map[string]interface{}{}

type IamGenerator struct {
	GCPService
}

func (g IamGenerator) createServiceAccountResources(serviceAccountsIterator *admin.ServiceAccountIterator, project string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	re := regexp.MustCompile(`^[a-z]`)
	for {
		serviceAccount, err := serviceAccountsIterator.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Println("error with service account:", err)
			continue
		}
		if !re.MatchString(serviceAccount.Email) {
			log.Printf("skipping %s: service account email must start with [a-z]\n", serviceAccount.Name)
			continue
		}
		resources = append(resources, terraformutils.NewSimpleResource(
			serviceAccount.Name,
			serviceAccount.UniqueId,
			"google_service_account",
			g.ProviderName,
			IamAllowEmptyValues,
		))

		// Fetch IAM policy for the service account
		if err := g.createServiceAccountIamPolicyResources(serviceAccount.Name, serviceAccount.UniqueId, project, &resources); err != nil {
			log.Printf("error fetching iam policy for service account %s: %v", serviceAccount.Name, err)
		}
	}
	return resources
}

func (g *IamGenerator) createServiceAccountIamPolicyResources(serviceAccountName, serviceAccountID, project string, resources *[]terraformutils.Resource) error {
	ctx := context.Background()
	client, err := admin.NewIamClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	// Use iam/v1 API for GetIamPolicy as admin client might not expose it directly or uses different types
	// Actually, admin client has GetIamPolicy but it takes a resource string.
	// Let's use the raw API service which is easier for Policy handling in this codebase context
	iamService, err := iam.NewService(ctx)
	if err != nil {
		return err
	}

	policy, err := iamService.Projects.ServiceAccounts.GetIamPolicy(serviceAccountName).Do()
	if err != nil {
		return err
	}

	for _, binding := range policy.Bindings {
		attributes := map[string]string{
			"service_account_id": serviceAccountName,
		}
		conditionTitle := ""
		conditionDescription := ""
		conditionExpression := ""
		if binding.Condition != nil {
			conditionTitle = binding.Condition.Title
			conditionDescription = binding.Condition.Description
			conditionExpression = binding.Condition.Expression
		}
		*resources = append(*resources, g.CreateIamMemberResources(serviceAccountName, serviceAccountName, "google_service_account_iam_member", attributes, binding.Role, binding.Members, conditionTitle, conditionDescription, conditionExpression)...)
	}
	return nil
}

func (g *IamGenerator) createIamCustomRoleResources(rolesResponse *adminpb.ListRolesResponse, project string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	for _, role := range rolesResponse.Roles {
		if role.Deleted {
			// Note: no need to log that the resource has been deleted
			continue
		}
		resources = append(resources, terraformutils.NewResource(
			role.Name,
			role.Name,
			"google_project_iam_custom_role",
			g.ProviderName,
			map[string]string{
				"role_id": role.Name,
				"project": project,
			},
			IamAllowEmptyValues,
			map[string]interface{}{
				"stage": role.Stage.String(),
			},
		))
	}

	return resources
}

func (g *IamGenerator) createIamMemberResources(policy *cloudresourcemanager.Policy, project string) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	for _, b := range policy.Bindings {
		attributes := map[string]string{
			"project": project,
		}
		conditionTitle := ""
		conditionDescription := ""
		conditionExpression := ""
		if b.Condition != nil {
			conditionTitle = b.Condition.Title
			conditionDescription = b.Condition.Description
			conditionExpression = b.Condition.Expression
		}
		resources = append(resources, g.CreateIamMemberResources(project, project, "google_project_iam_member", attributes, b.Role, b.Members, conditionTitle, conditionDescription, conditionExpression)...)
	}

	return resources
}

func (g *IamGenerator) InitResources() error {
	if g.GetArgs()["region"].(compute.Region).Name != "" && g.GetArgs()["region"].(compute.Region).Name != "global" {
		return nil
	}

	ctx := context.Background()

	projectID := g.GetArgs()["project"].(string)
	client, err := admin.NewIamClient(ctx)
	if err != nil {
		return err
	}
	serviceAccountsIterator := client.ListServiceAccounts(ctx, &adminpb.ListServiceAccountsRequest{Name: "projects/" + projectID})
	rolesResponse, err := client.ListRoles(ctx, &adminpb.ListRolesRequest{Parent: "projects/" + projectID})
	if err != nil {
		return err
	}

	cm, err := cloudresourcemanager.NewService(context.Background())
	if err != nil {
		return err
	}
	rb := &cloudresourcemanager.GetIamPolicyRequest{
		Options: &cloudresourcemanager.GetPolicyOptions{
			RequestedPolicyVersion: 3,
		},
	}
	policyResponse, err := cm.Projects.GetIamPolicy(projectID, rb).Context(context.Background()).Do()
	if err != nil {
		return err
	}

	g.Resources = g.createServiceAccountResources(serviceAccountsIterator, projectID)
	g.Resources = append(g.Resources, g.createIamCustomRoleResources(rolesResponse, projectID)...)
	g.Resources = append(g.Resources, g.createIamMemberResources(policyResponse, projectID)...)
	return nil
}
