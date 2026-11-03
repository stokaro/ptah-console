package ociref_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/ociref"
)

var update = flag.Bool("update", false, "rewrite the verdicts the browser's copy of the parser is held to")

// corpusPath is where the browser's copy of the parser reads this parser's
// verdicts: web/app/src/model/sourceref.test.ts compares its own, message for
// message.
const corpusPath = "../../web/app/src/model/testdata/pinned-references.json"

// corpus is every value the tables in ociref_test.go read, and values on the
// edges where Go and a browser read text differently: whitespace each counts
// differently, code points outside the Basic Multilingual Plane, control
// characters a quoted value escapes, and each way go-digest refuses a digest.
func corpus() []string {
	pinned := "oci://registry.example/shop/storefront@" + digestNew
	repeated := func(s string, n int) string { return strings.Repeat(s, n) }
	return []string{
		// The operator's rows.
		pinned,
		"oci://localhost:5000/shop/storefront@" + digestNew,
		"oci://registry.example/acme/shop/storefront@" + digestNew,
		"oci://registry.example/shop/storefront:1.5.0",
		"oci://localhost:5000/shop/storefront:1.5.0",
		"oci://registry.example/shop/storefront",
		"oci://registry.example/shop/storefront:1.5.0@" + digestNew,
		"oci://registry.example/oci://other.example/shop/storefront@" + digestNew,
		"oci://registry.example/https://other.example/shop/storefront@" + digestNew,
		"oci://registry.example/oci://other.example/shop/storefront:1.5.0",
		"oci://registry.example/shop:storefront/v1@" + digestNew,
		"oci://registry.example/shop/storefront:@" + digestNew,
		"oci://registry.example/shop/storefront:1.5:0@" + digestNew,
		"oci://registry.example/shop/storefront:" + repeated("v", 129) + "@" + digestNew,
		"oci://registry.example/shop/storefront:" + repeated("v", 128) + "@" + digestNew,
		"oci://registry.example/shop/storefront@sha512:" + repeated("b", 128),
		"oci://registry.example/shop/storefront@sha256:" + repeated("B", 64),
		"oci://registry.example/shop/storefront@sha256:" + repeated("b", 63),
		"registry.example/shop/storefront@" + digestNew,
		"oci://user:secret@registry.example/shop/storefront@" + digestNew,
		pinned + "?x=1",
		" " + pinned,
		// The console's rows on registries and repositories.
		"oci://[::1]:5000/shop/storefront@" + digestNew,
		"oci://a:b:5000/shop/storefront@" + digestNew,
		"oci://[127.0.0.1]/shop/storefront@" + digestNew,
		"oci://registry.example:port/shop/storefront@" + digestNew,
		"oci://registry%41.example/shop/storefront@" + digestNew,
		"oci:///shop/storefront@" + digestNew,
		"oci://registry.example/Shop/storefront@" + digestNew,
		"oci://registry.example/shop/@" + digestNew,
		"oci://registry.example/shop/storefront@sha384:" + repeated("b", 96),
		"oci://registry.example/shop/storefront@md5:" + repeated("b", 32),
		// Registries.
		"oci://[::ffff:1.2.3.4]:443/shop/storefront@" + digestNew,
		"oci://[fe80::1%25en0]/shop/storefront@" + digestNew,
		"oci://[::1/shop/storefront@" + digestNew,
		"oci://x[::1]/shop/storefront@" + digestNew,
		"oci://localhost:/shop/storefront@" + digestNew,
		"oci://a:b:c/shop/storefront@" + digestNew,
		"oci://reg_istry.example/shop/storefront@" + digestNew,
		"oci://registré.example/shop/storefront@" + digestNew,
		"oci://reg😀.example/shop/storefront@" + digestNew,
		"oci://reg{istry}.example/shop/storefront@" + digestNew,
		"oci://registry.example",
		"oci://registry.example/",
		// Repositories and tags.
		"oci://registry.example/shop__x/storefront@" + digestNew,
		"oci://registry.example/shop---x/storefront@" + digestNew,
		"oci://registry.example/shop..x@" + digestNew,
		"oci://registry.example/shop/storefront:v1.0_beta-2",
		"oci://registry.example/shop/storefront:-v1",
		"oci://registry.example/shop/storefront%2Fx@" + digestNew,
		"oci://registry.example/shop/storefront#x",
		"oci://registry.example/shop/store\x7ffront@" + digestNew,
		"OCI://registry.example/shop/storefront@" + digestNew,
		// Digests.
		"oci://registry.example/shop/storefront@sha256:" + repeated("b", 65),
		"oci://registry.example/shop/storefront@sha256:" + repeated("g", 64),
		"oci://registry.example/shop/storefront@sha256:" + repeated("é", 32),
		"oci://registry.example/shop/storefront@sha384:" + repeated("b", 95),
		"oci://registry.example/shop/storefront@SHA256:" + repeated("b", 64),
		"oci://registry.example/shop/storefront@:" + repeated("b", 64),
		"oci://registry.example/shop/storefront@sha256:",
		// Whitespace and control characters.
		"",
		pinned + "\u00a0",
		"\u0085" + pinned,
		"\ufeff" + pinned,
		"\v" + pinned,
		"oci://registry example/shop/storefront@" + digestNew,
		"oci://registry.example/shop/storefront@" + digestNew + "\t",
	}
}

// verdict is what the parser makes of one value.
type verdict struct {
	Value   string `json:"value"`
	OK      bool   `json:"ok"`
	Digest  string `json:"digest,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Message string `json:"message,omitempty"`
}

func verdictOf(value string) verdict {
	pinned, err := ociref.Parse(value)
	if refused, ok := errors.AsType[*ociref.NotPinnedError](err); ok {
		return verdict{Value: value, Tag: refused.Tag, Message: err.Error()}
	}
	return verdict{Value: value, OK: true, Digest: pinned.Digest}
}

// golden is the corpus file's content, after writing rendered into it when
// the test runs with -update.
func golden(t *testing.T, rendered []byte) string {
	t.Helper()
	c := qt.New(t)
	if *update {
		c.Assert(os.WriteFile(corpusPath, rendered, 0o600), qt.IsNil)
	}
	want, err := os.ReadFile(corpusPath)
	c.Assert(err, qt.IsNil)
	return string(want)
}

// The browser's copy of the parser is held to this one's verdicts on the
// corpus, so the file it reads has to be what this parser says today.
func TestTheBrowsersCorpusIsThisParsersVerdicts(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	values := corpus()
	verdicts := make([]verdict, 0, len(values))
	for _, value := range values {
		verdicts = append(verdicts, verdictOf(value))
	}
	rendered, err := json.MarshalIndent(verdicts, "", "  ")
	c.Assert(err, qt.IsNil)
	rendered = append(rendered, '\n')

	want := golden(t, rendered)

	c.Assert(string(rendered), qt.Equals, want, qt.Commentf("run go test ./internal/ociref -update and review the diff"))
	accepted := 0
	for _, v := range verdicts {
		accepted += map[bool]int{true: 1}[v.OK]
	}
	// The corpus has to hold both verdicts to say anything.
	c.Assert(accepted > 5 && accepted < len(verdicts)-30, qt.IsTrue, qt.Commentf("%d of %d accepted", accepted, len(verdicts)))
}
