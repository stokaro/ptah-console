// Package agent is the console agent: the program that runs in a cluster the
// console does not, opens the channel to the console, and serves the
// console's typed requests as the people they name.
//
// The agent acts as nobody the console chooses. A request names a Console
// account; the agent maps it through its own allowlist to one Kubernetes
// username and impersonates exactly that, and refuses an account the
// allowlist does not name, a namespace outside its ceiling, and an operation
// it does not serve, before any call to the API server.
package agent

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

// The kinds of the agent's two files.
const (
	APIVersion        = "console.ptah.run/v1alpha1"
	ConfigurationKind = "AgentConfiguration"
	AccountsKind      = "AgentAccounts"
)

// Bounds on the files.
const (
	maxFileBytes   = 64 << 10
	maxNamespaces  = api.MaxNamespaces
	maxAccounts    = 256
	maxServerName  = 253
	maxAddressSize = 2048
)

// Configuration is what an installation fixes: which connection the agent
// serves and where its console is. A change to any of it is a new
// connection, so it is read once, when the agent starts.
type Configuration struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// InstallationID is the console's, so the usernames the agent acts as
	// are the ones the console's own accounts have:
	// ptah-console:<installationID>:<account>.
	InstallationID string     `json:"installationID"`
	Connection     Connection `json:"connection"`
	Console        Console    `json:"console"`
	// Namespaces is the ceiling. A request that names any other namespace is
	// refused before any call.
	Namespaces []string `json:"namespaces"`
}

// Connection is the registry entry the agent serves.
type Connection struct {
	UID        string `json:"uid"`
	Generation uint64 `json:"generation"`
}

// Console is where the agent dials, and the name the console's certificate
// has to carry.
type Console struct {
	// Address is the https URL of the console's agent listener.
	Address string `json:"address"`
	// ServerName is the name the console was enrolled with. The certificate
	// has to carry it and be issued by the console's own CA.
	ServerName string `json:"serverName"`
}

// Username is the Kubernetes username the agent acts as for an account.
func (c *Configuration) Username(account string) string {
	return accounts.UsernamePrefix + c.InstallationID + ":" + account
}

// Accounts is the allowlist: the Console accounts the agent acts for. It is
// read again when its file changes, without a restart.
type Accounts struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Accounts   []string `json:"accounts"`
}

var (
	label     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	subdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	uuid      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

const labelRule = "must be 1 to 63 lowercase letters, digits and hyphens, starting and ending with a letter or digit"

// ParseConfiguration reads and checks the agent's configuration.
func ParseConfiguration(data []byte) (*Configuration, error) {
	var config Configuration
	if err := yaml.UnmarshalStrict(data, &config); err != nil {
		return nil, fmt.Errorf("not an agent configuration: %w", err)
	}
	var problems []error
	add := func(field, format string, args ...any) {
		problems = append(problems, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...)))
	}
	if config.APIVersion != APIVersion || config.Kind != ConfigurationKind {
		add("kind", "must be %s %s", APIVersion, ConfigurationKind)
	}
	if !label.MatchString(config.InstallationID) {
		add("installationID", labelRule)
	}
	if !uuid.MatchString(config.Connection.UID) {
		add("connection.uid", "is not a lowercase UUID")
	}
	if config.Connection.Generation == 0 {
		add("connection.generation", "must be 1 or more")
	}
	if problem := addressProblem(config.Console.Address); problem != "" {
		add("console.address", "%s", problem)
	}
	if len(config.Console.ServerName) > maxServerName || !subdomain.MatchString(config.Console.ServerName) {
		add("console.serverName", "is not a DNS name")
	}
	problems = append(problems, nameProblems("namespaces", config.Namespaces, maxNamespaces)...)
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return &config, nil
}

// ParseAccounts reads and checks an allowlist.
func ParseAccounts(data []byte) (*Accounts, error) {
	var list Accounts
	if err := yaml.UnmarshalStrict(data, &list); err != nil {
		return nil, fmt.Errorf("not an agent allowlist: %w", err)
	}
	var problems []error
	if list.APIVersion != APIVersion || list.Kind != AccountsKind {
		problems = append(problems, fmt.Errorf("kind: must be %s %s", APIVersion, AccountsKind))
	}
	problems = append(problems, nameProblems("accounts", list.Accounts, maxAccounts)...)
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return &list, nil
}

// nameProblems refuses an empty, overlong or repeating list of DNS labels.
func nameProblems(field string, names []string, most int) []error {
	var problems []error
	switch {
	case len(names) == 0:
		problems = append(problems, fmt.Errorf("%s: names none", field))
	case len(names) > most:
		problems = append(problems, fmt.Errorf("%s: names more than %d", field, most))
	}
	seen := make(map[string]int, len(names))
	for index, name := range names {
		if !label.MatchString(name) {
			problems = append(problems, fmt.Errorf("%s[%d]: %s", field, index, labelRule))
		}
		if first, ok := seen[name]; ok {
			problems = append(problems, fmt.Errorf("%s[%d]: repeats %s[%d]", field, index, field, first))
			continue
		}
		seen[name] = index
	}
	return problems
}

// addressProblem refuses anything but the https root of a host.
func addressProblem(address string) string {
	if len(address) > maxAddressSize {
		return "is too long"
	}
	parsed, err := url.Parse(address)
	switch {
	case err != nil:
		return "is not a URL"
	case parsed.Scheme != "https":
		return "must be https"
	case parsed.Host == "" || parsed.User != nil:
		return "must name a host and nothing else"
	case strings.TrimSuffix(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "":
		return "must have no path, query or fragment"
	}
	return ""
}

// ReadFile reads one of the agent's files, bounded.
func ReadFile(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- the installation names the file, by flag.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, maxFileBytes)
	}
	return data, nil
}
