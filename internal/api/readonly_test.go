package api_test

import (
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/api"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

// Read-only refuses by what a procedure declares, not by a list of known
// writes: a procedure added later that changes something is refused until it
// is declared free of side effects or allowed by name.
func TestReadOnlyRefusesWhatMayChangeSomething(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	server := connect.NewServer(api.ReadOnly(consolev1connect.SessionServiceLogoutProcedure))
	consolev1connect.RegisterInfoServiceHandler(server, api.NewInfo(consolev1.Mode_MODE_LOCAL, true, api.InfoFacts{}))
	consolev1connect.RegisterSessionServiceHandler(server, &api.Session{})
	transport := connect.NewClient(connectinprocess.New(server))
	info := consolev1connect.NewInfoServiceClient(transport)
	sessions := consolev1connect.NewSessionServiceClient(transport)

	_, read := info.GetInfo(t.Context(), &consolev1.GetInfoRequest{})
	_, write := sessions.ExchangeLaunchToken(t.Context(), &consolev1.ExchangeLaunchTokenRequest{})
	_, allowed := sessions.Logout(t.Context(), &consolev1.LogoutRequest{})

	c.Assert(read, qt.IsNil)
	c.Assert(connect.CodeOf(write), qt.Equals, connect.CodePermissionDenied)
	c.Assert(write, qt.ErrorMatches, ".*--read-only.*")
	c.Assert(connect.CodeOf(allowed), qt.Not(qt.Equals), connect.CodePermissionDenied)
}
