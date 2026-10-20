// Package accounts reads who may sign in to a hosted console: the accounts,
// the Kubernetes username each one acts as, and the bcrypt hash each one signs
// in with.
//
// The configuration is a YAML file the installer writes, usually from a
// ConfigMap, and the hashes are an htpasswd file, usually from a Secret. A
// file with anything wrong in it is refused whole, and every problem in it is
// reported at once, so an installer fixes it in one pass.
package accounts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"

	"sigs.k8s.io/yaml"
)

// The configuration file's version and kind.
const (
	APIVersion = "console.ptah.run/v1alpha1"
	Kind       = "HostedConfiguration"
)

// UsernamePrefix starts every Kubernetes username an account acts as.
const UsernamePrefix = "ptah-console:"

const (
	maxFileBytes        = 1 << 20
	maxLoginBytes       = 254
	maxDisplayNameBytes = 128
	maxTrustedProxies   = 64
)

// Config is the hosted console's configuration file.
type Config struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// InstallationID tells this console's Kubernetes usernames apart from
	// another installation's in the same cluster.
	InstallationID string `json:"installationID"`
	// PublicURL is the HTTPS address people open the console at.
	PublicURL string `json:"publicURL"`
	// Namespaces is the ceiling: the console shows nothing outside them,
	// whatever an account's RBAC allows.
	Namespaces []string  `json:"namespaces"`
	Accounts   []Account `json:"accounts"`
	// TrustedProxies are the networks of the proxies in front of the console,
	// such as the ingress controller's Pods. Only a request whose connection
	// comes from one of them has its X-Forwarded-For read, to count sign-in
	// attempts per client rather than per proxy. Empty, the header is never
	// read.
	TrustedProxies []string `json:"trustedProxies,omitempty"`
}

// Proxies is TrustedProxies, parsed. A configuration ParseConfig accepted
// parses whole.
func (c *Config) Proxies() []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(c.TrustedProxies))
	for _, text := range c.TrustedProxies {
		if prefix, err := netip.ParsePrefix(text); err == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}

// Account is one person who may sign in.
type Account struct {
	// ID is part of the account's Kubernetes username, so RBAC bindings name
	// it. It never changes; a person who needs a new one gets a new account.
	ID string `json:"id"`
	// Login is what the person types to sign in. It may change.
	Login       string `json:"login"`
	DisplayName string `json:"displayName,omitempty"`
}

// Username is the Kubernetes username the account acts as. It is built here
// from the configuration and nothing else.
func (c *Config) Username(account Account) string {
	return UsernamePrefix + c.InstallationID + ":" + account.ID
}

// InvalidError lists every problem found in one file.
type InvalidError struct {
	Problems []string
}

func (e *InvalidError) Error() string {
	return "refused:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// ParseConfig reads and checks a configuration file.
func ParseConfig(data []byte) (*Config, error) {
	var config Config
	if err := yaml.UnmarshalStrict(data, &config); err != nil {
		return nil, &InvalidError{Problems: []string{"not a configuration: " + err.Error()}}
	}
	if problems := config.problems(); len(problems) > 0 {
		return nil, &InvalidError{Problems: problems}
	}
	return &config, nil
}

// Load reads and checks a configuration file and the passwords for it.
func Load(configPath, passwordsPath string) (*Config, *Passwords, error) {
	data, err := readBounded(configPath)
	if err != nil {
		return nil, nil, err
	}
	config, err := ParseConfig(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", configPath, err)
	}
	data, err = readBounded(passwordsPath)
	if err != nil {
		return nil, nil, err
	}
	passwords, err := ParsePasswords(data, config)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", passwordsPath, err)
	}
	return config, passwords, nil
}

func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- the installer names the file, by flag.
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

// label is what an installation ID, an account ID and a namespace look like:
// a DNS label, which also keeps ':' out of a Kubernetes username's parts.
var label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

const labelRule = "must be 1 to 63 lowercase letters, digits and hyphens, starting and ending with a letter or digit"

func (c *Config) problems() []string {
	var problems []string
	add := func(field, format string, args ...any) {
		problems = append(problems, field+": "+fmt.Sprintf(format, args...))
	}
	if c.APIVersion != APIVersion {
		add("apiVersion", "must be %s", APIVersion)
	}
	if c.Kind != Kind {
		add("kind", "must be %s", Kind)
	}
	if !label.MatchString(c.InstallationID) {
		add("installationID", labelRule)
	}
	if problem := publicURLProblem(c.PublicURL); problem != "" {
		add("publicURL", "%s", problem)
	}
	if len(c.Namespaces) == 0 {
		add("namespaces", "names no namespace; the console shows only the namespaces listed here")
	}
	seenNamespaces := make(map[string]int)
	for i, namespace := range c.Namespaces {
		field := fmt.Sprintf("namespaces[%d]", i)
		if !label.MatchString(namespace) {
			add(field, labelRule)
		}
		if first, seen := seenNamespaces[namespace]; seen {
			add(field, "repeats namespaces[%d]", first)
			continue
		}
		seenNamespaces[namespace] = i
	}
	if len(c.Accounts) == 0 {
		add("accounts", "lists no account")
	}
	problems = append(problems, proxyProblems(c.TrustedProxies)...)
	return append(problems, accountProblems(c.Accounts)...)
}

// proxyProblems refuses a network that cannot be read one way only, and one
// that would let anyone name any client.
func proxyProblems(proxies []string) []string {
	var problems []string
	add := func(field, format string, args ...any) {
		problems = append(problems, field+": "+fmt.Sprintf(format, args...))
	}
	if len(proxies) > maxTrustedProxies {
		add("trustedProxies", "lists more than %d networks", maxTrustedProxies)
	}
	seen := make(map[netip.Prefix]int)
	for i, text := range proxies {
		field := fmt.Sprintf("trustedProxies[%d]", i)
		prefix, err := netip.ParsePrefix(text)
		switch {
		case err != nil:
			add(field, "is not a network in CIDR notation, such as 10.244.0.0/16 or 192.0.2.10/32")
		case prefix.Bits() == 0:
			add(field, "is every address, which would let any client say it is any other")
		case prefix.Addr().Is4In6():
			add(field, "is an IPv4 network written as IPv6; write it as IPv4")
		case prefix != prefix.Masked():
			add(field, "is not a network address; write %s", prefix.Masked())
		default:
			if first, repeated := seen[prefix]; repeated {
				add(field, "repeats trustedProxies[%d]", first)
				continue
			}
			seen[prefix] = i
		}
	}
	return problems
}

func accountProblems(accounts []Account) []string {
	var problems []string
	add := func(field, format string, args ...any) {
		problems = append(problems, field+": "+fmt.Sprintf(format, args...))
	}
	ids, logins := make(map[string]int), make(map[string]int)
	for i, account := range accounts {
		field := fmt.Sprintf("accounts[%d]", i)
		if !label.MatchString(account.ID) {
			add(field+".id", labelRule)
		} else if first, seen := ids[account.ID]; seen {
			add(field+".id", "repeats accounts[%d].id", first)
		} else {
			ids[account.ID] = i
		}
		if problem := loginProblem(account.Login); problem != "" {
			add(field+".login", "%s", problem)
		} else if first, seen := logins[strings.ToLower(account.Login)]; seen {
			add(field+".login", "repeats accounts[%d].login, ignoring case", first)
		} else {
			logins[strings.ToLower(account.Login)] = i
		}
		if problem := displayNameProblem(account.DisplayName); problem != "" {
			add(field+".displayName", "%s", problem)
		}
	}
	return problems
}

func publicURLProblem(text string) string {
	if text == "" {
		return "is required"
	}
	address, err := url.Parse(text)
	switch {
	case err != nil:
		return "is not a URL"
	case address.Scheme != "https":
		return "must be an https URL; a hosted console serves HTTPS only"
	case address.Hostname() == "":
		return "names no host"
	case address.User != nil:
		return "must not carry a user name or password"
	case address.RawQuery != "" || address.ForceQuery || address.Fragment != "":
		return "must not carry a query or a fragment"
	case address.Path != "" && address.Path != "/":
		return "must be the root of its host; the console is served at /"
	}
	return ""
}

func loginProblem(login string) string {
	switch {
	case login == "":
		return "is required"
	case len(login) > maxLoginBytes:
		return fmt.Sprintf("is longer than %d bytes", maxLoginBytes)
	case strings.IndexFunc(login, func(r rune) bool { return r < '!' || r > '~' }) >= 0:
		return "must be ASCII letters, digits and punctuation, without spaces"
	case strings.HasPrefix(strings.ToLower(login), "system:"):
		return "must not start with system:, which Kubernetes keeps for itself"
	}
	return ""
}

func displayNameProblem(name string) string {
	switch {
	case len(name) > maxDisplayNameBytes:
		return fmt.Sprintf("is longer than %d bytes", maxDisplayNameBytes)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "must not hold control characters"
	}
	return ""
}

// errNoAccounts reports a configuration the passwords cannot be checked
// against.
var errNoAccounts = errors.New("accounts: a configuration is required")

// lines splits a file into its lines, without the line ends.
func lines(data []byte) [][]byte {
	data = bytes.TrimSuffix(data, []byte("\n"))
	split := bytes.Split(data, []byte("\n"))
	for i, line := range split {
		split[i] = bytes.TrimSuffix(line, []byte("\r"))
	}
	return split
}
