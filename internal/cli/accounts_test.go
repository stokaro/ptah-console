package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"golang.org/x/crypto/bcrypt"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/cli"
)

func TestAccountsHashReadsThePasswordFromStandardInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, password string
	}{
		{name: "one line", input: "s3cret pass\n", password: "s3cret pass"},
		{name: "one line from Windows", input: "s3cret pass\r\n", password: "s3cret pass"},
		{name: "no line end", input: "s3cret pass", password: "s3cret pass"},
		{name: "as long as bcrypt reads", input: strings.Repeat("p", 72) + "\n", password: strings.Repeat("p", 72)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, stderr := runWithInput(test.input, "accounts", "hash", "--cost", "10")

			c.Assert(code, qt.Equals, cli.ExitOK, qt.Commentf("%s", stderr))
			c.Assert(stdout, qt.Matches, `\$2a\$10\$\S{53}\n`)
			hash := []byte(strings.TrimSuffix(stdout, "\n"))
			c.Assert(bcrypt.CompareHashAndPassword(hash, []byte(test.password)), qt.IsNil)
			c.Assert(stderr, qt.Not(qt.Contains), test.password)
		})
	}
}

func TestAccountsHashUsesTheDefaultCost(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	code, stdout, _ := runWithInput("s3cret\n", "accounts", "hash")

	c.Assert(code, qt.Equals, cli.ExitOK)
	cost, err := bcrypt.Cost([]byte(strings.TrimSuffix(stdout, "\n")))
	c.Assert(err, qt.IsNil)
	c.Assert(cost, qt.Equals, accounts.DefaultCost)
}

func TestAccountsHashRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		args  []string
		code  int
		says  string
	}{
		{name: "nothing", input: "", code: cli.ExitFailed, says: "the password is empty"},
		{name: "an empty line", input: "\n", code: cli.ExitFailed, says: "the password is empty"},
		{name: "one byte past what bcrypt reads", input: strings.Repeat("p", 73) + "\n", code: cli.ExitFailed,
			says: "longer than the 72 bytes bcrypt reads"},
		{name: "two lines", input: "first\nsecond\n", code: cli.ExitFailed, says: "more than one line"},
		{name: "a whole file", input: strings.Repeat("p", 4097), code: cli.ExitFailed, says: "more than a password"},
		{name: "a low cost", input: "s3cret\n", args: []string{"--cost", "9"}, code: cli.ExitFailed,
			says: "cost 9 is outside 10 to 31"},
		{name: "the password as an argument", input: "", args: []string{"s3cret"}, code: cli.ExitUsage,
			says: "accepts 0 arg"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, stderr := runWithInput(test.input, append([]string{"accounts", "hash"}, test.args...)...)

			c.Assert(code, qt.Equals, test.code)
			c.Assert(stdout, qt.Equals, "")
			c.Assert(stderr, qt.Contains, test.says)
		})
	}
}

func TestCommandGroupsReportAnUnknownCommandAsAUsageMistake(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "accounts", args: []string{"accounts", "create"}},
		{name: "config", args: []string{"config", "apply"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, _, stderr := run(test.args...)

			c.Assert(code, qt.Equals, cli.ExitUsage)
			c.Assert(stderr, qt.Contains, "unknown command")
		})
	}
}

// hostedFiles writes a configuration and the passwords for it, and returns
// their paths.
func hostedFiles(t *testing.T, passwords string) (configPath, passwordsPath string) {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	configPath, passwordsPath = filepath.Join(dir, "configuration.yaml"), filepath.Join(dir, "passwords")
	c.Assert(os.WriteFile(configPath, []byte(`apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: staging
publicURL: https://console.staging.example.com
namespaces: [storefront]
accounts:
  - id: alex
    login: alex@example.com
  - id: sam
    login: sam@example.com
`), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(passwordsPath, []byte(passwords), 0o600), qt.IsNil)
	return configPath, passwordsPath
}

func hashed(t *testing.T, password string) string {
	t.Helper()
	hash, err := accounts.Hash([]byte(password), accounts.MinCost)
	qt.New(t).Assert(err, qt.IsNil)
	return string(hash)
}

func TestConfigValidatePrintsEachAccountsKubernetesUsername(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	configPath, passwordsPath := hostedFiles(t,
		"alex:"+hashed(t, "alex-secret")+"\nsam:"+hashed(t, "sam-secret")+"\n")

	code, stdout, stderr := run("config", "validate", "--config", configPath, "--passwords", passwordsPath)

	c.Assert(code, qt.Equals, cli.ExitOK, qt.Commentf("%s", stderr))
	c.Assert(stdout, qt.Equals, "Valid for installation staging, in namespaces storefront.\nThe accounts act as:\n"+
		"  ptah-console:staging:alex  (alex@example.com)\n"+
		"  ptah-console:staging:sam  (sam@example.com)\n")
}

func TestConfigValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	configPath, passwordsPath := hostedFiles(t, "alex:{SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=\n")

	code, stdout, stderr := run("config", "validate", "--config", configPath, "--passwords", passwordsPath)

	c.Assert(code, qt.Equals, cli.ExitFailed)
	c.Assert(stdout, qt.Equals, "")
	c.Assert(stderr, qt.Equals, "ptah-console: "+passwordsPath+": refused:\n"+
		"  - line 1: the hash for alex is SHA-1; only bcrypt is accepted\n"+
		"  - accounts[1] (sam) has no password\n")
}

func TestConfigValidateNeedsBothFiles(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	configPath, _ := hostedFiles(t, "")

	code, _, stderr := run("config", "validate", "--config", configPath)

	c.Assert(code, qt.Equals, cli.ExitUsage)
	c.Assert(stderr, qt.Contains, "--config and --passwords are both required")
}
