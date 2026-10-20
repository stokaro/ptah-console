// Package audit writes one line for every action the console takes for a
// person, to its own stream.
//
// A line names who acted (the Console account and the Kubernetes identity
// the API server saw), what they did to which object, under which plan, and
// how it ended, with the API server's own audit ID when it gave one. The
// fields are fixed: there is no free-form payload, so a line cannot carry SQL,
// a token, a password or a request body, whatever a caller passes.
package audit

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// Outcomes.
const (
	Admitted    = "admitted"
	Refused     = "refused"
	Forbidden   = "forbidden"
	PlanChanged = "plan-changed"
	Unknown     = "unknown"
	Failed      = "failed"
)

// Event is one action.
type Event struct {
	Action  string
	Outcome string

	AccountID string
	// Identity is the Kubernetes username the request ran as.
	Identity string
	Context  string

	Namespace string
	Kind      string
	Name      string
	UID       string

	PlanName        string
	PlanUID         string
	PlanFingerprint string

	// RequestID is the console's own identifier for the action.
	RequestID string
	// AuditID is what the API server returned in its Audit-Id header.
	AuditID string
	// Reason is a short, fixed explanation chosen by the console.
	Reason string
	// Address is where a sign-in came from, as the console resolved it.
	Address string
}

// Logger writes events.
type Logger struct {
	log *slog.Logger
}

// New writes JSON lines to w.
func New(w io.Writer) *Logger {
	return &Logger{log: slog.New(slog.NewJSONHandler(w, nil))}
}

// Discard drops every event, for tests that do not look at the audit.
func Discard() *Logger {
	return &Logger{log: slog.New(slog.DiscardHandler)}
}

// Record writes one event.
func (l *Logger) Record(ctx context.Context, event Event) {
	l.log.LogAttrs(ctx, slog.LevelInfo, "audit",
		slog.String("action", event.Action),
		slog.String("outcome", event.Outcome),
		slog.String("account", event.AccountID),
		slog.String("identity", event.Identity),
		slog.String("context", event.Context),
		slog.String("namespace", event.Namespace),
		slog.String("kind", event.Kind),
		slog.String("name", event.Name),
		slog.String("uid", event.UID),
		slog.String("plan", event.PlanName),
		slog.String("planUID", event.PlanUID),
		slog.String("planFingerprint", event.PlanFingerprint),
		slog.String("requestID", event.RequestID),
		slog.String("auditID", event.AuditID),
		slog.String("reason", event.Reason),
		slog.String("address", event.Address),
		slog.Time("at", time.Now().UTC()),
	)
}
