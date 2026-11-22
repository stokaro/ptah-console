package release_test

import (
	"os"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/release"
)

var window = []string{"1.36", "1.37", "1.38"}

func releasedUpTo(version string) func(string) bool {
	return func(console string) bool { return console <= version }
}

func twoRows() release.Record {
	return release.Record{SchemaVersion: 1, Releases: []release.Row{
		{Console: "0.4.0", Operator: "v0.2.14", Kubernetes: []string{"1.35", "1.36", "1.37"}},
		{Console: "0.3.2", Operator: "v0.2.9", Kubernetes: []string{"1.35", "1.36", "1.37"}},
	}}
}

func TestMoveNamesTheOperatorInTheFirstRow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		released func(string) bool
		want     []release.Row
	}{
		{name: "a released first row stays, under a row for the next patch", released: releasedUpTo("0.4.0"), want: []release.Row{
			{Console: "0.4.1", Operator: "v0.2.16", Kubernetes: window},
			{Console: "0.4.0", Operator: "v0.2.14", Kubernetes: []string{"1.35", "1.36", "1.37"}},
			{Console: "0.3.2", Operator: "v0.2.9", Kubernetes: []string{"1.35", "1.36", "1.37"}},
		}},
		{name: "a first row no tag names takes the operator in place", released: releasedUpTo("0.3.2"), want: []release.Row{
			{Console: "0.4.0", Operator: "v0.2.16", Kubernetes: window},
			{Console: "0.3.2", Operator: "v0.2.9", Kubernetes: []string{"1.35", "1.36", "1.37"}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			before := twoRows()

			moved, err := release.Move(before, "v0.2.16", window, test.released)

			c.Assert(err, qt.IsNil)
			c.Assert(moved.Releases, qt.DeepEquals, test.want)
			c.Assert(moved.SchemaVersion, qt.Equals, 1)
			c.Assert(before, qt.DeepEquals, twoRows(), qt.Commentf("Move changed the record it was given"))
		})
	}
}

func TestMoveRefusesWhatItCannotMove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		record     release.Record
		operator   string
		kubernetes []string
		says       string
	}{
		{name: "a record with no row", record: release.Record{SchemaVersion: 1},
			operator: "v0.2.16", kubernetes: window, says: "the record names no release to move from"},
		{name: "an operator that is not a module version", record: twoRows(),
			operator: "0.2.16", kubernetes: window, says: `operator "0.2.16" is not a module version such as v0.2.5`},
		{name: "a window with no minor", record: twoRows(),
			operator: "v0.2.16", says: "the operator's window names no Kubernetes minor"},
		{name: "a first row that is not a version", record: release.Record{SchemaVersion: 1, Releases: []release.Row{
			{Console: "0.4", Operator: "v0.2.14", Kubernetes: window}}},
			operator: "v0.2.16", kubernetes: window, says: `console "0.4" is not a version such as 0.1.0`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			_, err := release.Move(test.record, test.operator, test.kubernetes, releasedUpTo("0.4.0"))

			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

// The repository's own record is the format: writing what was read gives back
// the same bytes, so a move changes only the rows it moves.
func TestFormatWritesTheRecordTheWayTheRepositoryDoes(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	data, err := os.ReadFile("../../support/compatibility.json")
	c.Assert(err, qt.IsNil)

	record, err := release.Parse(data)

	c.Assert(err, qt.IsNil)
	c.Assert(len(record.Releases) > 1, qt.IsTrue, qt.Commentf("the record has %d rows", len(record.Releases)))
	c.Assert(string(release.Format(record)), qt.Equals, string(data))
}

const page = `# Compatibility

| Console | Operator | Kubernetes |
| --- | --- | --- |
| 0.4.0 | v0.2.14 | 1.35, 1.36, 1.37 |
| 0.3.2 | v0.2.9 | 1.35, 1.36, 1.37 |

Before v1 nothing is promised.
`

func TestMoveTableShowsTheMovedFirstRow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		released func(string) bool
		want     string
	}{
		{name: "above a released row", released: releasedUpTo("0.4.0"), want: strings.Replace(page,
			"| 0.4.0 | v0.2.14 |", "| 0.4.1 | v0.2.16 | 1.36, 1.37, 1.38 |\n| 0.4.0 | v0.2.14 |", 1)},
		{name: "in place of a row no tag names", released: releasedUpTo("0.3.2"), want: strings.Replace(page,
			"| 0.4.0 | v0.2.14 | 1.35, 1.36, 1.37 |", "| 0.4.0 | v0.2.16 | 1.36, 1.37, 1.38 |", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			moved, err := release.Move(twoRows(), "v0.2.16", window, test.released)
			c.Assert(err, qt.IsNil)

			table, err := release.MoveTable(page, twoRows(), moved)

			c.Assert(err, qt.IsNil)
			c.Assert(table, qt.Equals, test.want)
		})
	}
}

func TestMoveTableRefusesAPageThatDisagreesWithTheRecord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		page string
		says string
	}{
		{name: "a first row the record does not have",
			page: strings.Replace(page, "| 0.4.0 | v0.2.14 |", "| 0.4.0 | v0.2.13 |", 1),
			says: `docs/compatibility.md's first row is "\| 0.4.0 \| v0.2.13 \| 1.35, 1.36, 1.37 \|", and the record's is "\| 0.4.0 \| v0.2.14 \| 1.35, 1.36, 1.37 \|"`},
		{name: "no table", page: "# Compatibility\n", says: "docs/compatibility.md has 0 tables, not one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			moved, err := release.Move(twoRows(), "v0.2.16", window, releasedUpTo("0.4.0"))
			c.Assert(err, qt.IsNil)

			_, err = release.MoveTable(test.page, twoRows(), moved)

			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

const chart = `apiVersion: v2
name: ptah-console
type: application
version: 0.4.0
appVersion: "0.4.0"
kubeVersion: ">=1.35.0-0 <1.38.0-0"
dependencies:
  - name: library
    version: 1.2.3
`

func TestSetChartVersionChangesOnlyTheTwoVersions(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	set, err := release.SetChartVersion([]byte(chart), "0.4.1")

	c.Assert(err, qt.IsNil)
	c.Assert(string(set), qt.Equals, strings.Replace(chart,
		"version: 0.4.0\nappVersion: \"0.4.0\"", "version: 0.4.1\nappVersion: \"0.4.1\"", 1))
}

func TestSetChartVersionRefusesAChartItCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		chart string
		says  string
	}{
		{name: "no version", chart: strings.Replace(chart, "version: 0.4.0\n", "", 1),
			says: `the chart has 0 lines matching \(\?m\)\^version: \.\*\$, not one`},
		{name: "no appVersion", chart: strings.Replace(chart, "appVersion: \"0.4.0\"\n", "", 1),
			says: `the chart has 0 lines matching \(\?m\)\^appVersion: \.\*\$, not one`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			_, err := release.SetChartVersion([]byte(test.chart), "0.4.1")

			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

// What a move writes is what Check holds the tree to.
func TestAMovedTreePassesCheck(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	moved, err := release.Move(twoRows(), "v0.2.16", window, releasedUpTo("0.4.0"))
	c.Assert(err, qt.IsNil)
	set, err := release.SetChartVersion([]byte(chart), moved.Releases[0].Console)
	c.Assert(err, qt.IsNil)
	c.Assert(string(set), qt.Contains, "version: 0.4.1\n")

	err = release.Check(release.Inputs{
		Record:       release.Format(moved),
		ChartVersion: "0.4.1", ChartAppVersion: "0.4.1",
		AgentChartVersion: "0.4.1", AgentChartAppVersion: "0.4.1",
		Operator:   "v0.2.16",
		Kubernetes: window,
	})

	c.Assert(err, qt.IsNil)
}
