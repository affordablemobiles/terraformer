// Copyright 2024 The Terraformer Authors.
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
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/firestore/v1"
)

// FirestoreGenerator generates Terraform resources for Google Cloud Firestore.
type FirestoreGenerator struct {
	GCPService
}

// InitResources initializes the Firestore resources.
func (g *FirestoreGenerator) InitResources() error {
	project := g.GetArgs()["project"].(string)

	ctx := context.Background()

	firestoreService, err := firestore.NewService(ctx)
	if err != nil {
		return fmt.Errorf("failed to create firestore service: %w", err)
	}

	isGlobal := g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global"
	currentRegion := g.GetArgs()["region"].(compute.Region).Name

	if err := g.initDatabases(ctx, firestoreService, project, isGlobal, currentRegion); err != nil {
		return err
	}

	return nil
}

// initDatabases fetches all Firestore databases and their sub-resources.
func (g *FirestoreGenerator) initDatabases(ctx context.Context, firestoreService *firestore.Service, project string, isGlobal bool, currentRegion string) error {
	parent := fmt.Sprintf("projects/%s", project)
	req := firestoreService.Projects.Databases.List(parent)
	page, err := req.Do()
	if err != nil {
		return fmt.Errorf("failed to list firestore databases: %w", err)
	}

	for _, database := range page.Databases {
		databaseLocation := strings.ToLower(database.LocationId)
		isMultiRegion := !strings.Contains(databaseLocation, "-")

		if isGlobal {
			// For a global run, we only want multi-region databases.
			if !isMultiRegion {
				continue
			}
		} else {
			// For a regional run, we only want databases in that specific region.
			if databaseLocation != currentRegion {
				continue
			}
		}

		parts := strings.Split(database.Name, "/")
		databaseID := parts[len(parts)-1]
		resourceName := terraformutils.TfSanitize(databaseID)

		g.Resources = append(g.Resources, terraformutils.NewResource(
			database.Name,
			resourceName,
			"google_firestore_database",
			g.ProviderName,
			map[string]string{
				"project": project,
				"name":    databaseID,
			},
			[]string{},
			map[string]interface{}{},
		))

		// Initialize sub-resources for each database found.
		if err := g.initIndexes(ctx, firestoreService, database.Name); err != nil {
			return err
		}
		if err := g.initFields(ctx, firestoreService, database.Name); err != nil {
			return err
		}
		if err := g.initBackupSchedules(ctx, firestoreService, database.Name); err != nil {
			return err
		}
	}
	return nil
}

// initIndexes fetches all composite indexes for a given database.
func (g *FirestoreGenerator) initIndexes(ctx context.Context, firestoreService *firestore.Service, databaseName string) error {
	project := g.GetArgs()["project"].(string)

	parent := fmt.Sprintf("%s/collectionGroups/-", databaseName)
	req := firestoreService.Projects.Databases.CollectionGroups.Indexes.List(parent)
	if err := req.Pages(ctx, func(page *firestore.GoogleFirestoreAdminV1ListIndexesResponse) error {
		for _, index := range page.Indexes {
			parts := strings.Split(index.Name, "/")
			indexName := parts[len(parts)-1]
			collectionName := parts[len(parts)-3]
			resourceName := terraformutils.TfSanitize(fmt.Sprintf("%s-%s", collectionName, indexName))
			g.Resources = append(g.Resources, terraformutils.NewResource(
				index.Name,
				resourceName,
				"google_firestore_index",
				g.ProviderName,
				map[string]string{
					"project": project,
					"name":    index.Name,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list firestore indexes for database %s: %w", databaseName, err)
	}
	return nil
}

// initFields fetches all field overrides for a given database.
func (g *FirestoreGenerator) initFields(ctx context.Context, firestoreService *firestore.Service, databaseName string) error {
	project := g.GetArgs()["project"].(string)

	parent := fmt.Sprintf("%s/collectionGroups/-", databaseName)
	req := firestoreService.Projects.Databases.CollectionGroups.Fields.List(parent)
	// The API requires a filter for fields that have been explicitly overridden.
	req.Filter("indexConfig.usesAncestorConfig:false OR ttlConfig:*")
	if err := req.Pages(ctx, func(page *firestore.GoogleFirestoreAdminV1ListFieldsResponse) error {
		for _, field := range page.Fields {
			parts := strings.Split(field.Name, "/")
			fieldName := parts[len(parts)-1]
			// The API returns a wildcard '*' for collection-level overrides, which are not manageable as individual field resources.
			if fieldName == "*" {
				continue
			}
			collectionName := parts[len(parts)-3]
			resourceName := terraformutils.TfSanitize(fmt.Sprintf("%s-%s", collectionName, fieldName))
			g.Resources = append(g.Resources, terraformutils.NewResource(
				field.Name,
				resourceName,
				"google_firestore_field",
				g.ProviderName,
				map[string]string{
					"project": project,
					"name":    field.Name,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to list firestore fields for database %s: %w", databaseName, err)
	}
	return nil
}

// initBackupSchedules fetches the backup schedule for a given database.
func (g *FirestoreGenerator) initBackupSchedules(ctx context.Context, firestoreService *firestore.Service, databaseName string) error {
	req := firestoreService.Projects.Databases.BackupSchedules.List(databaseName)
	resp, err := req.Do()
	if err != nil {
		return fmt.Errorf("failed to list firestore backup schedules for database %s: %w", databaseName, err)
	}

	for _, schedule := range resp.BackupSchedules {
		parts := strings.Split(schedule.Name, "/")
		scheduleName := parts[len(parts)-1]
		databaseID := parts[len(parts)-3]
		resourceName := terraformutils.TfSanitize(fmt.Sprintf("%s-%s", databaseID, scheduleName))

		g.Resources = append(g.Resources, terraformutils.NewResource(
			schedule.Name, // The import ID is the full resource name.
			resourceName,
			"google_firestore_backup_schedule",
			g.ProviderName,
			map[string]string{
				"project":  g.GetArgs()["project"].(string),
				"database": databaseID,
				"name":     scheduleName,
			},
			[]string{},
			map[string]interface{}{},
		))
	}
	return nil
}

func (g *FirestoreGenerator) PostConvertHook() error {
	for i, resource := range g.Resources {
		if resource.InstanceInfo.Type == "google_firestore_field" {
			g.Resources[i].PreserveOrder = []string{"fields"}
		}
	}

	return nil
}
