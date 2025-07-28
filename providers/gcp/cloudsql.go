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
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"

	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1"
)

var cloudSQLAllowEmptyValues = []string{}

var cloudSQLAdditionalFields = map[string]interface{}{}

type CloudSQLGenerator struct {
	GCPService
}

func (g *CloudSQLGenerator) loadDBInstances(svc *sqladmin.Service, project string) error {
	if g.GetArgs()["region"].(compute.Region).Name == "" || g.GetArgs()["region"].(compute.Region).Name == "global" {
		return nil
	}

	dbInstances, err := svc.Instances.List(project).Filter(
		fmt.Sprintf("region:%s", g.GetArgs()["region"].(compute.Region).Name),
	).Do()
	if err != nil {
		return err
	}
	for _, dbInstance := range dbInstances.Items {
		switch dbInstance.InstanceType {
		case "CLOUD_SQL_INSTANCE":
			g.Resources = append(g.Resources, terraformutils.NewResource(
				dbInstance.Name,
				dbInstance.Name,
				"google_sql_database_instance",
				g.ProviderName,
				map[string]string{
					"project": project,
					"name":    dbInstance.Name,
				},
				cloudSQLAllowEmptyValues,
				cloudSQLAdditionalFields,
			))
			if err := g.loadDBs(svc, dbInstance, project); err != nil {
				return err
			}
			if err := g.loadUsers(svc, dbInstance, project); err != nil {
				return err
			}
			if err := g.loadSslCerts(svc, dbInstance.Name, project); err != nil {
				return err
			}
		case "ON_PREMISES_INSTANCE":
			g.Resources = append(g.Resources, terraformutils.NewResource(
				dbInstance.Name,
				dbInstance.Name,
				"google_sql_source_representation_instance",
				g.ProviderName,
				map[string]string{
					"project": project,
					"name":    dbInstance.Name,
				},
				cloudSQLAllowEmptyValues,
				cloudSQLAdditionalFields,
			))
		}
	}
	return nil
}

func (g *CloudSQLGenerator) loadDBs(svc *sqladmin.Service, instance *sqladmin.DatabaseInstance, project string) error {
	DBs, err := svc.Databases.List(project, instance.Name).Do()
	if err != nil {
		return err
	}
	for _, db := range DBs.Items {
		g.Resources = append(g.Resources, terraformutils.NewResource(
			fmt.Sprintf("%s/%s", instance.Name, db.Name),
			fmt.Sprintf("%s-%s", instance.Name, db.Name),
			"google_sql_database",
			g.ProviderName,
			map[string]string{
				"instance": instance.Name,
				"project":  project,
				"name":     db.Name,
			},

			cloudSQLAllowEmptyValues,
			cloudSQLAdditionalFields,
		))
	}
	return nil
}

func (g *CloudSQLGenerator) loadUsers(svc *sqladmin.Service, instance *sqladmin.DatabaseInstance, project string) error {
	users, err := svc.Users.List(project, instance.Name).Do()
	if err != nil {
		return err
	}
	for _, user := range users.Items {
		userName := user.Name
		if (user.Type == "CLOUD_IAM_USER" || user.Type == "CLOUD_IAM_SERVICE_ACCOUNT") && !strings.Contains(userName, "@") {
			log.Printf("[WARNING] IAM user %s for instance %s does not have a domain. Please add the domain manually to the generated Terraform.", userName, instance.Name)
		}

		var resourceID, resourceName string
		// The 'host' attribute is only for MySQL users.
		if strings.HasPrefix(instance.DatabaseVersion, "MYSQL") {
			resourceID = fmt.Sprintf("projects/%s/instances/%s/users/%s/%s", project, instance.Name, user.Host, user.Name)
			resourceName = fmt.Sprintf("%s-%s-%s", instance.Name, userName, user.Host)
		} else { // For PostgreSQL and SQL Server
			resourceID = fmt.Sprintf("projects/%s/instances/%s/users/%s", project, instance.Name, user.Name)
			resourceName = fmt.Sprintf("%s-%s", instance.Name, userName)
		}

		g.Resources = append(g.Resources, terraformutils.NewResource(
			resourceID,
			resourceName,
			"google_sql_user",
			g.ProviderName,
			map[string]string{
				"project":  project,
				"instance": instance.Name,
				"name":     userName,
				"host":     user.Host,
			},
			cloudSQLAllowEmptyValues,
			cloudSQLAdditionalFields,
		))
	}
	return nil
}

func (g *CloudSQLGenerator) loadSslCerts(svc *sqladmin.Service, instanceName, project string) error {
	sslCerts, err := svc.SslCerts.List(project, instanceName).Do()
	if err != nil {
		return err
	}
	for _, sslCert := range sslCerts.Items {
		g.Resources = append(g.Resources, terraformutils.NewResource(
			fmt.Sprintf("projects/%s/instances/%s/sslCerts/%s", project, instanceName, sslCert.Sha1Fingerprint),
			fmt.Sprintf("%s-%s", instanceName, sslCert.CommonName),
			"google_sql_ssl_cert",
			g.ProviderName,
			map[string]string{
				"project":     project,
				"instance":    instanceName,
				"common_name": sslCert.CommonName,
			},
			cloudSQLAllowEmptyValues,
			cloudSQLAdditionalFields,
		))
	}
	return nil
}

// Generate TerraformResources from GCP API,
// from each databases create many TerraformResource(dbinstance + databases)
// Need dbinstance name as ID for terraform resource
func (g *CloudSQLGenerator) InitResources() error {
	project := g.GetArgs()["project"].(string)
	ctx := context.Background()
	svc, err := sqladmin.NewService(ctx)
	if err != nil {
		return err
	}
	if err := g.loadDBInstances(svc, project); err != nil {
		return err
	}

	return nil
}
