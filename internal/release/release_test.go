package release_test

import (
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/release"
)

const record = `{
  "schemaVersion": 1,
  "releases": [
    {"console": "0.1.0", "operator": "v0.2.5", "kubernetes": ["1.35", "1.36", "1.37"]}
  ]
}`

func inputs(change func(*release.Inputs)) release.Inputs {
	in := release.Inputs{
		Record:       []byte(record),
		ChartVersion: "0.1.0", ChartAppVersion: "0.1.0",
		Operator:   "v0.2.5",
		Kubernetes: []string{"1.35", "1.36", "1.37"},
		Version:    "0.1.0",
		Release:    true,
	}
	change(&in)
	return in
}

func TestARecordThatMatchesItsTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*release.Inputs)
	}{
		{name: "a release", change: func(*release.Inputs) {}},
		{name: "a commit between releases, built against an operator commit", change: func(in *release.Inputs) {
			in.Record = []byte(strings.Replace(record, `"v0.2.5"`, `"v0.2.6-0.20261021000000-abcdef012345"`, 1))
			in.Operator = "v0.2.6-0.20261021000000-abcdef012345"
			in.Version, in.Release = "", false
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			c.Assert(release.Check(inputs(test.change)), qt.IsNil)
		})
	}
}

func TestEachDisagreement(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*release.Inputs)
		says   string
	}{
		{name: "a field the record does not have",
			change: func(in *release.Inputs) { in.Record = []byte(`{"schemaVersion": 1, "releases": [], "notes": ""}`) },
			says:   `support/compatibility.json: json: unknown field "notes"`},
		{name: "another schema version",
			change: func(in *release.Inputs) {
				in.Record = []byte(strings.Replace(record, `"schemaVersion": 1`, `"schemaVersion": 2`, 1))
			},
			says: `(?s).*schemaVersion is 2, not 1.*`},
		{name: "no release", change: func(in *release.Inputs) { in.Record = []byte(`{"schemaVersion": 1, "releases": []}`) },
			says: `support/compatibility.json names no release`},
		{name: "a console that is not a version",
			change: func(in *release.Inputs) { in.Record = []byte(strings.Replace(record, `"0.1.0"`, `"v0.1.0"`, 1)) },
			says:   `(?s).*releases\[0\].console "v0.1.0" is not a version such as 0.1.0.*`},
		{name: "a console twice",
			change: func(in *release.Inputs) {
				in.Record = []byte(strings.Replace(record, `]
}`, `, {"console": "0.1.0", "operator": "v0.2.4", "kubernetes": ["1.35"]}]
}`, 1))
			},
			says: `(?s).*releases\[1\] repeats console 0.1.0 from releases\[0\].*`},
		{name: "an operator that is no module version",
			change: func(in *release.Inputs) {
				in.Record = []byte(strings.Replace(record, `"v0.2.5"`, `"0.2.5"`, 1))
				in.Operator = "0.2.5"
			},
			says: `(?s).*releases\[0\].operator "0.2.5" is not a module version such as v0.2.5.*`},
		{name: "a Kubernetes version that is no minor",
			change: func(in *release.Inputs) {
				in.Record = []byte(strings.Replace(record, `"1.37"`, `"1.37.0"`, 1))
				in.Kubernetes = []string{"1.35", "1.36", "1.37.0"}
			},
			says: `(?s).*releases\[0\].kubernetes has "1.37.0", which is not a minor such as 1.37.*`},
		{name: "a chart of another version", change: func(in *release.Inputs) { in.ChartVersion = "0.2.0" },
			says: `(?s).*releases\[0\] is console 0.1.0, and the chart is version 0.2.0 with appVersion 0.1.0; they name one version.*`},
		{name: "a chart whose appVersion differs", change: func(in *release.Inputs) { in.ChartAppVersion = "0.0.9" },
			says: `(?s).*the chart is version 0.1.0 with appVersion 0.0.9.*`},
		{name: "another operator in go.mod", change: func(in *release.Inputs) { in.Operator = "v0.2.6" },
			says: `(?s).*releases\[0\] names operator v0.2.5, and go.mod requires v0.2.6.*`},
		{name: "another Kubernetes window", change: func(in *release.Inputs) { in.Kubernetes = []string{"1.36", "1.37", "1.38"} },
			says: `(?s).*releases\[0\] names Kubernetes 1.35, 1.36, 1.37, and the operator's window is 1.36, 1.37, 1.38.*`},
		{name: "a release of another version", change: func(in *release.Inputs) { in.Version = "0.1.1" },
			says: `(?s).*the release is 0.1.1, and releases\[0\] is console 0.1.0.*`},
		{name: "a release built against an operator commit",
			change: func(in *release.Inputs) {
				in.Record = []byte(strings.Replace(record, `"v0.2.5"`, `"v0.2.6-0.20261021000000-abcdef012345"`, 1))
				in.Operator = "v0.2.6-0.20261021000000-abcdef012345"
			},
			says: `(?s).*names operator v0.2.6-0.20261021000000-abcdef012345, which is not a release; a release names a released operator.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			err := release.Check(inputs(test.change))

			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}
