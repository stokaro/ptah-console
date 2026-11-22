package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Move is the record after the console moves to another operator, as
// docs/releases.md describes the change. released reports whether a console
// version, such as 0.4.0, has been released. When the first row's version has,
// a new first row names the next patch version, the operator and its
// Kubernetes window, and the released row stays as it was. When it has not,
// the first row takes the operator and its window in place, so a second move
// before a release adds no second row.
//
// The person who releases may still choose another version than the next
// patch; the record only has to name the one the charts carry.
func Move(record Record, operator string, kubernetes []string, released func(console string) bool) (Record, error) {
	if len(record.Releases) == 0 {
		return Record{}, errors.New("the record names no release to move from")
	}
	if !strings.HasPrefix(operator, "v") {
		return Record{}, fmt.Errorf("operator %q is not a module version such as v0.2.5", operator)
	}
	if len(kubernetes) == 0 {
		return Record{}, errors.New("the operator's window names no Kubernetes minor")
	}
	current := record.Releases[0]
	row := Row{Console: current.Console, Operator: operator, Kubernetes: append([]string(nil), kubernetes...)}
	rows := append([]Row(nil), record.Releases...)
	if !released(current.Console) {
		rows[0] = row
		return Record{SchemaVersion: record.SchemaVersion, Releases: rows}, nil
	}
	next, err := nextPatch(current.Console)
	if err != nil {
		return Record{}, err
	}
	row.Console = next
	return Record{SchemaVersion: record.SchemaVersion, Releases: append([]Row{row}, rows...)}, nil
}

func nextPatch(version string) (string, error) {
	parts := semver.FindStringSubmatch(version)
	if parts == nil {
		return "", fmt.Errorf("console %q is not a version such as 0.1.0", version)
	}
	patch, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", fmt.Errorf("console %q: %w", version, err)
	}
	return fmt.Sprintf("%s.%s.%d", parts[1], parts[2], patch+1), nil
}

// Format writes the record the way support/compatibility.json is written: one
// object per row, its Kubernetes minors on one line.
func Format(record Record) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "{\n  \"schemaVersion\": %d,\n  \"releases\": [\n", record.SchemaVersion)
	for i, row := range record.Releases {
		minors := make([]string, 0, len(row.Kubernetes))
		for _, minor := range row.Kubernetes {
			minors = append(minors, quote(minor))
		}
		fmt.Fprintf(&out, "    {\n      \"console\": %s,\n      \"operator\": %s,\n      \"kubernetes\": [%s]\n    }",
			quote(row.Console), quote(row.Operator), strings.Join(minors, ", "))
		if i < len(record.Releases)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("  ]\n}\n")
	return out.Bytes()
}

func quote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		// A string always encodes.
		panic(err)
	}
	return string(encoded)
}

// Parse reads support/compatibility.json as Check does, refusing a field the
// record does not have.
func Parse(data []byte) (Record, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("support/compatibility.json: %w", err)
	}
	return record, nil
}

// TableRow is the row docs/compatibility.md shows for a record row.
func TableRow(row Row) string {
	return fmt.Sprintf("| %s | %s | %s |", row.Console, row.Operator, strings.Join(row.Kubernetes, ", "))
}

const tableRule = "| --- | --- | --- |\n"

// MoveTable is docs/compatibility.md after Move: its first row becomes the
// record's new first row, inserted above the released one or in place of an
// unreleased one. It refuses a page whose first row is not the record's old
// first row, since then the page and the record disagree already.
func MoveTable(page string, before, after Record) (string, error) {
	if len(before.Releases) == 0 || len(after.Releases) == 0 {
		return "", errors.New("a record names no release")
	}
	if strings.Count(page, tableRule) != 1 {
		return "", fmt.Errorf("docs/compatibility.md has %d tables, not one", strings.Count(page, tableRule))
	}
	start := strings.Index(page, tableRule) + len(tableRule)
	end := strings.IndexByte(page[start:], '\n')
	if end < 0 {
		return "", errors.New("docs/compatibility.md's table has no row")
	}
	first := page[start : start+end]
	if first != TableRow(before.Releases[0]) {
		return "", fmt.Errorf("docs/compatibility.md's first row is %q, and the record's is %q", first, TableRow(before.Releases[0]))
	}
	row := TableRow(after.Releases[0])
	if len(after.Releases) > len(before.Releases) {
		return page[:start] + row + "\n" + page[start:], nil
	}
	return page[:start] + row + page[start+end:], nil
}

var (
	chartVersion    = regexp.MustCompile(`(?m)^version: .*$`)
	chartAppVersion = regexp.MustCompile(`(?m)^appVersion: .*$`)
)

// SetChartVersion is a Chart.yaml whose version and appVersion name version,
// with every other byte kept.
func SetChartVersion(chart []byte, version string) ([]byte, error) {
	for _, field := range []*regexp.Regexp{chartVersion, chartAppVersion} {
		if found := len(field.FindAll(chart, -1)); found != 1 {
			return nil, fmt.Errorf("the chart has %d lines matching %s, not one", found, field)
		}
	}
	chart = chartVersion.ReplaceAllLiteral(chart, []byte("version: "+version))
	return chartAppVersion.ReplaceAllLiteral(chart, []byte("appVersion: "+quote(version))), nil
}
