package audit_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/audit"
)

// The line has a fixed set of keys. Nothing a caller passes can add a field
// that would carry SQL, a token or a body.
func TestALineHasExactlyTheFixedFields(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	var out bytes.Buffer

	audit.New(&out).Record(t.Context(), audit.Event{
		Action: "approve-schema-plan", Outcome: audit.Admitted,
		Identity: "alice", Context: "production",
		Namespace: "application", Kind: "PtahSchema", Name: "storefront", UID: "uid-1",
		PlanName: "ptah-plan-1", PlanUID: "uid-2", PlanFingerprint: "sha256:abc",
		RequestID: "r-1", AuditID: "a-1",
	})
	var line map[string]any
	c.Assert(json.Unmarshal(out.Bytes(), &line), qt.IsNil)

	c.Assert(slices.Sorted(maps.Keys(line)), qt.DeepEquals, []string{
		"account", "action", "at", "auditID", "context", "identity", "kind", "level", "msg",
		"name", "namespace", "outcome", "plan", "planFingerprint", "planUID", "reason",
		"requestID", "time", "uid",
	})
	c.Assert(line["msg"], qt.Equals, "audit")
	c.Assert(line["identity"], qt.Equals, "alice")
	c.Assert(line["auditID"], qt.Equals, "a-1")
}
