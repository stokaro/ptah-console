package api

import (
	"connectrpc.com/connect/v2"

	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/decision"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// Register adds the console's services beside InfoService: the same set in
// both modes, so a screen never finds a service one mode lacks.
func Register(rpc *connect.Server, sessions *Session, factory kube.ClientFactory, scope Scope, auditLog *audit.Logger) {
	consolev1connect.RegisterSessionServiceHandler(rpc, sessions)
	consolev1connect.RegisterScopeServiceHandler(rpc, NewScopeService(factory, scope))
	consolev1connect.RegisterSchemaServiceHandler(rpc, NewSchemaService(factory, scope))
	consolev1connect.RegisterMigrationServiceHandler(rpc, NewMigrationService(factory, scope))
	consolev1connect.RegisterDecisionServiceHandler(rpc, NewDecisionService(factory, scope, decision.NewLedger(), auditLog))
	consolev1connect.RegisterWatchServiceHandler(rpc, NewWatchService(factory, scope, WatchOptions{}))
	consolev1connect.RegisterDiagnosticsServiceHandler(rpc, NewDiagnosticsService(factory, scope, nil))
	consolev1connect.RegisterAuthoringServiceHandler(rpc, NewAuthoringService(factory, scope, auditLog))
}
