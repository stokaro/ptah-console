// Package enrollment is how an external cluster is enrolled: the file the
// console gives an installation administrator, the fingerprint the console
// and the target administrator's helper both show, and the console's side of
// creating and completing an enrollment.
//
// The file carries the single-use bootstrap secret, so it is handed over as a
// file and never as a URL, an argument or a Helm value. Everything else in it
// is what the fingerprint covers, so a file edited on its way to the target
// no longer matches what the console issued.
package enrollment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
)

// The file's kind.
const (
	APIVersion = "console.ptah.run/v1alpha1"
	Kind       = "EnrollmentFile"
)

// Bounds of an enrollment.
const (
	// BootstrapValidity is how long the bootstrap secret is accepted.
	BootstrapValidity = 10 * time.Minute
	// ActivationWindow is how long a completed enrollment waits for an
	// administrator to activate it.
	ActivationWindow = 24 * time.Hour
	// MaxPending is how many enrollments a console holds at once.
	MaxPending = 16
)

// File is what the helper on the target reads.
type File struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// Console is where the agent will dial, and how it knows the console.
	Console Console `json:"console"`
	// Identity is the connection the enrollment proposes.
	Identity Identity `json:"identity"`
	// Chart is the agent chart release to install.
	Chart Chart `json:"chart"`
	// Bootstrap is the single-use secret that completes the enrollment.
	Bootstrap string `json:"bootstrap"`
	// ExpiresAt is when the bootstrap stops being accepted.
	ExpiresAt time.Time `json:"expiresAt"`
	// Fingerprint is the console's fingerprint of Identity.
	Fingerprint string `json:"fingerprint"`
}

// Console is the console's agent listener and its CA.
type Console struct {
	Address    string `json:"address"`
	ServerName string `json:"serverName"`
	// CABundle is the console's own CA, in PEM.
	CABundle string `json:"caBundle"`
}

// Chart is the agent chart the helper's Helm command installs.
type Chart struct {
	Reference string `json:"reference"`
	Version   string `json:"version"`
}

// Identity is everything that decides whom the agent trusts and what it is
// granted, and nothing secret. The fingerprint covers it.
type Identity struct {
	ClusterName         string    `json:"clusterName"`
	ConnectionUID       string    `json:"connectionUID"`
	Generation          int64     `json:"generation"`
	InstallationID      string    `json:"installationID"`
	Namespaces          []string  `json:"namespaces"`
	Accounts            []Account `json:"accounts"`
	ApproverClusterRole string    `json:"approverClusterRole"`
	ConsoleServerName   string    `json:"consoleServerName"`
	ConsoleCASHA256     string    `json:"consoleCASHA256"`
}

// Account is an account the agent will act for, with its roles.
type Account struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles"`
}

// IdentityOf is the identity an enrollment proposes.
func IdentityOf(spec consolev1alpha1.ConsoleClusterEnrollmentSpec) Identity {
	accounts := make([]Account, 0, len(spec.Accounts))
	for _, account := range spec.Accounts {
		roles := make([]string, 0, len(account.Roles))
		for _, role := range account.Roles {
			roles = append(roles, string(role))
		}
		accounts = append(accounts, Account{ID: account.ID, Roles: roles})
	}
	return Identity{
		ClusterName: spec.ClusterName, ConnectionUID: spec.ConnectionUID, Generation: 1, InstallationID: spec.InstallationID,
		Namespaces: slices.Clone(spec.Namespaces), Accounts: accounts, ApproverClusterRole: spec.ApproverClusterRole,
		ConsoleServerName: spec.ConsoleServerName, ConsoleCASHA256: spec.ConsoleCASHA256,
	}
}

// Fingerprint is the SHA-256 of the identity in a canonical form: the order
// the namespaces, the accounts and their roles are listed in is not part of
// it.
func (i Identity) Fingerprint() string {
	canonical := i
	canonical.Namespaces = slices.Sorted(slices.Values(i.Namespaces))
	canonical.Accounts = make([]Account, 0, len(i.Accounts))
	for _, account := range i.Accounts {
		canonical.Accounts = append(canonical.Accounts, Account{ID: account.ID, Roles: slices.Sorted(slices.Values(account.Roles))})
	}
	slices.SortFunc(canonical.Accounts, func(a, b Account) int { return strings.Compare(a.ID, b.ID) })
	encoded, err := json.Marshal(canonical)
	if err != nil {
		// A struct of strings and integers always encodes.
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CASHA256 is how the console's CA is named in an enrollment and an entry.
func CASHA256(pem string) string {
	sum := sha256.Sum256([]byte(pem))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verifier is what the console keeps of a bootstrap secret.
func Verifier(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

var (
	label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	uuid  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Parse reads an enrollment file and refuses one that is not whole: a
// fingerprint that is not the identity's, a CA that is not the one the
// identity names, or anything missing.
func Parse(data []byte) (*File, error) {
	var file File
	if err := yaml.UnmarshalStrict(data, &file); err != nil {
		return nil, fmt.Errorf("not an enrollment file: %w", err)
	}
	identity := file.Identity
	var problems []error
	add := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	switch {
	case file.APIVersion != APIVersion || file.Kind != Kind:
		add("kind: must be %s %s", APIVersion, Kind)
	case !label.MatchString(identity.ClusterName) || !label.MatchString(identity.InstallationID):
		add("identity: the cluster name and installation ID must be DNS labels")
	case !uuid.MatchString(identity.ConnectionUID) || identity.Generation != 1:
		add("identity: a new connection has a lowercase UUID and generation 1")
	case len(identity.Namespaces) == 0 || len(identity.Accounts) == 0:
		add("identity: an enrollment names namespaces and accounts")
	case file.Bootstrap == "" || file.Console.Address == "" || file.Console.ServerName == "" || file.Console.CABundle == "":
		add("the file lacks the bootstrap secret or the console's address, name or CA")
	case CASHA256(file.Console.CABundle) != identity.ConsoleCASHA256 || file.Console.ServerName != identity.ConsoleServerName:
		add("console: the CA or name is not the one the enrollment names")
	case identity.Fingerprint() != file.Fingerprint:
		add("fingerprint: the file does not match what the console issued; it was changed on its way")
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return &file, nil
}

// Marshal writes the file.
func (f *File) Marshal() ([]byte, error) {
	return yaml.Marshal(f)
}
