package gcp

import (
	"context"
	"fmt"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	pb "cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/iterator"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
)

const cbMaxPageSize = 50

type CloudBuildGenerator struct {
	GCPService
}

// InitResources generates TerraformResources from GCP API.
func (g *CloudBuildGenerator) InitResources() error {
	ctx := context.Background()

	c, err := cloudbuild.NewClient(ctx)
	if err != nil {
		return err
	}

	var (
		triggers []*pb.BuildTrigger
	)

	req := &pb.ListBuildTriggersRequest{
		ProjectId: g.GetArgs()["project"].(string),
	}

	if g.GetArgs()["region"].(compute.Region).Name != "" {
		req.Parent = fmt.Sprintf("projects/%s/locations/%s", g.GetArgs()["project"].(string), g.GetArgs()["region"].(compute.Region).Name)
	}

	it := c.ListBuildTriggers(ctx, req)
	for {
		trigger, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}

		triggers = append(triggers, trigger)
	}

	g.Resources = g.createBuildTriggers(triggers)
	return nil
}

func (g *CloudBuildGenerator) createBuildTriggers(triggers []*pb.BuildTrigger) []terraformutils.Resource {
	var resources []terraformutils.Resource

	for _, trigger := range triggers {
		resources = append(resources, terraformutils.NewResource(
			trigger.GetId(),
			trigger.GetName(),
			"google_cloudbuild_trigger",
			g.ProviderName,
			map[string]string{
				"project":    g.GetArgs()["project"].(string),
				"location":   g.getLocation(),
				"trigger_id": trigger.GetId(),
			},
			[]string{},
			map[string]interface{}{},
		))
	}

	return resources
}

func (g *CloudBuildGenerator) getLocation() string {
	if g.GetArgs()["region"].(compute.Region).Name != "" {
		return g.GetArgs()["region"].(compute.Region).Name
	}

	return "global"
}

func (g *CloudBuildGenerator) PostConvertHook() error {
	for i, resource := range g.Resources {
		if resource.InstanceInfo.Type == "google_cloudbuild_trigger" {
			// Tell the HCL printer to preserve the order for both the list of 'step' blocks
			// and the 'args' list found within ANY of those steps.
			g.Resources[i].PreserveOrder = []string{"build.step", "build.step.args"}
		}
	}

	return nil
}
