package web_test

import (
	"io/fs"
	"regexp"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/web"
)

var reference = regexp.MustCompile(`(?:src|href)="/([^"]*)"`)

// Every file index.html loads is in the embedded tree. A bundler that names
// assets one way and an embed pattern that drops them another would serve a
// page whose script is a 404. The built app loads at least one file, so the
// check cannot pass by reading nothing; the stub page loads none.
func TestIndexLoadsOnlyWhatIsEmbedded(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	assets := web.Assets()

	index, err := fs.ReadFile(assets, "index.html")
	c.Assert(err, qt.IsNil)

	refs := reference.FindAllStringSubmatch(string(index), -1)
	c.Assert(len(refs) > 0, qt.Equals, web.Built, qt.Commentf("index.html loads %d files", len(refs)))
	for _, ref := range refs {
		_, err := fs.Stat(assets, ref[1])
		c.Check(err, qt.IsNil, qt.Commentf("index.html loads /%s", ref[1]))
	}
}
