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
	"fmt"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
)

type GCPService struct { //nolint
	terraformutils.Service
}

func (s *GCPService) applyCustomProviderType(resources []terraformutils.Resource, providerName string) []terraformutils.Resource {
	editedResources := []terraformutils.Resource{}
	for _, r := range resources {
		r.Item["provider"] = providerName
		editedResources = append(editedResources, r)
	}
	return editedResources
}

// CreateIamMemberResources creates terraform resources for each member of a role binding.
// It handles conditional bindings by appending the condition title to the resource name.
func (s *GCPService) CreateIamMemberResources(resourceID, resourceName, resourceType string, attributes map[string]string, role string, members []string, conditionTitle, conditionDescription, conditionExpression string) []terraformutils.Resource {
	var resources []terraformutils.Resource
	for _, member := range members {
		memberAttributes := map[string]string{
			"role":   role,
			"member": member,
		}
		for k, v := range attributes {
			memberAttributes[k] = v
		}

		var memberResourceID string
		// The terraform provider expects the import ID for IAM members to be space-delimited.
		if conditionTitle != "" {
			// For conditional bindings, the condition title is the fourth part of the ID.
			memberResourceID = fmt.Sprintf("%s %s %s %s", resourceID, role, member, conditionTitle)
			memberAttributes["condition.#"] = "1"
			memberAttributes["condition.0.title"] = conditionTitle
			memberAttributes["condition.0.description"] = conditionDescription
			memberAttributes["condition.0.expression"] = conditionExpression
		} else {
			memberResourceID = fmt.Sprintf("%s %s %s", resourceID, role, member)
		}

		memberResourceName := fmt.Sprintf("%s-%s-%s", resourceName, terraformutils.TfSanitize(role), terraformutils.TfSanitize(member))
		if conditionTitle != "" {
			memberResourceName = fmt.Sprintf("%s-%s", memberResourceName, terraformutils.TfSanitize(conditionTitle))
		}

		resources = append(resources, terraformutils.NewResource(
			memberResourceID,
			memberResourceName,
			resourceType,
			s.ProviderName,
			memberAttributes,
			[]string{},
			map[string]interface{}{},
		))
	}
	return resources
}
