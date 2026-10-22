// Command crdschemas writes the JSON schema of every version of every CRD in
// a directory where kubeconform looks schemas up, as
// <group>/<kind>_<version>.json with the kind in lowercase.
//
// An object schema that declares its properties is closed to any other, as
// kubeconform's -strict does for the built-in kinds, unless the CRD keeps
// unknown fields there. hack/chart-conform.sh runs this over the chart's
// crds directory, so the registry's entries are checked against the schema
// the chart installs.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: crdschemas <crd directory> <schema directory>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "crdschemas:", err)
		os.Exit(1)
	}
}

type crd struct {
	Spec struct {
		Group string `json:"group"`
		Names struct {
			Kind string `json:"kind"`
		} `json:"names"`
		Versions []struct {
			Name   string `json:"name"`
			Schema struct {
				OpenAPIV3Schema map[string]any `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

func run(in, out string) error {
	files, err := filepath.Glob(filepath.Join(in, "*.yaml"))
	if err != nil {
		return err
	}
	written := 0
	for _, file := range files {
		data, err := os.ReadFile(file) // #nosec G304 G703 -- a CRD of this repository's chart, named by the make target.
		if err != nil {
			return err
		}
		var definition crd
		if err := yaml.Unmarshal(data, &definition); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if definition.Spec.Group == "" || definition.Spec.Names.Kind == "" {
			return fmt.Errorf("%s: not a CustomResourceDefinition", file)
		}
		dir := filepath.Join(out, definition.Spec.Group)
		if err := os.MkdirAll(dir, 0o750); err != nil { // #nosec G703 -- the scratch directory the make target names.
			return err
		}
		for _, version := range definition.Spec.Versions {
			schema := version.Schema.OpenAPIV3Schema
			if schema == nil {
				return fmt.Errorf("%s: version %s has no schema", file, version.Name)
			}
			closeObjects(schema)
			encoded, err := json.MarshalIndent(schema, "", "  ")
			if err != nil {
				return err
			}
			name := strings.ToLower(definition.Spec.Names.Kind) + "_" + version.Name + ".json"
			if err := os.WriteFile(filepath.Join(dir, name), encoded, 0o600); err != nil { // #nosec G703 -- under the scratch directory the make target names.
				return err
			}
			written++
		}
	}
	if written == 0 {
		return errors.New("no CRD schema under " + in)
	}
	return nil
}

// closeObjects refuses undeclared properties in every object schema that
// declares some, except where the CRD keeps unknown fields.
func closeObjects(schema map[string]any) {
	properties, declared := schema["properties"].(map[string]any)
	preserved, _ := schema["x-kubernetes-preserve-unknown-fields"].(bool)
	if declared && !preserved {
		if _, set := schema["additionalProperties"]; !set {
			schema["additionalProperties"] = false
		}
	}
	for _, property := range properties {
		if nested, ok := property.(map[string]any); ok {
			closeObjects(nested)
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		closeObjects(items)
	}
	if additional, ok := schema["additionalProperties"].(map[string]any); ok {
		closeObjects(additional)
	}
}
