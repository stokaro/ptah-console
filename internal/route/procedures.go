package route

import (
	"google.golang.org/protobuf/proto"

	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	cv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

// across is how a unary procedure crosses the channel: the request it takes,
// how it rides in an agent request, and how its answer comes out of a result.
func across[Req, Resp any, PReq interface {
	*Req
	proto.Message
}, PResp interface {
	*Resp
	proto.Message
}](wrap func(PReq) *agentv1.Request, unwrap func(*agentv1.Result) PResp, decision bool) procedure {
	return procedure{
		request: func() proto.Message { return PReq(new(Req)) },
		wrap: func(message proto.Message) *agentv1.Request {
			request, _ := message.(PReq)
			return wrap(request)
		},
		unwrap: func(result *agentv1.Result) proto.Message {
			if response := unwrap(result); response != nil {
				return response
			}
			return nil
		},
		decision: decision,
	}
}

// procedures are the console's procedures that act on a cluster, and so
// follow the connection a request names. The session and the console's own
// information are the console's and are not here.
var procedures = map[string]procedure{
	cv1.ScopeServiceListNamespacesProcedure: across(func(r *consolev1.ListNamespacesRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ListNamespaces{ListNamespaces: r}}
	}, (*agentv1.Result).GetListNamespaces, false),
	cv1.SchemaServiceListSchemasProcedure: across(func(r *consolev1.ListSchemasRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ListSchemas{ListSchemas: r}}
	}, (*agentv1.Result).GetListSchemas, false),
	cv1.SchemaServiceGetSchemaProcedure: across(func(r *consolev1.GetSchemaRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_GetSchema{GetSchema: r}}
	}, (*agentv1.Result).GetGetSchema, false),
	cv1.SchemaServiceGetSchemaPlanProcedure: across(func(r *consolev1.GetSchemaPlanRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_GetSchemaPlan{GetSchemaPlan: r}}
	}, (*agentv1.Result).GetGetSchemaPlan, false),
	cv1.MigrationServiceListMigrationsProcedure: across(func(r *consolev1.ListMigrationsRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ListMigrations{ListMigrations: r}}
	}, (*agentv1.Result).GetListMigrations, false),
	cv1.MigrationServiceGetMigrationProcedure: across(func(r *consolev1.GetMigrationRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_GetMigration{GetMigration: r}}
	}, (*agentv1.Result).GetGetMigration, false),
	cv1.MigrationServiceGetMigrationPlanProcedure: across(func(r *consolev1.GetMigrationPlanRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_GetMigrationPlan{GetMigrationPlan: r}}
	}, (*agentv1.Result).GetGetMigrationPlan, false),
	cv1.DecisionServiceListDecisionsProcedure: across(func(r *consolev1.ListDecisionsRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ListDecisions{ListDecisions: r}}
	}, (*agentv1.Result).GetListDecisions, false),
	cv1.DecisionServiceGetDecisionProcedure: across(func(r *consolev1.GetDecisionRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_GetDecision{GetDecision: r}}
	}, (*agentv1.Result).GetGetDecision, false),
	cv1.DecisionServiceApproveSchemaPlanProcedure: across(func(r *consolev1.ApproveSchemaPlanRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ApproveSchemaPlan{ApproveSchemaPlan: r}}
	}, (*agentv1.Result).GetApproveSchemaPlan, true),
	cv1.DecisionServiceApproveMigrationPlanProcedure: across(func(r *consolev1.ApproveMigrationPlanRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ApproveMigrationPlan{ApproveMigrationPlan: r}}
	}, (*agentv1.Result).GetApproveMigrationPlan, true),
	cv1.DecisionServiceAcknowledgeMigrationRunProcedure: across(func(r *consolev1.AcknowledgeMigrationRunRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_AcknowledgeMigrationRun{AcknowledgeMigrationRun: r}}
	}, (*agentv1.Result).GetAcknowledgeMigrationRun, true),
	cv1.DecisionServiceSetSuspendedProcedure: across(func(r *consolev1.SetSuspendedRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_SetSuspended{SetSuspended: r}}
	}, (*agentv1.Result).GetSetSuspended, true),
	cv1.DiagnosticsServiceGetDiagnosticsProcedure: across(func(r *consolev1.GetDiagnosticsRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_GetDiagnostics{GetDiagnostics: r}}
	}, (*agentv1.Result).GetGetDiagnostics, false),
	cv1.DiagnosticsServiceListEventsProcedure: across(func(r *consolev1.ListEventsRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_ListEvents{ListEvents: r}}
	}, (*agentv1.Result).GetListEvents, false),
	cv1.AuthoringServiceCreateSchemaProcedure: across(func(r *consolev1.CreateSchemaRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_CreateSchema{CreateSchema: r}}
	}, (*agentv1.Result).GetCreateSchema, true),
	cv1.AuthoringServiceCreateMigrationProcedure: across(func(r *consolev1.CreateMigrationRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_CreateMigration{CreateMigration: r}}
	}, (*agentv1.Result).GetCreateMigration, true),
	cv1.DecisionServiceRequestReadProcedure: across(func(r *consolev1.RequestReadRequest) *agentv1.Request {
		return &agentv1.Request{Action: &agentv1.Request_RequestRead{RequestRead: r}}
	}, (*agentv1.Result).GetRequestRead, true),
	cv1.WatchServiceSubscribeProcedure: {watch: true},
}
