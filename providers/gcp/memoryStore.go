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
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/memcache/v1"
	"google.golang.org/api/redis/v1"
)

var (
	memoryStoreAllowEmptyValues = []string{""}
	memoryStoreAdditionalFields = map[string]interface{}{}
)

// MemoryStoreGenerator holds all the logic for generating memory store resources
type MemoryStoreGenerator struct {
	GCPService
}

// createRedisInstanceResources creates terraform resources for `google_redis_instance`.
// Note: The resource `google_memorystore_instance` is a legacy alias for `google_redis_instance`.
// To align with current best practices and avoid generating conflicting resources, we generate
// `google_redis_instance`. If you need the legacy resource type, you can manually
// change the resource type string in the generated files.
func (g *MemoryStoreGenerator) createRedisInstanceResources(ctx context.Context, redisService *redis.Service) ([]terraformutils.Resource, error) {
	resources := []terraformutils.Resource{}
	project := g.GetArgs()["project"].(string)
	region := g.GetArgs()["region"].(compute.Region).Name
	parent := "projects/" + project + "/locations/" + region
	call := redisService.Projects.Locations.Instances.List(parent)

	err := call.Pages(ctx, func(page *redis.ListInstancesResponse) error {
		for _, obj := range page.Instances {
			t := strings.Split(obj.Name, "/")
			name := t[len(t)-1]
			resources = append(resources, terraformutils.NewResource(
				obj.Name,
				name,
				"google_redis_instance",
				g.ProviderName,
				map[string]string{
					"name":    name,
					"project": project,
					"region":  region,
				},
				memoryStoreAllowEmptyValues,
				memoryStoreAdditionalFields,
			))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resources, nil
}

// createRedisClusterResources creates terraform resources for `google_redis_cluster`
func (g *MemoryStoreGenerator) createRedisClusterResources(ctx context.Context, redisService *redis.Service) ([]terraformutils.Resource, error) {
	resources := []terraformutils.Resource{}
	project := g.GetArgs()["project"].(string)
	region := g.GetArgs()["region"].(compute.Region).Name
	parent := "projects/" + project + "/locations/" + region
	call := redisService.Projects.Locations.Clusters.List(parent)

	err := call.Pages(ctx, func(page *redis.ListClustersResponse) error {
		for _, cluster := range page.Clusters {
			t := strings.Split(cluster.Name, "/")
			name := t[len(t)-1]
			resources = append(resources, terraformutils.NewResource(
				cluster.Name,
				name,
				"google_redis_cluster",
				g.ProviderName,
				map[string]string{
					"name":    name,
					"project": project,
				},
				memoryStoreAllowEmptyValues,
				memoryStoreAdditionalFields,
			))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resources, nil
}

// createMemcacheInstanceResources creates terraform resources for `google_memcache_instance`
func (g *MemoryStoreGenerator) createMemcacheInstanceResources(ctx context.Context, memcacheService *memcache.Service) ([]terraformutils.Resource, error) {
	resources := []terraformutils.Resource{}
	project := g.GetArgs()["project"].(string)
	region := g.GetArgs()["region"].(compute.Region).Name
	parent := "projects/" + project + "/locations/" + region
	call := memcacheService.Projects.Locations.Instances.List(parent)

	err := call.Pages(ctx, func(page *memcache.ListInstancesResponse) error {
		if page.Instances == nil {
			return nil
		}
		for _, obj := range page.Instances {
			t := strings.Split(obj.Name, "/")
			name := t[len(t)-1]
			resources = append(resources, terraformutils.NewResource(
				obj.Name,
				name,
				"google_memcache_instance",
				g.ProviderName,
				map[string]string{
					"name":    name,
					"project": project,
					"region":  region,
				},
				memoryStoreAllowEmptyValues,
				memoryStoreAdditionalFields,
			))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resources, nil
}

// InitResources fetches all Memory Store resources for a given region.
func (g *MemoryStoreGenerator) InitResources() error {
	region := g.GetArgs()["region"].(compute.Region).Name
	if region == "" || region == "global" {
		return nil
	}

	ctx := context.Background()
	var allResources []terraformutils.Resource

	// Redis Service for Redis Instances and Clusters
	redisService, err := redis.NewService(ctx)
	if err != nil {
		return err
	}

	redisInstances, err := g.createRedisInstanceResources(ctx, redisService)
	if err != nil {
		log.Println(err)
	}
	allResources = append(allResources, redisInstances...)

	redisClusters, err := g.createRedisClusterResources(ctx, redisService)
	if err != nil {
		log.Println(err)
	}
	allResources = append(allResources, redisClusters...)

	// Memcache Service for Memcache Instances
	memcacheService, err := memcache.NewService(ctx)
	if err != nil {
		// Not returning an error because the API might not be enabled for the project.
		log.Printf("Error creating Memcache service, skipping Memcache instances: %v", err)
	} else {
		memcacheInstances, err := g.createMemcacheInstanceResources(ctx, memcacheService)
		if err != nil {
			log.Println(err)
		}
		allResources = append(allResources, memcacheInstances...)
	}

	g.Resources = allResources
	return nil
}
