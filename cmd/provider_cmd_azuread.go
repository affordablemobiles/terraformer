//go:build azuread || !single_provider
// Copyright 2019 The Terraformer Authors.
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
package cmd

import (
	azuread "github.com/GoogleCloudPlatform/terraformer/providers/azuread"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"github.com/spf13/cobra"
)

// init will automatically register this provider with the global lists.
func init() {
	providerImporterSubcommands = append(providerImporterSubcommands, newCmdAzureADImporter)
	providerGenerators["azuread"] = newAzureADProvider
}

func newCmdAzureADImporter(options ImportOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "azuread",
		Short: "Import current state to Terraform configuration from Azure Active Directory",
		Long:  "Import current state to Terraform configuration from Azure Active Directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := newAzureADProvider()
			err := Import(provider, options, []string{options.ResourceGroup})
			if err != nil {
				return err
			}
			return nil
		},
	}

	cmd.AddCommand(listCmd(newAzureADProvider()))
	baseProviderFlags(cmd.PersistentFlags(), &options, "resource_group", "resource_group=name1:name2:name3")
	cmd.PersistentFlags().StringVarP(&options.ResourceGroup, "resource-group", "R", "", "")
	return cmd
}

func newAzureADProvider() terraformutils.ProviderGenerator {
	return &azuread.AzureADProvider{}
}
