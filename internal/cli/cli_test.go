package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/cli"
)

func run(args ...string) (code int, stdout, stderr string) {
	return runWithInput("", args...)
}

// runWithInput runs the command with input on its standard input, as a pipe
// gives it: not a terminal.
func runWithInput(input string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = cli.Run(context.Background(), args, strings.NewReader(input), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	code, stdout, stderr := run("version")

	c.Assert(code, qt.Equals, cli.ExitOK)
	c.Assert(stderr, qt.Equals, "")
	c.Assert(stdout, qt.Matches, `ptah-console \S+ \(commit \S+(, modified)?, go\S+, with(out)? the web app\)\n`)
	c.Assert(stdout, qt.Contains, runtime.Version())
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	code, stdout, _ := run("version", "-o", "json")

	c.Assert(code, qt.Equals, cli.ExitOK)
	var info map[string]any
	c.Assert(json.Unmarshal([]byte(stdout), &info), qt.IsNil)
	c.Assert(info["goVersion"], qt.Equals, runtime.Version())
	c.Assert(info["version"], qt.Not(qt.Equals), "")
	c.Assert(info["commit"], qt.Not(qt.Equals), "")
	c.Assert(info["webApp"], qt.IsFalse, qt.Commentf("go test builds without the with_frontend tag"))
}

func TestUsageMistakes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		says string
	}{
		{name: "unknown command", args: []string{"approve-everything"}, says: "unknown command"},
		{name: "unknown flag", args: []string{"version", "--fast"}, says: "unknown flag"},
		{name: "unknown output", args: []string{"version", "-o", "yaml"}, says: `--output "yaml"`},
		{name: "extra argument", args: []string{"version", "now"}, says: "accepts 0 arg"},
		{name: "unknown log level", args: []string{"--log-level", "loud", "version"}, says: `--log-level "loud"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, stderr := run(test.args...)

			c.Assert(code, qt.Equals, cli.ExitUsage, qt.Commentf("stderr: %s", stderr))
			c.Assert(stdout, qt.Equals, "")
			c.Assert(strings.HasPrefix(stderr, "ptah-console: "), qt.IsTrue)
			c.Assert(stderr, qt.Contains, test.says)
		})
	}
}

func TestHelpIsNotAMistake(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	code, stdout, stderr := run("--help")

	c.Assert(code, qt.Equals, cli.ExitOK)
	c.Assert(stderr, qt.Equals, "")
	c.Assert(stdout, qt.Contains, "version")
}
