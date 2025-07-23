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

package terraformutils

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper"
	"github.com/hashicorp/terraform/configs/configschema"
	"github.com/hashicorp/terraform/terraform"
	"github.com/zclconf/go-cty/cty"
)

type Resource struct {
	InstanceInfo  *terraform.InstanceInfo
	InstanceState *terraform.InstanceState
	Outputs       map[string]*terraform.OutputState `json:",omitempty"`
	ResourceName  string
	Provider      string
	Item          map[string]interface{} `json:",omitempty"`
	// A list of dot-separated paths for attributes where list order must be preserved.
	// e.g., "build.step" or "build.step.args"
	PreserveOrder     []string
	IgnoreKeys        []string               `json:",omitempty"`
	AllowEmptyValues  []string               `json:",omitempty"`
	AdditionalFields  map[string]interface{} `json:",omitempty"`
	SlowQueryRequired bool
	DataFiles         map[string][]byte
}

type ApplicableFilter interface {
	IsApplicable(resourceName string) bool
}

type ResourceFilter struct {
	ApplicableFilter
	ServiceName      string
	FieldPath        string
	AcceptableValues []string
}

func (rf *ResourceFilter) Filter(resource Resource) bool {
	if !rf.IsApplicable(strings.TrimPrefix(resource.InstanceInfo.Type, resource.Provider+"_")) {
		return true
	}
	var vals []interface{}
	switch {
	case rf.FieldPath == "id":
		vals = []interface{}{resource.InstanceState.ID}
	case rf.AcceptableValues == nil:
		var hasField = WalkAndCheckField(rf.FieldPath, resource.InstanceState.Attributes)
		if hasField {
			return true
		}
		return WalkAndCheckField(rf.FieldPath, resource.Item)
	default:
		vals = WalkAndGet(rf.FieldPath, resource.InstanceState.Attributes)
		if len(vals) == 0 {
			vals = WalkAndGet(rf.FieldPath, resource.Item)
		}
	}
	for _, val := range vals {
		for _, acceptableValue := range rf.AcceptableValues {
			if val == acceptableValue {
				return true
			}
		}
	}
	return false
}

func (rf *ResourceFilter) IsApplicable(serviceName string) bool {
	return rf.ServiceName == "" || rf.ServiceName == serviceName
}

func (rf *ResourceFilter) isInitial() bool {
	return rf.FieldPath == "id"
}

func NewResource(id, resourceName, resourceType, provider string,
	attributes map[string]string,
	allowEmptyValues []string,
	additionalFields map[string]interface{}) Resource {
	return Resource{
		ResourceName: TfSanitize(resourceName),
		Item:         nil,
		Provider:     provider,
		InstanceState: &terraform.InstanceState{
			ID:         id,
			Attributes: attributes,
		},
		InstanceInfo: &terraform.InstanceInfo{
			Type: resourceType,
			Id:   fmt.Sprintf("%s.%s", resourceType, TfSanitize(resourceName)),
		},
		AdditionalFields: additionalFields,
		AllowEmptyValues: allowEmptyValues,
	}
}

func NewSimpleResource(id, resourceName, resourceType, provider string, allowEmptyValues []string) Resource {
	return NewResource(
		id,
		resourceName,
		resourceType,
		provider,
		map[string]string{},
		allowEmptyValues,
		map[string]interface{}{},
	)
}

func (r *Resource) Refresh(provider *providerwrapper.ProviderWrapper) {
	var err error
	if r.SlowQueryRequired {
		time.Sleep(200 * time.Millisecond)
	}
	r.InstanceState, err = provider.Refresh(r.InstanceInfo, r.InstanceState)
	if err != nil {
		log.Println(err)
	}
}

func (r Resource) GetIDKey() string {
	if _, exist := r.InstanceState.Attributes["self_link"]; exist {
		return "self_link"
	}
	return "id"
}

func (r *Resource) ParseTFstate(parser Flatmapper, impliedType cty.Type) error {
	attributes, err := parser.Parse(impliedType)
	if err != nil {
		return err
	}

	// add Additional Fields to resource
	for key, value := range r.AdditionalFields {
		attributes[key] = value
	}

	if attributes == nil {
		attributes = map[string]interface{}{} // ensure HCL can represent empty resource correctly
	}

	r.Item = attributes
	return nil
}

func (r *Resource) ConvertTFstate(provider *providerwrapper.ProviderWrapper) error {
	ignoreKeys := []*regexp.Regexp{}
	for _, pattern := range r.IgnoreKeys {
		ignoreKeys = append(ignoreKeys, regexp.MustCompile(pattern))
	}
	allowEmptyValues := []*regexp.Regexp{}
	for _, pattern := range r.AllowEmptyValues {
		if pattern != "" {
			allowEmptyValues = append(allowEmptyValues, regexp.MustCompile(pattern))
		}
	}
	parser := NewFlatmapParser(r.InstanceState.Attributes, ignoreKeys, allowEmptyValues)
	pSchema := provider.GetSchema()
	resourceSchema := pSchema.ResourceTypes[r.InstanceInfo.Type]
	impliedType := resourceSchema.Block.ImpliedType()
	if err := r.ParseTFstate(parser, impliedType); err != nil {
		return err
	}

	// Add the new call to the cleanup function here, after the state has been parsed into r.Item
	r.CleanUpOptionalEmptyAttributes(resourceSchema.Block)

	return nil
}

func (r *Resource) ServiceName() string {
	return strings.TrimPrefix(r.InstanceInfo.Type, r.Provider+"_")
}

// CleanUpOptionalEmptyAttributes initiates the cleanup process.
func (r *Resource) CleanUpOptionalEmptyAttributes(resourceSchema *configschema.Block) {
	if r.Item == nil || resourceSchema == nil {
		return
	}

	// First, handle the special case for the top-level 'project' attribute.
	// This is done non-recursively to avoid removing 'project' from nested blocks
	// where it might have a different meaning.
	if projectSchema, ok := resourceSchema.Attributes["project"]; ok {
		if projectSchema.Computed {
			delete(r.Item, "project")
		}
	}

	// Now, perform the recursive cleanup for all other optional+empty attributes.
	cleanOptionalEmptyAttributes(r.Item, resourceSchema)
}

// cleanOptionalEmptyAttributes is a recursive helper function that traverses the resource data.
func cleanOptionalEmptyAttributes(data map[string]interface{}, blockSchema *configschema.Block) {
	for key, value := range data {
		// Check for the key in attributes
		if attrSchema, ok := blockSchema.Attributes[key]; ok {
			// Handle generic optional attributes with zero-values.
			if attrSchema.Optional {
				if value == nil || reflect.DeepEqual(value, reflect.Zero(reflect.TypeOf(value)).Interface()) {
					delete(data, key)
					continue
				}
			}
		}

		// Check for the key in nested block types
		if blockTypeSchema, ok := blockSchema.BlockTypes[key]; ok {
			if list, ok := value.([]interface{}); ok {
				for _, item := range list {
					if itemMap, ok := item.(map[string]interface{}); ok {
						// Recurse into the nested block
						cleanOptionalEmptyAttributes(itemMap, &blockTypeSchema.Block)
					}
				}
			}
		}
	}
}
