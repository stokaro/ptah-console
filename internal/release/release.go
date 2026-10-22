// Package release holds a release's inputs to each other: the compatibility
// record in support/compatibility.json, the two charts' versions, the
// operator module go.mod requires, and the operator's Kubernetes support
// window.
//
// The record's first row describes the tree it is in. CI checks that row on
// every change, so the record cannot drift from what the console is built
// against; a release checks it once more and requires that the operator it
// names is a release rather than a commit.
package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// SchemaVersion is the record's format.
const SchemaVersion = 1

// Record is support/compatibility.json.
type Record struct {
	SchemaVersion int   `json:"schemaVersion"`
	Releases      []Row `json:"releases"`
}

// Row says which operator and which Kubernetes minors one console version
// was built and tested against.
type Row struct {
	Console    string   `json:"console"`
	Operator   string   `json:"operator"`
	Kubernetes []string `json:"kubernetes"`
}

// Inputs are what the record is checked against, read from the tree.
type Inputs struct {
	// Record is the content of support/compatibility.json.
	Record []byte
	// ChartVersion and ChartAppVersion are Chart.yaml's.
	ChartVersion, ChartAppVersion string
	// AgentChartVersion and AgentChartAppVersion are the agent chart's,
	// which is released with the console under the same version.
	AgentChartVersion, AgentChartAppVersion string
	// Operator is the operator module version go.mod requires.
	Operator string
	// Kubernetes are the minors of the operator's support window, oldest
	// first.
	Kubernetes []string
	// Version, when set, is the version being released, without the v.
	Version string
	// Release requires the operator to be a release: a release names the
	// operator versions it works with, and a commit is not one.
	Release bool
}

var (
	semver       = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	releaseTag   = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	minorVersion = regexp.MustCompile(`^1\.(0|[1-9][0-9]*)$`)
)

// Check returns every way the inputs disagree, or nil.
func Check(inputs Inputs) error {
	decoder := json.NewDecoder(bytes.NewReader(inputs.Record))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return fmt.Errorf("support/compatibility.json: %w", err)
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if record.SchemaVersion != SchemaVersion {
		add("schemaVersion is %d, not %d", record.SchemaVersion, SchemaVersion)
	}
	if len(record.Releases) == 0 {
		return errors.New("support/compatibility.json names no release")
	}
	seen := make(map[string]int)
	for i, row := range record.Releases {
		if !semver.MatchString(row.Console) {
			add("releases[%d].console %q is not a version such as 0.1.0", i, row.Console)
		}
		if first, repeated := seen[row.Console]; repeated {
			add("releases[%d] repeats console %s from releases[%d]", i, row.Console, first)
		}
		seen[row.Console] = i
		if !strings.HasPrefix(row.Operator, "v") {
			add("releases[%d].operator %q is not a module version such as v0.2.5", i, row.Operator)
		}
		if len(row.Kubernetes) == 0 {
			add("releases[%d] names no Kubernetes minor", i)
		}
		for _, minor := range row.Kubernetes {
			if !minorVersion.MatchString(minor) {
				add("releases[%d].kubernetes has %q, which is not a minor such as 1.37", i, minor)
			}
		}
	}

	checkCurrent(record.Releases[0], inputs, add)
	if len(problems) > 0 {
		return errors.New("support/compatibility.json:\n  - " + strings.Join(problems, "\n  - "))
	}
	return nil
}

// checkCurrent holds the record's first row to the tree it is in.
func checkCurrent(current Row, inputs Inputs, add func(format string, args ...any)) {
	if current.Console != inputs.ChartVersion || current.Console != inputs.ChartAppVersion {
		add("releases[0] is console %s, and the chart is version %s with appVersion %s; they name one version",
			current.Console, inputs.ChartVersion, inputs.ChartAppVersion)
	}
	if current.Console != inputs.AgentChartVersion || current.Console != inputs.AgentChartAppVersion {
		add("releases[0] is console %s, and the agent chart is version %s with appVersion %s; the agent is released with the console",
			current.Console, inputs.AgentChartVersion, inputs.AgentChartAppVersion)
	}
	if current.Operator != inputs.Operator {
		add("releases[0] names operator %s, and go.mod requires %s", current.Operator, inputs.Operator)
	}
	if !slices.Equal(current.Kubernetes, inputs.Kubernetes) {
		add("releases[0] names Kubernetes %s, and the operator's window is %s",
			strings.Join(current.Kubernetes, ", "), strings.Join(inputs.Kubernetes, ", "))
	}
	if inputs.Version != "" && inputs.Version != current.Console {
		add("the release is %s, and releases[0] is console %s", inputs.Version, current.Console)
	}
	if inputs.Release && !releaseTag.MatchString(current.Operator) {
		add("releases[0] names operator %s, which is not a release; a release names a released operator", current.Operator)
	}
}
