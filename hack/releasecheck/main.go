// Command releasecheck holds support/compatibility.json to the tree it is in:
// the chart's version, the operator module go.mod requires, and that
// operator's Kubernetes support window.
//
//	go run ./hack/releasecheck                          the record matches this tree
//	go run ./hack/releasecheck -version 0.1.0           and this tree is 0.1.0
//	go run ./hack/releasecheck -version 0.1.0 -release  and the operator is a release
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/release"
)

func main() {
	version := flag.String("version", "", "the version being released, without the v")
	isRelease := flag.Bool("release", false, "require the operator to be a released version")
	flag.Parse()
	if err := run(*version, *isRelease); err != nil {
		fmt.Fprintln(os.Stderr, "releasecheck:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "support/compatibility.json matches this tree")
}

func run(version string, isRelease bool) error {
	record, err := os.ReadFile("support/compatibility.json")
	if err != nil {
		return err
	}
	chartFile, err := os.ReadFile("charts/ptah-console/Chart.yaml")
	if err != nil {
		return err
	}
	var chart struct {
		Version    string `json:"version"`
		AppVersion string `json:"appVersion"`
	}
	if err := yaml.Unmarshal(chartFile, &chart); err != nil {
		return fmt.Errorf("Chart.yaml: %w", err)
	}
	operator, err := operatorModule()
	if err != nil {
		return err
	}
	support, err := os.ReadFile(filepath.Join(operator.Dir, "support", "kubernetes.json"))
	if err != nil {
		return err
	}
	var window struct {
		Releases []struct {
			Minor string `json:"minor"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(support, &window); err != nil {
		return fmt.Errorf("the operator's support/kubernetes.json: %w", err)
	}
	minors := make([]string, 0, len(window.Releases))
	for _, entry := range window.Releases {
		minors = append(minors, entry.Minor)
	}
	return release.Check(release.Inputs{
		Record:       record,
		ChartVersion: chart.Version, ChartAppVersion: chart.AppVersion,
		Operator:   operator.Version,
		Kubernetes: minors,
		Version:    version,
		Release:    isRelease,
	})
}

// operatorModule is the operator module go.mod requires, downloaded first:
// go list -m names no directory for a module nothing has downloaded yet.
func operatorModule() (struct{ Dir, Version string }, error) {
	var module struct{ Dir, Version string }
	downloaded, err := exec.Command("go", "mod", "download", "-json", "github.com/stokaro/ptah-operator").Output()
	if err != nil {
		return module, fmt.Errorf("download the operator module: %w", err)
	}
	if err := json.Unmarshal(downloaded, &module); err != nil || module.Dir == "" || module.Version == "" {
		return module, fmt.Errorf("download the operator module: %q", downloaded)
	}
	return module, nil
}
