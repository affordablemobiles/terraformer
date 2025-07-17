//go:build octopusdeploy || !single_provider
package cmd

import (
	octopusdeploy_terraforming "github.com/GoogleCloudPlatform/terraformer/providers/octopusdeploy"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"github.com/spf13/cobra"
)

// init will automatically register this provider with the global lists.
func init() {
	providerImporterSubcommands = append(providerImporterSubcommands, newCmdOctopusDeployImporter)
	providerGenerators["octopusdeploy"] = newOctopusDeployProvider
}

func newCmdOctopusDeployImporter(options ImportOptions) *cobra.Command {
	var server, apiKey string
	cmd := &cobra.Command{
		Use:   "octopusdeploy",
		Short: "Import current state to Terraform configuration from Octopus Deploy",
		Long:  "Import current state to Terraform configuration from Octopus Deploy",
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := newOctopusDeployProvider()
			options.PathPattern = "{output}/{provider}/"
			err := Import(provider, options, []string{server, apiKey})
			if err != nil {
				return err
			}
			return nil
		},
	}

	cmd.AddCommand(listCmd(newOctopusDeployProvider()))
	baseProviderFlags(cmd.PersistentFlags(), &options, "octopusdeploy", "tagset")
	cmd.PersistentFlags().StringVar(&server, "server", "", "Octopus Server's API endpoint or env param OCTOPUS_CLI_SERVER")
	cmd.PersistentFlags().StringVar(&apiKey, "apikey", "", "Octopus API key or env param OCTOPUS_CLI_API_KEY")
	return cmd
}

func newOctopusDeployProvider() terraformutils.ProviderGenerator {
	return &octopusdeploy_terraforming.OctopusDeployProvider{}
}
