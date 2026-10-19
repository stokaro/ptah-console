package buildinfo_test

import (
	"runtime"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/buildinfo"
)

// An unstamped build still names itself: a version and a commit are never
// empty, so nothing downstream prints a blank where a version belongs.
func TestReadNeverReturnsABlankIdentity(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	info := buildinfo.Read()

	c.Assert(info.Version, qt.Not(qt.Equals), "")
	c.Assert(info.Commit, qt.Not(qt.Equals), "")
	c.Assert(info.GoVersion, qt.Equals, runtime.Version())
}
