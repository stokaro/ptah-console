package kube

import (
	"context"
	"net/http"
	"sync"
)

// AuditIDs collects the Audit-Id headers the API server returns for the
// requests made with a context, so an action can name the API server's own
// audit record beside the console's.
type AuditIDs struct {
	mu  sync.Mutex
	ids []string
}

// IDs returns what was collected, in order.
func (a *AuditIDs) IDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.ids...)
}

// Last returns the last ID collected, or the empty string.
func (a *AuditIDs) Last() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.ids) == 0 {
		return ""
	}
	return a.ids[len(a.ids)-1]
}

type auditIDsKey struct{}

// CollectAuditIDs returns a context whose requests record their Audit-Id.
func CollectAuditIDs(ctx context.Context) (context.Context, *AuditIDs) {
	ids := &AuditIDs{}
	return context.WithValue(ctx, auditIDsKey{}, ids), ids
}

// auditIDTransport records the Audit-Id of each response whose request
// context asked for it.
type auditIDTransport struct {
	next http.RoundTripper
}

func (t auditIDTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	if ids, ok := request.Context().Value(auditIDsKey{}).(*AuditIDs); ok {
		if id := response.Header.Get("Audit-Id"); id != "" {
			ids.mu.Lock()
			ids.ids = append(ids.ids, id)
			ids.mu.Unlock()
		}
	}
	return response, nil
}
