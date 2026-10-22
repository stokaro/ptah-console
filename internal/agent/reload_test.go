package agent_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/agent"
)

// lockedBuffer is a log the reload loop writes while the test reads it.
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// eventually waits until check holds, and fails the test after five seconds.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A changed allowlist takes effect without a restart; one that does not
// parse leaves the list in force as it was.
func TestTheAllowlistIsReadAgainWhenItChanges(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	dispatcher, factory := newDispatcher(t, &apiServer{})
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	c.Assert(os.WriteFile(path, []byte(allowlist), 0o600), qt.IsNil)
	log := &lockedBuffer{}
	go agent.Reload(t.Context(), path, []byte(allowlist), 10*time.Millisecond, factory, dispatcher, slog.New(slog.NewTextHandler(log, nil)))

	c.Assert(os.WriteFile(path, []byte(strings.Replace(allowlist, "[alex, sam]", "[sam, kim]", 1)), 0o600), qt.IsNil)
	eventually(t, "the allowlist without alex taking effect", func() bool { return !factory.Allowed("alex") })
	c.Assert(factory.Allowed("kim"), qt.IsTrue)

	c.Assert(os.WriteFile(path, []byte(strings.Replace(allowlist, "[alex, sam]", "['system:masters']", 1)), 0o600), qt.IsNil)
	eventually(t, "the refusal of the changed allowlist", func() bool {
		return strings.Contains(log.String(), "the changed allowlist was refused")
	})
	c.Assert(factory.Allowed("kim"), qt.IsTrue, qt.Commentf("a refused allowlist replaced the one in force"))
	c.Assert(factory.Allowed("system:masters"), qt.IsFalse)
}
