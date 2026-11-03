package ociref_test

import (
	"fmt"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/ociref"
)

// The rows below are the operator's own: TestParsePinnedReference in
// internal/operate/source_test.go of stokaro/ptah-operator at v0.2.9, the
// table `kubectl ptah set-source` is held to. Each refused value is one edit
// away from an accepted one, so a row that passes shows the parser refusing
// that edit and nothing more. web/app/src/model/sourceref.test.ts holds the
// browser's copy of these rules to the same rows.
var (
	digestNew = "sha256:" + strings.Repeat("b", 64)
	pinnedNew = "oci://registry.example/shop/storefront@" + digestNew
)

func TestParseAcceptsTheOperatorsPinnedReferences(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, value string }{
		{name: "a digest", value: pinnedNew},
		{name: "a registry with a port", value: "oci://localhost:5000/shop/storefront@" + digestNew},
		{name: "a nested repository", value: "oci://registry.example/acme/shop/storefront@" + digestNew},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			pinned, err := ociref.Parse(row.value)

			c.Assert(err, qt.IsNil)
			c.Assert(pinned, qt.Equals, ociref.Pinned{Reference: row.value, Digest: digestNew})
		})
	}
}

func TestParseRefusesWhatTheOperatorRefuses(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, value string
		want        string
		tag         string
		// notWant is what the refusal must not call the value.
		notWant string
	}{
		{name: "a tag", value: "oci://registry.example/shop/storefront:1.5.0", want: "names tag 1.5.0, not a digest", tag: "1.5.0"},
		{name: "a tag on a registry with a port", value: "oci://localhost:5000/shop/storefront:1.5.0",
			want: "names tag 1.5.0, not a digest", tag: "1.5.0"},
		{name: "no selector", value: "oci://registry.example/shop/storefront", want: "which means tag latest", tag: "latest"},
		{name: "a tag beside the digest", value: "oci://registry.example/shop/storefront:1.5.0@" + digestNew,
			want: "names tag 1.5.0 beside its digest"},
		// The operator's parser reads repository oci, and drops the rest up
		// to the digest unread.
		{name: "another reference after the registry", value: "oci://registry.example/oci://other.example/shop/storefront@" + digestNew,
			want: "has oci://other.example/shop/storefront where its repository path belongs, and a repository path carries " +
				"no scheme: the operator would read repository oci from it and drop the rest unread", notWant: "tag"},
		{name: "another scheme after the registry", value: "oci://registry.example/https://other.example/shop/storefront@" + digestNew,
			want:    "has https://other.example/shop/storefront where its repository path belongs, and a repository path carries no scheme",
			notWant: "tag"},
		// The parser refuses it, calling what follows the first colon a tag.
		{name: "another reference after the registry, with no digest", value: "oci://registry.example/oci://other.example/shop/storefront:1.5.0",
			want:    "has oci://other.example/shop/storefront:1.5.0 where its repository path belongs, and a repository path carries no scheme",
			notWant: "tag \""},
		{name: "a path with a colon in it before the digest", value: "oci://registry.example/shop:storefront/v1@" + digestNew,
			want: "has shop:storefront/v1 where its repository path belongs, which is not a repository path, with a tag or " +
				"without: the operator would read repository shop from it and drop the rest unread", notWant: "names tag"},
		{name: "an empty tag beside the digest", value: "oci://registry.example/shop/storefront:@" + digestNew,
			want: "has shop/storefront: where its repository path belongs, which is not a repository path", notWant: "names tag"},
		{name: "two tags beside the digest", value: "oci://registry.example/shop/storefront:1.5:0@" + digestNew,
			want: "which is not a repository path", notWant: "names tag"},
		{name: "a tag longer than a tag can be beside the digest", value: "oci://registry.example/shop/storefront:" +
			strings.Repeat("v", 129) + "@" + digestNew, want: "which is not a repository path", notWant: "names tag"},
		{name: "the longest tag beside the digest", value: "oci://registry.example/shop/storefront:" +
			strings.Repeat("v", 128) + "@" + digestNew, want: "names tag " + strings.Repeat("v", 128) + " beside its digest"},
		{name: "another algorithm", value: "oci://registry.example/shop/storefront@sha512:" + strings.Repeat("b", 128),
			want: "names digest sha512:"},
		{name: "upper-case hex", value: "oci://registry.example/shop/storefront@sha256:" + strings.Repeat("B", 64),
			want: "is not an OCI reference the operator accepts"},
		{name: "a short digest", value: "oci://registry.example/shop/storefront@sha256:" + strings.Repeat("b", 63),
			want: "is not an OCI reference the operator accepts"},
		{name: "no scheme", value: "registry.example/shop/storefront@" + digestNew, want: "oci:// scheme"},
		{name: "a credential", value: "oci://user:secret@registry.example/shop/storefront@" + digestNew, want: "embedded credentials"},
		{name: "a query", value: pinnedNew + "?x=1", want: "is not an OCI reference the operator accepts"},
		{name: "space around it", value: " " + pinnedNew, want: "whitespace"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			pinned, err := ociref.Parse(row.value)

			var notPinned *ociref.NotPinnedError
			c.Assert(err, qt.ErrorAs, &notPinned, qt.Commentf("Parse(%q) = %+v", row.value, pinned))
			c.Assert(err.Error(), qt.Contains, row.want)
			c.Assert(notPinned.Tag, qt.Equals, row.tag)
			c.Assert(notPinned.Value, qt.Equals, row.value)
			c.Assert(row.notWant != "" && strings.Contains(err.Error(), row.notWant), qt.IsFalse,
				qt.Commentf("the refusal %q calls the value %q", err, row.notWant))
			c.Assert(pinned, qt.Equals, ociref.Pinned{})
		})
	}
}

// The rows above are the operator's. These are the console's own, on what
// oras-go reads as a registry and a repository, so the browser's copy of the
// parser is held to this one where the operator's table says nothing.
// web/app/src/model/sourceref.test.ts carries the same rows.
func TestParseReadsTheRegistryAndRepositoryAsOrasGoDoes(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, value string
		accepted    bool
		want        string
	}{
		{name: "an IPv6 registry with a port", value: "oci://[::1]:5000/shop/storefront@" + digestNew, accepted: true},
		{name: "a registry of host names joined by colons", value: "oci://a:b:5000/shop/storefront@" + digestNew, accepted: true},
		{name: "an IPv4 address in brackets", value: "oci://[127.0.0.1]/shop/storefront@" + digestNew,
			want: "is not an OCI reference the operator accepts"},
		{name: "a port that is not a number", value: "oci://registry.example:port/shop/storefront@" + digestNew,
			want: "is not an OCI reference the operator accepts"},
		{name: "an escape in the registry", value: "oci://registry%41.example/shop/storefront@" + digestNew,
			want: "is not an OCI reference the operator accepts"},
		{name: "no registry", value: "oci:///shop/storefront@" + digestNew, want: "is not an OCI reference the operator accepts"},
		{name: "an upper-case repository", value: "oci://registry.example/Shop/storefront@" + digestNew,
			want: "is not an OCI reference the operator accepts"},
		{name: "a repository that ends in a separator", value: "oci://registry.example/shop/@" + digestNew,
			want: "is not an OCI reference the operator accepts"},
		{name: "another SHA-2 digest", value: "oci://registry.example/shop/storefront@sha384:" + strings.Repeat("b", 96),
			want: "names digest sha384:"},
		{name: "an algorithm go-digest does not know", value: "oci://registry.example/shop/storefront@md5:" + strings.Repeat("b", 32),
			want: "is not an OCI reference the operator accepts"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			pinned, err := ociref.Parse(row.value)

			c.Assert(err == nil, qt.Equals, row.accepted, qt.Commentf("Parse(%q) = %+v, %v", row.value, pinned, err))
			c.Assert(fmt.Sprint(err), qt.Contains, row.want)
		})
	}
}
