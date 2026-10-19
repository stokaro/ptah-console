package localstate_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/localstate"
)

var plugin = localstate.TrustedPlugin{
	Kubeconfig: "/home/person/.kube/config",
	Context:    "production",
	Digest:     "sha256:aaaa",
	TrustedAt:  time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
}

func TestSaveAndLoadKeepTheFilePrivate(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	path := filepath.Join(t.TempDir(), "ptah-console", "state.json")
	var state localstate.State
	state.Trust(plugin)

	c.Assert(localstate.Save(path, state), qt.IsNil)
	loaded, err := localstate.Load(path)
	c.Assert(err, qt.IsNil)
	file, err := os.Stat(path)
	c.Assert(err, qt.IsNil)
	dir, err := os.Stat(filepath.Dir(path))
	c.Assert(err, qt.IsNil)
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".state-*"))
	c.Assert(err, qt.IsNil)

	c.Assert(loaded.Trusts(plugin), qt.IsTrue)
	c.Assert(file.Mode().Perm(), qt.Equals, os.FileMode(0o600))
	c.Assert(dir.Mode().Perm(), qt.Equals, os.FileMode(0o700))
	c.Assert(leftovers, qt.HasLen, 0)
}

func TestLoadRefusesWhatItCannotTrust(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		contents string
		mode     os.FileMode
		refusal  string
	}{
		{name: "readable by others", contents: `{"version":1}`, mode: 0o644, refusal: ".*only its owner may read it.*"},
		{name: "writable by the group", contents: `{"version":1}`, mode: 0o620, refusal: ".*only its owner may read it.*"},
		{name: "another schema version", contents: `{"version":2}`, mode: 0o600, refusal: ".*schema version 2.*"},
		{name: "not JSON", contents: `version: 1`, mode: 0o600, refusal: ".*is not a state file.*"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			path := filepath.Join(t.TempDir(), "state.json")
			c.Assert(os.WriteFile(path, []byte(test.contents), test.mode), qt.IsNil)
			c.Assert(os.Chmod(path, test.mode), qt.IsNil)

			_, err := localstate.Load(path)

			c.Assert(err, qt.ErrorMatches, test.refusal)
		})
	}
}

func TestAMissingFileIsAnEmptyState(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	state, err := localstate.Load(filepath.Join(t.TempDir(), "state.json"))

	c.Assert(err, qt.IsNil)
	c.Assert(state.Version, qt.Equals, localstate.Version)
	c.Assert(state.TrustedExecPlugins, qt.HasLen, 0)
}

// Trust is for exactly what was agreed to: another context, another file, or
// a changed command is a new question.
func TestTrustIsForExactlyWhatWasAgreedTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		asked   localstate.TrustedPlugin
		trusted bool
	}{
		{name: "the same plugin", asked: plugin, trusted: true},
		{name: "another context", asked: localstate.TrustedPlugin{Kubeconfig: plugin.Kubeconfig, Context: "staging", Digest: plugin.Digest}},
		{name: "another kubeconfig", asked: localstate.TrustedPlugin{Kubeconfig: "/tmp/other", Context: plugin.Context, Digest: plugin.Digest}},
		{name: "a changed command", asked: localstate.TrustedPlugin{Kubeconfig: plugin.Kubeconfig, Context: plugin.Context, Digest: "sha256:bbbb"}},
	}
	var state localstate.State
	state.Trust(plugin)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			c.Assert(state.Trusts(test.asked), qt.Equals, test.trusted)
		})
	}
}

// Agreeing again to a changed plugin replaces the earlier answer for that
// context instead of keeping both.
func TestTrustReplacesTheEarlierAnswerForAContext(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	var state localstate.State
	state.Trust(plugin)
	changed := plugin
	changed.Digest = "sha256:bbbb"

	state.Trust(changed)

	c.Assert(state.TrustedExecPlugins, qt.HasLen, 1)
	c.Assert(state.Trusts(plugin), qt.IsFalse)
	c.Assert(state.Trusts(changed), qt.IsTrue)
}
