// Copyright 2018 The Terraformer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package terraformoutput

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper"

	"github.com/hashicorp/terraform/terraform"
)

// getExistingTfFiles reads a directory and returns a list of .tf and .tf.json files
// that are considered resource files and candidates for cleanup.
func getExistingTfFiles(dirPath string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Directory doesn't exist, so no files to return
		}
		return nil, err
	}

	for _, entry := range entries {
		fileName := entry.Name()
		// Check for both .tf and .tf.json extensions
		if !entry.IsDir() && (strings.HasSuffix(fileName, ".tf") || strings.HasSuffix(fileName, ".tf.json")) {
			// Exclude special terraform files from cleanup as they are managed separately
			// and are not resource-specific files.
			switch fileName {
			case "provider.tf", "versions.tf", "outputs.tf", "provider.tf.json", "outputs.tf.json":
				continue
			default:
				files = append(files, filepath.Join(dirPath, fileName))
			}
		}
	}
	return files, nil
}

// getAllFilesFromDir reads a directory and returns a list of all files within it.
func getAllFilesFromDir(dirPath string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Directory doesn't exist, no files to return
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(dirPath, entry.Name()))
		}
	}
	return files, nil
}

func OutputHclFiles(resources []terraformutils.Resource, provider terraformutils.ProviderGenerator, path string, serviceName string, isCompact bool, output string, sort bool) error {
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return err
	}

	// Get a list of existing .tf files before we start generating new ones
	existingTfFiles, err := getExistingTfFiles(path)
	if err != nil {
		log.Printf("could not read directory for cleanup %s: %v", path, err)
	}
	// Get a list of existing data files
	existingDataFiles, err := getAllFilesFromDir(filepath.Join(path, "data"))
	if err != nil {
		log.Printf("could not read data directory for cleanup %s: %v", path, err)
	}

	// Keep track of all files generated during this run
	generatedTfFiles := map[string]bool{}
	generatedDataFiles := map[string]bool{}

	providerConfig := map[string]interface{}{
		"version": providerwrapper.GetProviderVersion(provider.GetName()),
	}

	if providerWithSource, ok := provider.(terraformutils.ProviderWithSource); ok {
		providerConfig["source"] = providerWithSource.GetSource()
	}

	// create provider file
	providerData := provider.GetProviderData()
	providerData["terraform"] = map[string]interface{}{
		"required_providers": []map[string]interface{}{{
			provider.GetName(): providerConfig,
		}},
	}

	providerDataFile, err := terraformutils.Print(providerData, map[string]struct{}{}, output, sort, make(map[string]map[string][]string))
	if err != nil {
		return err
	}
	PrintFile(filepath.Join(path, "provider."+GetFileExtension(output)), providerDataFile)

	// create outputs files
	outputs := map[string]interface{}{}
	outputsByResource := map[string]map[string]interface{}{}

	for i, r := range resources {
		outputState := map[string]*terraform.OutputState{}
		outputsByResource[r.InstanceInfo.Type+"_"+r.ResourceName+"_"+r.GetIDKey()] = map[string]interface{}{
			"value": "${" + r.InstanceInfo.Type + "." + r.ResourceName + "." + r.GetIDKey() + "}",
		}
		outputState[r.InstanceInfo.Type+"_"+r.ResourceName+"_"+r.GetIDKey()] = &terraform.OutputState{
			Type:  "string",
			Value: r.InstanceState.Attributes[r.GetIDKey()],
		}
		for _, v := range provider.GetResourceConnections() {
			for k, ids := range v {
				if (serviceName != "" && k == serviceName) || (serviceName == "" && k == r.ServiceName()) {
					if _, exist := r.InstanceState.Attributes[ids[1]]; exist {
						key := ids[1]
						if ids[1] == "self_link" || ids[1] == "id" {
							key = r.GetIDKey()
						}
						linkKey := r.InstanceInfo.Type + "_" + r.ResourceName + "_" + key
						outputsByResource[linkKey] = map[string]interface{}{
							"value": "${" + r.InstanceInfo.Type + "." + r.ResourceName + "." + key + "}",
						}
						outputState[linkKey] = &terraform.OutputState{
							Type:  "string",
							Value: r.InstanceState.Attributes[ids[1]],
						}
					}
				}
			}
		}
		resources[i].Outputs = outputState
	}
	if len(outputsByResource) > 0 {
		outputs["output"] = outputsByResource
		outputsFile, err := terraformutils.Print(outputs, map[string]struct{}{}, output, sort, make(map[string]map[string][]string))
		if err != nil {
			return err
		}
		PrintFile(filepath.Join(path, "outputs."+GetFileExtension(output)), outputsFile)
	}

	// group by resource by type
	typeOfServices := map[string][]terraformutils.Resource{}
	for _, r := range resources {
		typeOfServices[r.InstanceInfo.Type] = append(typeOfServices[r.InstanceInfo.Type], r)
	}
	if isCompact {
		filePath := filepath.Join(path, "resources."+GetFileExtension(output))
		err := printFile(resources, "resources", path, output, sort, generatedDataFiles)
		if err != nil {
			return err
		}
		generatedTfFiles[filePath] = true
	} else {
		for k, v := range typeOfServices {
			fileName := strings.ReplaceAll(k, strings.Split(k, "_")[0]+"_", "")
			filePath := filepath.Join(path, fileName+"."+GetFileExtension(output))
			err := printFile(v, fileName, path, output, sort, generatedDataFiles)
			if err != nil {
				return err
			}
			generatedTfFiles[filePath] = true
		}
	}

	// Delete stale .tf files that were not generated in this run
	for _, filePath := range existingTfFiles {
		if !generatedTfFiles[filePath] {
			log.Printf("removing stale file: %s", filePath)
			if err := os.Remove(filePath); err != nil {
				log.Printf("failed to remove stale file %s: %v", filePath, err)
			}
		}
	}

	// Delete stale data files that were not generated in this run
	for _, filePath := range existingDataFiles {
		if !generatedDataFiles[filePath] {
			log.Printf("removing stale data file: %s", filePath)
			if err := os.Remove(filePath); err != nil {
				log.Printf("failed to remove stale data file %s: %v", filePath, err)
			}
		}
	}
	return nil
}

func printFile(v []terraformutils.Resource, fileName, path, output string, sort bool, generatedDataFiles map[string]bool) error {
	for _, res := range v {
		if res.DataFiles == nil {
			continue
		}
		for dataFileName, content := range res.DataFiles {
			dataDirPath := filepath.Join(path, "data")
			if err := os.MkdirAll(dataDirPath, os.ModePerm); err != nil {
				return err
			}
			fullDataPath := filepath.Join(dataDirPath, dataFileName)
			err := os.WriteFile(fullDataPath, content, os.ModePerm)
			if err != nil {
				return err
			}
			generatedDataFiles[fullDataPath] = true
		}
	}

	tfFile, err := terraformutils.HclPrintResource(v, map[string]interface{}{}, output, sort)
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(path, fileName+"."+GetFileExtension(output)), tfFile, os.ModePerm)
	if err != nil {
		return err
	}

	return nil
}

func PrintFile(path string, data []byte) {
	err := os.WriteFile(path, data, os.ModePerm)
	if err != nil {
		log.Fatal(err)
		return
	}
}

func GetFileExtension(outputFormat string) string {
	if outputFormat == "json" {
		return "tf.json"
	}
	return "tf"
}
