package gcp

import (
	"context"
	"fmt"
	"strings"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	pb "cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	cloudbuildv2 "cloud.google.com/go/cloudbuild/apiv2"
	pbv2 "cloud.google.com/go/cloudbuild/apiv2/cloudbuildpb"
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
	project := g.GetArgs()["project"].(string)

	// v1 client
	c, err := cloudbuild.NewClient(ctx)
	if err != nil {
		return err
	}

	// v2 client
	c2, err := cloudbuildv2.NewRepositoryManagerClient(ctx)
	if err != nil {
		return err
	}

	// Triggers
	if err := g.initTriggers(ctx, c, project); err != nil {
		return err
	}

	// Worker Pools, Connections and Repositories are regional.
	// Only try to fetch them if a region is specified.
	if g.GetArgs()["region"].(compute.Region).Name != "" {
		if err := g.initWorkerPools(ctx, c, project); err != nil {
			return err
		}

		if err := g.initV2Resources(ctx, c2, project); err != nil {
			return err
		}
	}

	return nil
}

func (g *CloudBuildGenerator) initTriggers(ctx context.Context, c *cloudbuild.Client, project string) error {
	location := g.GetArgs()["region"].(compute.Region).Name
	var triggers []*pb.BuildTrigger
	req := &pb.ListBuildTriggersRequest{
		ProjectId: project,
	}

	if location != "" {
		req.Parent = fmt.Sprintf("projects/%s/locations/%s", project, location)
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

	g.Resources = append(g.Resources, g.createBuildTriggers(triggers)...)
	return nil
}

func (g *CloudBuildGenerator) initWorkerPools(ctx context.Context, c *cloudbuild.Client, project string) error {
	location := g.GetArgs()["region"].(compute.Region).Name
	var workerPools []*pb.WorkerPool
	req := &pb.ListWorkerPoolsRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s", project, location),
	}

	it := c.ListWorkerPools(ctx, req)
	for {
		workerPool, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}

		workerPools = append(workerPools, workerPool)
	}

	g.Resources = append(g.Resources, g.createWorkerPools(workerPools)...)
	return nil
}

func (g *CloudBuildGenerator) initV2Resources(ctx context.Context, c2 *cloudbuildv2.RepositoryManagerClient, project string) error {
	location := g.GetArgs()["region"].(compute.Region).Name
	var connections []*pbv2.Connection
	req := &pbv2.ListConnectionsRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s", project, location),
	}

	it := c2.ListConnections(ctx, req)
	for {
		connection, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		connections = append(connections, connection)
	}

	g.Resources = append(g.Resources, g.createConnections(connections)...)

	for _, connection := range connections {
		var repositories []*pbv2.Repository
		req := &pbv2.ListRepositoriesRequest{
			Parent: connection.Name,
		}
		it := c2.ListRepositories(ctx, req)
		for {
			repo, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			repositories = append(repositories, repo)
		}
		g.Resources = append(g.Resources, g.createRepositories(repositories, connection.Name)...)
	}

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

func (g *CloudBuildGenerator) createWorkerPools(workerPools []*pb.WorkerPool) []terraformutils.Resource {
	var resources []terraformutils.Resource

	for _, workerPool := range workerPools {
		s := strings.Split(workerPool.GetName(), "/")
		workerPoolID := s[len(s)-1]

		resources = append(resources, terraformutils.NewResource(
			workerPool.GetName(),
			workerPoolID,
			"google_cloudbuild_worker_pool",
			g.ProviderName,
			map[string]string{
				"project":  g.GetArgs()["project"].(string),
				"location": g.getLocation(),
				"name":     workerPoolID,
			},
			[]string{},
			map[string]interface{}{},
		))
	}

	return resources
}

func (g *CloudBuildGenerator) createConnections(connections []*pbv2.Connection) []terraformutils.Resource {
	var resources []terraformutils.Resource

	for _, connection := range connections {
		s := strings.Split(connection.GetName(), "/")
		connectionID := s[len(s)-1]

		resources = append(resources, terraformutils.NewResource(
			connection.GetName(),
			connectionID,
			"google_cloudbuildv2_connection",
			g.ProviderName,
			map[string]string{
				"project":  g.GetArgs()["project"].(string),
				"location": g.getLocation(),
				"name":     connectionID,
			},
			[]string{},
			map[string]interface{}{},
		))
	}

	return resources
}

func (g *CloudBuildGenerator) createRepositories(repositories []*pbv2.Repository, parentConnection string) []terraformutils.Resource {
	var resources []terraformutils.Resource

	for _, repo := range repositories {
		s := strings.Split(repo.GetName(), "/")
		repoID := s[len(s)-1]

		resources = append(resources, terraformutils.NewResource(
			repo.GetName(),
			repoID,
			"google_cloudbuildv2_repository",
			g.ProviderName,
			map[string]string{
				"project":           g.GetArgs()["project"].(string),
				"location":          g.getLocation(),
				"name":              repoID,
				"parent_connection": parentConnection,
			},
			[]string{},
			map[string]interface{}{},
		))
	}

	return resources
}

func (g *CloudBuildGenerator) getLocation() string {
	if region, ok := g.GetArgs()["region"].(compute.Region); ok && region.Name != "" {
		return region.Name
	}

	return "global"
}

func (g *CloudBuildGenerator) PostConvertHook() error {
	for i, resource := range g.Resources {
		if resource.InstanceInfo.Type == "google_cloudbuild_trigger" {
			g.Resources[i].PreserveOrder = []string{"build.step", "build.step.args"}
		}
	}

	return nil
}
