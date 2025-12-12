package gcp

import (
	"context"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/serviceusage/v1"
)

type ServiceUsageGenerator struct {
	GCPService
}

func (g *ServiceUsageGenerator) InitResources() error {
	// Project services are global; prevent duplicate work if regions are specified.
	if region, ok := g.GetArgs()["region"].(compute.Region); ok && region.Name != "" && region.Name != "global" {
		return nil
	}

	project := g.GetArgs()["project"].(string)
	ctx := context.Background()
	service, err := serviceusage.NewService(ctx)
	if err != nil {
		return err
	}

	parent := "projects/" + project
	// Filter to only include enabled services, matching user intent for import.
	call := service.Services.List(parent).Filter("state:ENABLED")

	return call.Pages(ctx, func(page *serviceusage.ListServicesResponse) error {
		for _, s := range page.Services {
			// s.Name format is usually "projects/{project_number}/services/{service_name}"
			// We extract the service name (e.g., "compute.googleapis.com")
			parts := strings.Split(s.Name, "/")
			if len(parts) == 0 {
				continue
			}
			serviceName := parts[len(parts)-1]

			g.Resources = append(g.Resources, terraformutils.NewResource(
				project+"/"+serviceName,
				serviceName,
				"google_project_service",
				g.ProviderName,
				map[string]string{
					"project": project,
					"service": serviceName,
				},
				[]string{},
				map[string]interface{}{},
			))
		}
		return nil
	})
}
