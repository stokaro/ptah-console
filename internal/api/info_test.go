package api_test

import (
	"os"
	"regexp"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"

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
			consolev1connect.RegisterInfoServiceHandler(server, api.NewInfo(test.mode, test.readOnly))
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
	consolev1connect.RegisterInfoServiceHandler(server, api.NewInfo(consolev1.Mode_MODE_LOCAL, false))
	client := consolev1connect.NewInfoServiceClient(connect.NewClient(connectinprocess.New(server)))

	info, err := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})

	c.Assert(err, qt.IsNil)
	c.Assert(info.GetOperatorVersion(), qt.Equals, string(required[1]))
}
