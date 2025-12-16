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
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/api/compute/v1"
)

var (
	InvalidRegion = errors.New("invalid region specified")
)

var (
	regionsCache = &sync.Map{} // Caches the list of regions for a project
	regionCache  = &sync.Map{} // Caches the details of a specific region
)

type GCPProvider struct { //nolint
	terraformutils.Provider
	projectName  string
	regions      []string
	region       compute.Region
	providerType string
}

func GetRegions(project string) []string {
	// 1. Check the cache first.
	if cachedRegions, ok := regionsCache.Load(project); ok {
		return cachedRegions.([]string)
	}

	// 2. If not in cache, make the API call.
	computeService, err := compute.NewService(context.Background())
	if err != nil {
		log.Printf("ERROR creating compute service: %v", err)
		return []string{}
	}

	regionsList, err := computeService.Regions.List(project).Do()
	if err != nil {
		log.Printf("ERROR listing regions for project %s: %v", project, err)
		return []string{}
	}

	regions := []string{}
	for _, region := range regionsList.Items {
		regions = append(regions, region.Name)
	}

	// 3. Store the result in the cache for next time.
	regionsCache.Store(project, regions)

	return regions
}

func getRegion(project, regionName string) (compute.Region, error) {
	if regionName == "global" {
		return compute.Region{}, nil
	}

	cacheKey := fmt.Sprintf("%s-%s", project, regionName)

	// 1. Check the cache first.
	if cachedRegion, ok := regionCache.Load(cacheKey); ok {
		return cachedRegion.(compute.Region), nil
	}

	// 2. If not in cache, make the API call.
	computeService, err := compute.NewService(context.Background())
	if err != nil {
		return compute.Region{}, fmt.Errorf("failed to create compute service: %w", err)
	}

	region, err := computeService.Regions.Get(project, regionName).Fields("name", "zones").Do()
	if err != nil {
		if strings.Contains(err.Error(), "Unknown region") || strings.Contains(err.Error(), "notFound") || strings.Contains(err.Error(), "invalid") {
			return compute.Region{}, InvalidRegion
		}
		log.Println(err)
		return compute.Region{}, fmt.Errorf("failed to get region details for %s: %w", regionName, err)
	}

	// 3. Store the result in the cache for next time.
	regionCache.Store(cacheKey, *region)

	return *region, nil
}

func (p *GCPProvider) Init(args []string) error {
	// The main project name for Terraformer to scan, taken from the arguments.
	projectName := args[1]
	if projectName == "" {
		return errors.New("the project name to scan must be provided as an argument")
	}
	p.projectName = projectName

	// Use a separate project for API lookups if the environment variable is set.
	// This ensures regional lookups run against a project where APIs are enabled.
	regionalProject := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if regionalProject == "" {
		// If the environment variable isn't set, fall back to the main project.
		regionalProject = projectName
	}

	log.Printf("Scanning project '%s', using project '%s' for regional API lookups.", p.projectName, regionalProject)

	// Call the region functions using the dedicated regional project ID.
	var err error
	p.regions = GetRegions(regionalProject)
	p.region, err = getRegion(regionalProject, args[0])
	if err != nil {
		return err
	}

	p.providerType = args[2]
	return nil
}

func (p *GCPProvider) GetName() string {
	if p.providerType != "" {
		return "google-" + p.providerType
	}
	return "google"
}

func (p *GCPProvider) InitService(serviceName string, verbose bool) error {
	var isSupported bool
	if _, isSupported = p.GetSupportedService()[serviceName]; !isSupported {
		return errors.New("gcp: " + serviceName + " not supported service")
	}
	p.Service = p.GetSupportedService()[serviceName]
	p.Service.SetName(serviceName)
	p.Service.SetVerbose(verbose)
	p.Service.SetProviderName(p.GetName())
	p.Service.SetArgs(map[string]interface{}{
		"region":  p.region,
		"regions": p.regions,
		"project": p.projectName,
	})
	return nil
}

// GetGCPSupportService return map of support service for GCP
func (p *GCPProvider) GetSupportedService() map[string]terraformutils.ServiceGenerator {
	services := GetComputeServices()
	services["addresses"] = &GCPFacade{service: &AddressesGenerator{}}
	services["networkEndpointGroups"] = &GCPFacade{service: &NEGGenerator{}}
	services["bigQuery"] = &GCPFacade{service: &BigQueryGenerator{}}
	services["cloudFunctions"] = &GCPFacade{service: &CloudFunctionsGenerator{}}
	services["cloudsql"] = &GCPFacade{service: &CloudSQLGenerator{}}
	services["cloudtasks"] = &GCPFacade{service: &CloudTaskGenerator{}}
	services["dataProc"] = &GCPFacade{service: &DataprocGenerator{}}
	services["dns"] = &GCPFacade{service: &CloudDNSGenerator{}}
	services["gcs"] = &GCPFacade{service: &GcsGenerator{}}
	services["gke"] = &GCPFacade{service: &GkeGenerator{}}
	services["iam"] = &GCPFacade{service: &IamGenerator{}}
	services["kms"] = &GCPFacade{service: &KmsGenerator{}}
	services["logging"] = &GCPFacade{service: &LoggingGenerator{}}
	services["memoryStore"] = &GCPFacade{service: &MemoryStoreGenerator{}}
	services["monitoring"] = &GCPFacade{service: &MonitoringGenerator{}}
	services["project"] = &GCPFacade{service: &ProjectGenerator{}}
	services["instances"] = &GCPFacade{service: &InstancesGenerator{}}
	services["pubsub"] = &GCPFacade{service: &PubsubGenerator{}}
	services["schedulerJobs"] = &GCPFacade{service: &SchedulerJobsGenerator{}}
	services["cloudbuild"] = &GCPFacade{service: &CloudBuildGenerator{}}
	services["appengine"] = &GCPFacade{service: &AppEngineGenerator{}}
	services["artifactRegistry"] = &GCPFacade{service: &ArtifactRegistryGenerator{}}
	services["serverlessvpc"] = &GCPFacade{service: &VpcAccessConnectorGenerator{}}
	services["spanner"] = &GCPFacade{service: &SpannerGenerator{}}
	services["cloudrun"] = &GCPFacade{service: &CloudRunGenerator{}}
	services["filestore"] = &GCPFacade{service: &FilestoreGenerator{}}
	services["firestore"] = &GCPFacade{service: &FirestoreGenerator{}}
	services["iap"] = &GCPFacade{service: &IapGenerator{}}
	services["secretmanager"] = &GCPFacade{service: &SecretManagerGenerator{}}
	services["vpnGateways"] = &GCPFacade{service: &VpnGatewaysGenerator{}}
	services["vpcPeering"] = &GCPFacade{service: &VpcPeeringGenerator{}}
	services["routerNat"] = &GCPFacade{service: &RouterNatGenerator{}}
	services["externalVpnGateways"] = &GCPFacade{service: &ExternalVpnGatewayGenerator{}}
	services["vpnTunnels"] = &GCPFacade{service: &VpnTunnelGenerator{}}
	services["project_services"] = &GCPFacade{service: &ServiceUsageGenerator{}}
	return services
}

func (GCPProvider) GetResourceConnections() map[string]map[string][]string {
	return map[string]map[string][]string{
		"backendBuckets": {"gcs": []string{"bucket_name", "name"}},
		"firewall":       {"networks": []string{"network", "self_link"}},
		"gke": {
			"networks":    []string{"network", "self_link"},
			"subnetworks": []string{"subnetwork", "self_link"},
		},
		"instanceTemplates": {
			"networks":    []string{"network", "self_link"},
			"subnetworks": []string{"subnetworks", "self_link"},
		},
		"regionInstanceGroupManagers": {"instanceTemplates": []string{"version.instance_template", "self_link"}},
		"instanceGroups":              {"instanceTemplates": []string{"version.instance_template", "self_link"}},
		"routes":                      {"networks": []string{"network", "self_link"}},
		"subnetworks":                 {"networks": []string{"network", "self_link"}},
		"forwardingRules": {
			"regionBackendServices": []string{"backend_service", "self_link"},
			"networks":              []string{"network", "self_link"},
		},
		"globalForwardingRules": {
			"targetHttpsProxies": []string{"target", "self_link"},
			"targetHttpProxies":  []string{"target", "self_link"},
			"targetSslProxies":   []string{"target", "self_link"},
		},
		"targetHttpsProxies": {
			"urlMaps": []string{"url_map", "self_link"},
		},
		"targetHttpProxies": {
			"urlMaps": []string{"url_map", "self_link"},
		},
		"targetSslProxies": {
			"backendServices": []string{"backend_service", "self_link"},
		},
		"backendServices": {
			"regionInstanceGroupManagers": []string{"backend.group", "instance_group"},
			"instanceGroupManagers":       []string{"backend.group", "instance_group"},
			"healthChecks":                []string{"health_checks", "self_link"},
		},
		"regionBackendServices": {
			"regionInstanceGroupManagers": []string{"backend.group", "instance_group"},
			"instanceGroupManagers":       []string{"backend.group", "instance_group"},
			"healthChecks":                []string{"health_checks", "self_link"},
		},
		"urlMaps": {
			"backendServices": []string{
				"default_service", "self_link",
				"path_matcher.default_service", "self_link",
				"path_matcher.path_rule.service", "self_link",
			},
			"regionBackendServices": []string{
				"default_service", "self_link",
				"path_matcher.default_service", "self_link",
				"path_matcher.path_rule.service", "self_link",
			},
		},
	}
}
func (p GCPProvider) GetProviderData(arg ...string) map[string]interface{} {
	return map[string]interface{}{
		"provider": map[string]interface{}{
			p.GetName(): map[string]interface{}{
				"project": p.projectName,
			},
		},
	}
}

// GetConfig constructs the configuration object passed to the Google Provider.
// This is critical for v7+ to initialize the transport config and UserAgent,
// preventing "Value Conversion Errors" and segfaults.
func (p *GCPProvider) GetConfig() cty.Value {
	config := map[string]cty.Value{
		"project": cty.StringVal(p.projectName),
	}

	if p.region.Name != "" && p.region.Name != "global" {
		config["region"] = cty.StringVal(p.region.Name)
	}

	return cty.ObjectVal(config)
}

func (p *GCPProvider) GetBasicConfig() cty.Value {
	return p.GetConfig()
}
