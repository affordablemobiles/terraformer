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
	"slices"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
	"google.golang.org/api/storagetransfer/v1"
)

var GcsAllowEmptyValues = []string{"labels.", "created_before"}

var GcsAdditionalFields = map[string]interface{}{}

type GcsGenerator struct {
	GCPService
}

func (g *GcsGenerator) createBucketsResources(ctx context.Context, gcsService *storage.Service) []terraformutils.Resource {
	isGlobal := g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global"

	resources := []terraformutils.Resource{}
	bucketList := gcsService.Buckets.List(g.GetArgs()["project"].(string))
	if err := bucketList.Pages(ctx, func(page *storage.Buckets) error {
		for _, bucket := range page.Items {
			isBucketRegional := slices.Contains(g.GetArgs()["regions"].([]string), strings.ToLower(bucket.Location))
			if isGlobal == isBucketRegional {
				continue
			}
			resources = append(resources, terraformutils.NewResource(
				bucket.Name,
				bucket.Name,
				"google_storage_bucket",
				g.ProviderName,
				map[string]string{
					"name":          bucket.Name,
					"force_destroy": "false",
				},
				GcsAllowEmptyValues,
				GcsAdditionalFields,
			))
			resources = append(resources, terraformutils.NewResource(
				bucket.Name,
				bucket.Name,
				"google_storage_bucket_acl",
				g.ProviderName,
				map[string]string{
					"bucket":        bucket.Name,
					"role_entity.#": strconv.Itoa(len(bucket.Acl)),
				},
				GcsAllowEmptyValues,
				GcsAdditionalFields,
			))
			resources = append(resources, terraformutils.NewResource(
				bucket.Name,
				bucket.Name,
				"google_storage_default_object_acl",
				g.ProviderName,
				map[string]string{
					"bucket":        bucket.Name,
					"role_entity.#": strconv.Itoa(len(bucket.Acl)),
				},
				GcsAllowEmptyValues,
				GcsAdditionalFields,
			))

			resources = append(resources, terraformutils.NewResource(
				bucket.Name,
				bucket.Name,
				"google_storage_bucket_iam_policy",
				g.ProviderName,
				map[string]string{
					"bucket": bucket.Name,
				},
				GcsAllowEmptyValues,
				GcsAdditionalFields,
			))

			if iam, err := gcsService.Buckets.GetIamPolicy(bucket.Name).Do(); err == nil {
				for _, binding := range iam.Bindings {
					resources = append(resources, terraformutils.NewResource(
						bucket.Name,
						bucket.Name,
						"google_storage_bucket_iam_binding",
						g.ProviderName,
						map[string]string{
							"bucket": bucket.Name,
							"role":   binding.Role,
						},
						GcsAllowEmptyValues,
						GcsAdditionalFields,
					))

					for _, member := range binding.Members {
						resources = append(resources, terraformutils.NewResource(
							bucket.Name,
							bucket.Name,
							"google_storage_bucket_iam_member",
							g.ProviderName,
							map[string]string{
								"bucket": bucket.Name,
								"role":   binding.Role,
								"member": member,
							},
							GcsAllowEmptyValues,
							GcsAdditionalFields,
						))
					}
				}
			}

			resources = append(resources, g.createNotificationResources(gcsService, bucket)...)
		}
		return nil
	}); err != nil {
		log.Println(err)
	}
	return resources
}

func (g *GcsGenerator) createNotificationResources(gcsService *storage.Service, bucket *storage.Bucket) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	notificationList, err := gcsService.Notifications.List(bucket.Name).Do()
	if err != nil {
		log.Println(err)
		return resources
	}
	for _, notification := range notificationList.Items {
		resources = append(resources, terraformutils.NewResource(
			bucket.Name+"/notificationConfigs/"+notification.Id,
			bucket.Name+"/"+notification.Id,
			"google_storage_notification",
			g.ProviderName,
			map[string]string{},
			GcsAllowEmptyValues,
			GcsAdditionalFields,
		))
	}
	return resources
}

func (g *GcsGenerator) createTransferJobsResources(ctx context.Context, storageTransferService *storagetransfer.Service) []terraformutils.Resource {
	resources := []terraformutils.Resource{}
	projectID := g.GetArgs()["project"].(string)
	filter := fmt.Sprintf(`{"projectId":"%s"}`, projectID)
	transferJobsList := storageTransferService.TransferJobs.List(filter)
	err := transferJobsList.Pages(ctx, func(page *storagetransfer.ListTransferJobsResponse) error {
		log.Println(page.TransferJobs)
		for _, transferJob := range page.TransferJobs {
			resources = append(resources, terraformutils.NewResource(
				transferJob.Name,
				transferJob.Name,
				"google_storage_transfer_job",
				g.ProviderName,
				map[string]string{
					"name": transferJob.Name,
				},
				GcsAllowEmptyValues,
				GcsAdditionalFields,
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
// from each bucket  create 1 TerraformResource
// Need bucket name as ID for terraform resource
func (g *GcsGenerator) InitResources() error {
	ctx := context.Background()
	projectID := g.GetArgs()["project"].(string)

	gcsService, err := storage.NewService(ctx)
	if err != nil {
		log.Print(err)
		return err
	}
	g.Resources = g.createBucketsResources(ctx, gcsService)

	storageTransferService, err := storagetransfer.NewService(ctx, option.WithQuotaProject(projectID))
	if err != nil {
		log.Print(err)
		return err
	}
	g.Resources = append(g.Resources, g.createTransferJobsResources(ctx, storageTransferService)...)
	return nil
}

// PostGenerateHook for add bucket policy json as heredoc
// support only bucket with policy
func (g *GcsGenerator) PostConvertHook() error {
	for i, resource := range g.Resources {
		if resource.InstanceInfo.Type != "google_storage_bucket_iam_policy" {
			continue
		}
		if _, exist := resource.Item["policy_data"]; exist {
			policy := resource.Item["policy_data"].(string)
			g.Resources[i].Item["policy_data"] = fmt.Sprintf(`<<POLICY
%s
POLICY`, policy)
		}
	}
	return nil
}
