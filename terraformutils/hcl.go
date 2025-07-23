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

package terraformutils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/hcl/ast"
	hclPrinter "github.com/hashicorp/hcl/hcl/printer"
	hclParser "github.com/hashicorp/hcl/json/parser"
)

// Copy code from https://github.com/kubernetes/kops project with few changes for support many provider and heredoc

const safeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

var unsafeChars = regexp.MustCompile(`[^0-9A-Za-z_\-]`)

// make HCL output reproducible by sorting the AST nodes
func sortHclTree(tree interface{}) {
	switch t := tree.(type) {
	case []*ast.ObjectItem:
		sort.Slice(t, func(i, j int) bool {
			var bI, bJ bytes.Buffer
			_, _ = hclPrinter.Fprint(&bI, t[i]), hclPrinter.Fprint(&bJ, t[j])
			return bI.String() < bJ.String()
		})
	case []ast.Node:
		sort.Slice(t, func(i, j int) bool {
			var bI, bJ bytes.Buffer
			_, _ = hclPrinter.Fprint(&bI, t[i]), hclPrinter.Fprint(&bJ, t[j])
			return bI.String() < bJ.String()
		})
	default:
	}
}

// sanitizer fixes up an invalid HCL AST, as produced by the HCL parser for JSON
type astSanitizer struct {
	sort            bool
	hintsByResource map[string]map[string][]string
	currentPath     []string // Track the current path in the AST, e.g., ["build", "0", "step"]
}

// output prints creates b printable HCL output and returns it.
func (v *astSanitizer) visit(n interface{}) {
	switch t := n.(type) {
	case *ast.File:
		v.visit(t.Node)
	case *ast.ObjectList:
		// Recurse into all child items first to process nested structures.
		for _, item := range t.Items {
			v.visit(item)
		}

		if !v.sort {
			return
		}

		// Check if the current block (e.g., "build") contains a list of blocks
		// (e.g., "step") that should not be sorted.
		logicalPath := v.buildLogicalPath()
		var unorderedKey string

		if len(v.currentPath) >= 3 && v.currentPath[0] == "resource" {
			resourceType := v.currentPath[1]
			resourceName := v.currentPath[2]
			if resourceHints, ok := v.hintsByResource[resourceType][resourceName]; ok {
				for _, hint := range resourceHints {
					// e.g., hint is "build.step", logicalPath is "build"
					if strings.HasPrefix(hint, logicalPath) && len(logicalPath) > 0 {
						remainder := strings.TrimPrefix(hint, logicalPath)
						// Ensure the remainder is just ".<key>"
						if strings.HasPrefix(remainder, ".") && !strings.Contains(remainder[1:], ".") {
							unorderedKey = strings.TrimPrefix(remainder, ".") // e.g., "step"
							break
						}
					}
				}
			}
		}

		if unorderedKey != "" {
			// Partition the items into those to be preserved and those to be sorted.
			var orderedItems, sortedItems []*ast.ObjectItem
			for _, item := range t.Items {
				key, err := strconv.Unquote(item.Keys[0].Token.Text)
				if err != nil {
					key = item.Keys[0].Token.Text
				}

				if key == unorderedKey {
					orderedItems = append(orderedItems, item)
				} else {
					sortedItems = append(sortedItems, item)
				}
			}

			// Sort only the items that are not part of the preserved list.
			sortHclTree(sortedItems)

			// Reassemble the list with the preserved items first, in their original order.
			t.Items = append(orderedItems, sortedItems...)
			return // Skip the general sort below.
		}

		// Default behavior: sort all items in the list.
		sortHclTree(t.Items)

	case *ast.ListType:
		// A ListType is a list of values, like ["a", "b"] or a list of objects
		// that were explicitly in a JSON array.
		for i, item := range t.List {
			v.currentPath = append(v.currentPath, strconv.Itoa(i))
			v.visit(item)
			v.currentPath = v.currentPath[:len(v.currentPath)-1] // Pop index
		}

		// After visiting, decide whether to sort the list itself.
		currentPathStr := v.buildLogicalPath()
		if v.sort && !v.isPathOrdered(currentPathStr) {
			sortHclTree(t.List)
		}

	case *ast.ObjectType:
		// An ObjectType represents a block body { ... }. It contains an ObjectList.
		// We just need to visit the list of attributes. Sorting is handled in the
		// visit method for ObjectList.
		v.visit(t.List)
	case *ast.ObjectKey:
	case *ast.ObjectItem:
		v.visitObjectItem(t)
	case *ast.LiteralType:
		v.handleHeredoc(t)
	default:
		fmt.Printf(" unknown type: %T\n", n)
	}
}

// buildLogicalPath creates the dot-separated path of attributes from the current AST path,
// ignoring numeric indices used for lists. This creates a path suitable for matching against hints.
func (v *astSanitizer) buildLogicalPath() string {
	var logicalPathParts []string
	if len(v.currentPath) > 2 {
		// We start from index 3 to skip "resource", the type, and the name.
		for _, part := range v.currentPath[3:] {
			// If a path part is not an integer, it's a key we want to keep.
			if _, err := strconv.Atoi(part); err != nil {
				logicalPathParts = append(logicalPathParts, part)
			}
		}
	}

	return strings.Join(logicalPathParts, ".")
}

// isPathOrdered checks if the current path matches any of the ordering hints.
func (v *astSanitizer) isPathOrdered(path string) bool {
	if len(v.currentPath) > 2 && v.currentPath[0] == "resource" {
		resourceType := v.currentPath[1]
		resourceName := v.currentPath[2]

		if resourceHints, ok := v.hintsByResource[resourceType][resourceName]; ok {
			for _, orderedKey := range resourceHints {
				if path == orderedKey {
					return true
				}
			}
		}
	}

	return false
}

func (v *astSanitizer) handleHeredoc(t *ast.LiteralType) {
	if strings.HasPrefix(t.Token.Text, `"<<`) {
		t.Token.Text = t.Token.Text[1:]
		t.Token.Text = t.Token.Text[:len(t.Token.Text)-1]
		t.Token.Text = strings.ReplaceAll(t.Token.Text, `\n`, "\n")
		t.Token.Text = strings.ReplaceAll(t.Token.Text, `\t`, "")
		t.Token.Type = 10
		// check if text json for Unquote and Indent
		jsonTest := t.Token.Text
		lines := strings.Split(jsonTest, "\n")
		jsonTest = strings.Join(lines[1:len(lines)-1], "\n")
		jsonTest = strings.ReplaceAll(jsonTest, "\\\"", "\"")
		// it's json we convert to heredoc back
		var tmp interface{} = map[string]interface{}{}
		err := json.Unmarshal([]byte(jsonTest), &tmp)
		if err != nil {
			tmp = make([]interface{}, 0)
			err = json.Unmarshal([]byte(jsonTest), &tmp)
		}
		if err == nil {
			dataJSONBytes, err := json.MarshalIndent(tmp, "", "  ")
			if err == nil {
				jsonData := strings.Split(string(dataJSONBytes), "\n")
				// first line for heredoc
				jsonData = append([]string{lines[0]}, jsonData...)
				// last line for heredoc
				jsonData = append(jsonData, lines[len(lines)-1])
				hereDoc := strings.Join(jsonData, "\n")
				t.Token.Text = hereDoc
			}
		}
	}
}

func (v *astSanitizer) visitObjectItem(o *ast.ObjectItem) {
	for i, k := range o.Keys {
		if i == 0 {
			text := k.Token.Text
			if text != "" && text[0] == '"' && text[len(text)-1] == '"' {
				v_str := text[1 : len(text)-1]
				safe := true
				for _, c := range v_str {
					if !strings.ContainsRune(safeChars, c) {
						safe = false
						break
					}
				}
				if strings.HasPrefix(v_str, "--") { // if the key starts with "--", we must quote it. Seen in aws_glue_job.default_arguments parameter
					v_str = fmt.Sprintf(`"%s"`, v_str)
				}
				if safe {
					k.Token.Text = v_str
				}
			}
		}
	}

	// An ObjectItem can have multiple keys (e.g., resource "type" "name").
	// The json parser creates nested single-key items instead.
	keys := []string{}
	for _, k := range o.Keys {
		key, err := strconv.Unquote(k.Token.Text)
		if err != nil {
			// Fallback for keys that might not be quoted (e.g., resource type)
			key = k.Token.Text
		}
		keys = append(keys, key)
	}

	v.currentPath = append(v.currentPath, keys...) // Push all keys

	// A hack so that Assign.IsValid is true, so that the printer will output =
	o.Assign.Line = 1

	v.visit(o.Val)

	// Pop all the keys that were added for this item.
	v.currentPath = v.currentPath[:len(v.currentPath)-len(keys)] // Pop all keys
}

func Print(data interface{}, mapsObjects map[string]struct{}, format string, sort bool, hintsByResource map[string]map[string][]string) ([]byte, error) {
	switch format {
	case "hcl":
		return hclPrint(data, mapsObjects, sort, hintsByResource)
	case "json":
		return jsonPrint(data)
	}
	return []byte{}, errors.New("error: unknown output format")
}

func hclPrint(data interface{}, mapsObjects map[string]struct{}, sort bool, hintsByResource map[string]map[string][]string) ([]byte, error) {
	dataBytesJSON, err := jsonPrint(data)
	if err != nil {
		return dataBytesJSON, err
	}
	dataJSON := string(dataBytesJSON)
	nodes, err := hclParser.Parse([]byte(dataJSON))
	if err != nil {
		log.Println(dataJSON)
		return []byte{}, fmt.Errorf("error parsing terraform json: %v", err)
	}
	var sanitizer astSanitizer
	sanitizer.sort = sort
	sanitizer.hintsByResource = hintsByResource
	sanitizer.visit(nodes)

	var b bytes.Buffer
	err = hclPrinter.Fprint(&b, nodes)
	if err != nil {
		return nil, fmt.Errorf("error writing HCL: %v", err)
	}
	s := b.String()

	// Remove extra whitespace...
	s = strings.ReplaceAll(s, "\n\n", "\n")

	// ...but leave whitespace between resources
	s = strings.ReplaceAll(s, "}\nresource", "}\n\nresource")

	// Apply Terraform style (alignment etc.)
	formatted, err := hclPrinter.Format([]byte(s))
	if err != nil {
		return nil, err
	}
	// hack for support terraform 0.12
	formatted = terraform12Adjustments(formatted, mapsObjects)
	// hack for support terraform 0.13
	formatted = terraform13Adjustments(formatted)
	if err != nil {
		log.Println("Invalid HCL follows:")
		for i, line := range strings.Split(s, "\n") {
			fmt.Printf("%4d|\t%s\n", i+1, line)
		}
		return nil, fmt.Errorf("error formatting HCL: %v", err)
	}

	return formatted, nil
}

func terraform12Adjustments(formatted []byte, mapsObjects map[string]struct{}) []byte {
	singletonListFix := regexp.MustCompile(`^\s*\w+ = {`)
	singletonListFixEnd := regexp.MustCompile(`^\s*}`)

	s := string(formatted)
	old := " = {"
	newEquals := " {"
	lines := strings.Split(s, "\n")
	prefix := make([]string, 0)
	for i, line := range lines {
		if singletonListFixEnd.MatchString(line) && len(prefix) > 0 {
			prefix = prefix[:len(prefix)-1]
			continue
		}
		if !singletonListFix.MatchString(line) {
			continue
		}
		key := strings.Trim(strings.Split(line, old)[0], " ")
		prefix = append(prefix, key)
		if _, exist := mapsObjects[strings.Join(prefix, ".")]; exist {
			continue
		}
		lines[i] = strings.ReplaceAll(line, old, newEquals)
	}
	s = strings.Join(lines, "\n")
	return []byte(s)
}

func terraform13Adjustments(formatted []byte) []byte {
	s := string(formatted)
	requiredProvidersRe := regexp.MustCompile("required_providers \".*\" {")
	endBraceRe := regexp.MustCompile(`^\s*}`)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if requiredProvidersRe.MatchString(line) {
			parts := strings.Split(strings.TrimSpace(line), " ")
			provider := strings.ReplaceAll(parts[1], "\"", "")
			lines[i] = "\trequired_providers {"
			var innerBlock []string
			inner := i + 1
			for ; !endBraceRe.MatchString(lines[inner]); inner++ {
				innerBlock = append(innerBlock, "\t"+lines[inner])
			}
			lines[i+1] = "\t\t" + provider + " = {\n" + strings.Join(innerBlock, "\n") + "\n\t\t}"
			lines = append(lines[:i+2], lines[inner:]...)
			break
		}
	}
	s = strings.Join(lines, "\n")
	return []byte(s)
}

func escapeRune(s string) string {
	return fmt.Sprintf("-%04X-", s)
}

// Sanitize name for terraform style
func TfSanitize(name string) string {
	name = unsafeChars.ReplaceAllStringFunc(name, escapeRune)
	name = "tfer--" + name
	return name
}

// Print hcl file from TerraformResource + provider
func HclPrintResource(resources []Resource, providerData map[string]interface{}, output string, sort bool) ([]byte, error) {
	resourcesByType := map[string]map[string]interface{}{}
	mapsObjects := map[string]struct{}{}
	indexRe := regexp.MustCompile(`\.[0-9]+`)

	hintsByResource := make(map[string]map[string][]string)

	for _, res := range resources {
		r := resourcesByType[res.InstanceInfo.Type]
		if r == nil {
			r = make(map[string]interface{})
			resourcesByType[res.InstanceInfo.Type] = r
		}
		if r[res.ResourceName] != nil {
			log.Printf("[ERR]: duplicate resource found: %s.%s", res.InstanceInfo.Type, res.ResourceName)
			continue
		}
		r[res.ResourceName] = res.Item

		for k := range res.InstanceState.Attributes {
			if strings.HasSuffix(k, ".%") {
				key := strings.TrimSuffix(k, ".%")
				mapsObjects[indexRe.ReplaceAllString(key, "")] = struct{}{}
			}
		}

		if len(res.PreserveOrder) > 0 {
			if hintsByResource[res.InstanceInfo.Type] == nil {
				hintsByResource[res.InstanceInfo.Type] = make(map[string][]string)
			}
			hintsByResource[res.InstanceInfo.Type][res.ResourceName] = res.PreserveOrder
		}
	}

	data := map[string]interface{}{}
	if len(resourcesByType) > 0 {
		data["resource"] = resourcesByType
	}
	if len(providerData) > 0 {
		data["provider"] = providerData
	}

	hclBytes, err := Print(data, mapsObjects, output, sort, hintsByResource)
	if err != nil {
		return []byte{}, err
	}
	return hclBytes, nil
}
