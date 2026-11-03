// Package ociref reads the OCI reference a person sets as the source of a
// PtahSchema or a PtahMigration, by the rules `kubectl ptah set-source`
// applies before it reads the cluster.
//
// The operator's rules live in internal packages of stokaro/ptah-operator,
// which another module cannot import: internal/ocireference parses a
// reference the way the controller does, and internal/operate refuses
// anything but a reference pinned by a SHA-256 digest. This package is the
// same code over the same library, oras-go's registry.ParseReference, and its
// test holds it to the operator's own table of accepted and refused values.
// The API server and the operator's admission still decide; this decides
// only what the console is willing to send.
package ociref

import (
	_ "crypto/sha256" // Registers SHA-256 for digest validation, as the operator does.
	// Registers SHA-384 and SHA-512. The operator's binary links them through
	// its TLS stack, so its parser reads a sha512: digest as a digest; linking
	// them here keeps that verdict in any binary this package is in.
	_ "crypto/sha512"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"oras.land/oras-go/v2/registry"
)

const scheme = "oci://"

// Form is the shape a pinned reference takes, as a person writes it.
const Form = "oci://HOST/REPOSITORY@sha256:<64 hex>"

var (
	sha256Digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// tagName is a tag as oras-go validates one where no digest follows.
	tagName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// Pinned is an OCI reference that selects one manifest by its SHA-256
// digest.
type Pinned struct {
	// Reference is the reference as given.
	Reference string
	// Digest is the digest it selects, sha256:<64 lowercase hex>.
	Digest string
}

// NotPinnedError is a value Parse refuses.
type NotPinnedError struct {
	Value string
	// Tag is the tag the value names in place of a digest, when it names
	// one: a value `ptah oci resolve` can turn into a pinned reference.
	Tag     string
	Problem string
}

func (e *NotPinnedError) Error() string {
	return fmt.Sprintf("%q %s", e.Value, e.Problem)
}

// Parse reads an OCI reference that names its artifact by digest alone, as
// oci://HOST/REPOSITORY@sha256:<64 hex>.
//
// It parses the value the way the operator does, which is stricter than the
// CRD's pattern, so a value it accepts is one the operator resolves. A tag is
// refused even beside a digest: the operator's parser drops such a tag
// unread, and a stored tag nothing checks would tell a reader of the spec
// which release runs without anything holding it true.
//
// Beside a digest that parser drops everything from the first ":" to the "@"
// without reading it, so what sits there is named for what it is: a tag, or a
// repository path that is not one, such as another reference pasted after the
// registry. A scheme there is named even where the parser refuses the value,
// since its own message would call it a tag.
func Parse(value string) (Pinned, error) {
	written, digest, hasDigest := strings.Cut(repositoryPath(value), "@")
	repository, err := parse(value)
	if strings.Contains(written, "://") {
		problem := fmt.Sprintf("has %s where its repository path belongs, and a repository path carries no scheme", written)
		if err == nil {
			problem += fmt.Sprintf(": the operator would read repository %s from it and drop the rest unread", repository)
		}
		return Pinned{}, &NotPinnedError{Value: value, Problem: problem}
	}
	if err != nil {
		return Pinned{}, &NotPinnedError{Value: value, Problem: "is not an OCI reference the operator accepts: " + err.Error()}
	}
	_, tag, hasTag := strings.Cut(written, ":")
	switch {
	case !hasDigest && !hasTag:
		return Pinned{}, &NotPinnedError{Value: value, Tag: "latest",
			Problem: "names no digest, and no tag either, which means tag latest"}
	case !hasDigest:
		// The parser accepted it, so it is a tag.
		return Pinned{}, &NotPinnedError{Value: value, Tag: tag, Problem: fmt.Sprintf("names tag %s, not a digest", tag)}
	case hasTag && !tagName.MatchString(tag):
		return Pinned{}, &NotPinnedError{Value: value, Problem: fmt.Sprintf("has %s where its repository path "+
			"belongs, which is not a repository path, with a tag or without: the operator would read repository %s "+
			"from it and drop the rest unread", written, repository)}
	case hasTag:
		return Pinned{}, &NotPinnedError{Value: value, Problem: fmt.Sprintf("names tag %s beside its digest; "+
			"the operator reads the digest alone, so the tag would be stored and never checked", tag)}
	case !sha256Digest.MatchString(digest):
		return Pinned{}, &NotPinnedError{Value: value, Problem: fmt.Sprintf("names digest %s; the operator "+
			"pins an artifact by sha256: and 64 lowercase hexadecimal digits", digest)}
	}
	return Pinned{Reference: value, Digest: digest}, nil
}

// repositoryPath is what follows the registry: the repository and its
// selector. The registry is everything up to the first "/", as the parser
// splits it, so a port there is not a tag.
func repositoryPath(value string) string {
	_, path, _ := strings.Cut(strings.TrimPrefix(value, scheme), "/")
	return path
}

// parse is the operator's own reading of a reference (its
// internal/ocireference.Parse): the scheme, no whitespace, no query, fragment,
// escaped separator or credential, and then oras-go's parser. It returns the
// repository the operator would read.
func parse(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, " \t\r\n") {
		return "", errors.New("OCI reference must not be empty or contain whitespace")
	}
	value, ok := strings.CutPrefix(raw, scheme)
	if !ok {
		return "", errors.New("OCI reference must use the oci:// scheme")
	}
	if err := validateText(value); err != nil {
		return "", err
	}
	parsed, err := registry.ParseReference(value)
	if err != nil {
		return "", fmt.Errorf("parse OCI reference: %w", err)
	}
	return parsed.Repository, nil
}

func validateText(value string) error {
	lower := strings.ToLower(value)
	switch {
	case strings.ContainsAny(value, "?#\\\x00"):
		return errors.New("OCI reference contains unsupported query, fragment, or path data")
	case strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c"):
		return errors.New("OCI reference contains an escaped path separator")
	case hasUserInfo(value):
		return errors.New("OCI reference must not contain embedded credentials")
	default:
		return nil
	}
}

func hasUserInfo(value string) bool {
	firstSlash := strings.IndexByte(value, '/')
	firstAt := strings.IndexByte(value, '@')
	return firstAt >= 0 && (firstSlash < 0 || firstAt < firstSlash)
}
