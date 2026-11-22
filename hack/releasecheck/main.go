// Command releasecheck holds support/compatibility.json to the tree it is in:
// the two charts' versions, the operator module go.mod requires, and that
// operator's Kubernetes support window.
//
//	go run ./hack/releasecheck                          the record matches this tree
//	go run ./hack/releasecheck -version 0.1.0           and this tree is 0.1.0
//	go run ./hack/releasecheck -version 0.1.0 -release  and the operator is a release
//	go run ./hack/releasecheck -move                    move the record to the operator go.mod requires
//
// -move is the record's part of moving the operator, after go get and make
// generate: it adds a row for the next patch version above a released first
// row, or rewrites a first row no tag names yet, and sets both charts and the
// table in docs/compatibility.md to that row. Then it checks the tree as
// without the flag.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/release"
)

func main() {
	version := flag.String("version", "", "the version being released, without the v")
	isRelease := flag.Bool("release", false, "require the operator to be a released version")
	move := flag.Bool("move", false, "move the record, both charts and docs/compatibility.md to the operator go.mod requires, then check")
	flag.Parse()
	if *move {
		if err := moveRecord(); err != nil {
			fmt.Fprintln(os.Stderr, "releasecheck:", err)
			os.Exit(1)
		}
	}
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
	chart, err := readChart(charts[0])
	if err != nil {
		return err
	}
	agentChart, err := readChart(charts[1])
	if err != nil {
		return err
	}
	operator, minors, err := operatorWindow()
	if err != nil {
		return err
	}
	return release.Check(release.Inputs{
		Record:       record,
		ChartVersion: chart.Version, ChartAppVersion: chart.AppVersion,
		AgentChartVersion: agentChart.Version, AgentChartAppVersion: agentChart.AppVersion,
		Operator:   operator.Version,
		Kubernetes: minors,
		Version:    version,
		Release:    isRelease,
	})
}

// charts are the two charts released under the console's version.
var charts = []string{"charts/ptah-console/Chart.yaml", "charts/ptah-console-agent/Chart.yaml"}

// moveRecord writes the record, both charts and docs/compatibility.md for the
// operator go.mod requires.
func moveRecord() error {
	data, err := os.ReadFile("support/compatibility.json")
	if err != nil {
		return err
	}
	before, err := release.Parse(data)
	if err != nil {
		return err
	}
	if len(before.Releases) == 0 {
		return errors.New("support/compatibility.json names no release")
	}
	operator, minors, err := operatorWindow()
	if err != nil {
		return err
	}
	tags, err := tags()
	if err != nil {
		return err
	}
	after, err := release.Move(before, operator.Version, minors, func(console string) bool {
		return slices.Contains(tags, "v"+console)
	})
	if err != nil {
		return err
	}
	page, err := os.ReadFile("docs/compatibility.md")
	if err != nil {
		return err
	}
	table, err := release.MoveTable(string(page), before, after)
	if err != nil {
		return err
	}
	written := map[string][]byte{
		"support/compatibility.json": release.Format(after),
		"docs/compatibility.md":      []byte(table),
	}
	for _, path := range charts {
		chart, err := os.ReadFile(path) // #nosec G304 -- one of the repository's two charts.
		if err != nil {
			return err
		}
		if written[path], err = release.SetChartVersion(chart, after.Releases[0].Console); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	for path, content := range written {
		if err := os.WriteFile(path, content, 0o644); err != nil { // #nosec G306 -- tracked repository files.
			return err
		}
	}
	return nil
}

// tags are the clone's tags. A clone with none cannot tell a released console
// version, and a released first row taken for an unreleased one would be
// rewritten, so it is refused.
func tags() ([]string, error) {
	listed, err := exec.Command("git", "tag", "--list").Output()
	if err != nil {
		return nil, fmt.Errorf("list the tags: %w", err)
	}
	if len(bytes.TrimSpace(listed)) == 0 {
		return nil, errors.New("this clone has no tags, so it cannot tell a released console version; fetch them first")
	}
	return strings.Split(strings.TrimSpace(string(listed)), "\n"), nil
}

// operatorWindow is the operator module go.mod requires and the Kubernetes
// minors of its support window, oldest first.
func operatorWindow() (struct{ Dir, Version string }, []string, error) {
	operator, err := operatorModule()
	if err != nil {
		return operator, nil, err
	}
	support, err := os.ReadFile(filepath.Join(operator.Dir, "support", "kubernetes.json"))
	if err != nil {
		return operator, nil, err
	}
	var window struct {
		Releases []struct {
			Minor string `json:"minor"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(support, &window); err != nil {
		return operator, nil, fmt.Errorf("the operator's support/kubernetes.json: %w", err)
	}
	minors := make([]string, 0, len(window.Releases))
	for _, entry := range window.Releases {
		minors = append(minors, entry.Minor)
	}
	return operator, minors, nil
}

// chart is what a Chart.yaml says of its version.
type chart struct {
	Version    string `json:"version"`
	AppVersion string `json:"appVersion"`
}

func readChart(path string) (chart, error) {
	var read chart
	data, err := os.ReadFile(path) // #nosec G304 -- one of the repository's two charts.
	if err != nil {
		return read, err
	}
	if err := yaml.Unmarshal(data, &read); err != nil {
		return read, fmt.Errorf("%s: %w", path, err)
	}
	return read, nil
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
