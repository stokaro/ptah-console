package api_test

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	"github.com/stokaro/ptah-operator/pkg/planread"

	"github.com/stokaro/ptah-console/internal/api"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

func TestGetInfoReportsTheModeItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mode     consolev1.Mode
		readOnly bool
	}{
		{name: "local", mode: consolev1.Mode_MODE_LOCAL, readOnly: false},
		{name: "local read-only", mode: consolev1.Mode_MODE_LOCAL, readOnly: true},
		{name: "hosted", mode: consolev1.Mode_MODE_HOSTED, readOnly: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			server := connect.NewServer()
			consolev1connect.RegisterInfoServiceHandler(server, api.NewInfo(test.mode, test.readOnly, api.InfoFacts{}))
			client := consolev1connect.NewInfoServiceClient(connect.NewClient(connectinprocess.New(server)))

			info, err := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})

			c.Assert(err, qt.IsNil)
			c.Assert(info.GetMode(), qt.Equals, test.mode)
			c.Assert(info.GetReadOnly(), qt.Equals, test.readOnly)
			c.Assert(info.GetVersion(), qt.Not(qt.Equals), "")
			c.Assert(info.GetCommit(), qt.Not(qt.Equals), "")
		})
	}
}

// The operator version is the one go.mod requires, as the build recorded it,
// so a page that names it cannot drift from what the console links.
func TestGetInfoNamesTheOperatorItWasBuiltAgainst(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	goMod, err := os.ReadFile("../../go.mod")
	c.Assert(err, qt.IsNil)
	required := regexp.MustCompile(`(?m)^\s*github\.com/stokaro/ptah-operator (v\S+)`).FindSubmatch(goMod)
	c.Assert(required, qt.HasLen, 2)
	server := connect.NewServer()
	consolev1connect.RegisterInfoServiceHandler(server, api.NewInfo(consolev1.Mode_MODE_LOCAL, false, api.InfoFacts{}))
	client := consolev1connect.NewInfoServiceClient(connect.NewClient(connectinprocess.New(server)))

	info, err := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})

	c.Assert(err, qt.IsNil)
	c.Assert(info.GetOperatorVersion(), qt.Equals, string(required[1]))
}

// The Kubernetes minors are the support record's row for this tree, and the
// plan contract is the plan reader's, so the pre-flight page states what the
// build was tested against rather than a list kept beside it.
func TestGetInfoSaysWhatTheReleaseWasTestedAgainst(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	record, err := os.ReadFile("../../support/compatibility.json")
	c.Assert(err, qt.IsNil)
	var parsed struct {
		Releases []struct {
			Kubernetes []string `json:"kubernetes"`
		} `json:"releases"`
	}
	c.Assert(json.Unmarshal(record, &parsed), qt.IsNil)
	c.Assert(parsed.Releases, qt.Not(qt.HasLen), 0)
	c.Assert(parsed.Releases[0].Kubernetes, qt.Not(qt.HasLen), 0)
	server := connect.NewServer()
	consolev1connect.RegisterInfoServiceHandler(server, api.NewInfo(consolev1.Mode_MODE_LOCAL, false, api.InfoFacts{}))
	client := consolev1connect.NewInfoServiceClient(connect.NewClient(connectinprocess.New(server)))

	info, err := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})

	c.Assert(err, qt.IsNil)
	c.Assert(info.GetKubernetesMinors(), qt.DeepEquals, parsed.Releases[0].Kubernetes)
	// Contract 1 is a plan from an OCI artifact, 2 one from a ConfigMap or
	// a Git commit; the operator module this console requires reads both.
	c.Assert(info.GetPlanContractVersions(), qt.DeepEquals, []int32{1, 2})
	c.Assert(info.GetPlanContractVersion(), qt.Equals, int32(2))
	for version := int32(3); version <= 32; version++ {
		c.Assert(planread.SupportedContract(version), qt.IsFalse, qt.Commentf("contract %d", version))
	}
}
