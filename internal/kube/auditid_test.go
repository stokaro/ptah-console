package kube_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/kube"
)

const reviewAuditID = "4f1c2e60-0000-4000-8000-000000000001"

// apiServer answers discovery and a SelfSubjectReview, and puts an Audit-Id
// on every response, as the API server does.
func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			w.Header().Set("Audit-Id", "discovery-core")
			_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
		case "/apis":
			w.Header().Set("Audit-Id", "discovery-groups")
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"authentication.k8s.io",` +
				`"versions":[{"groupVersion":"authentication.k8s.io/v1","version":"v1"}],` +
				`"preferredVersion":{"groupVersion":"authentication.k8s.io/v1","version":"v1"}}]}`))
		case "/apis/authentication.k8s.io/v1":
			w.Header().Set("Audit-Id", "discovery-authentication")
			_, _ = w.Write([]byte(`{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"authentication.k8s.io/v1",` +
				`"resources":[{"name":"selfsubjectreviews","namespaced":false,"kind":"SelfSubjectReview","verbs":["create"]}]}`))
		default:
			w.Header().Set("Audit-Id", reviewAuditID)
			_, _ = w.Write([]byte(`{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview",` +
				`"status":{"userInfo":{"username":"alice"}}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The context that asked collects the review's Audit-Id; a request made with
// another context adds nothing to it.
func TestTheAPIServersAuditIDIsCollectedForTheAskingContext(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	client, err := kube.NewClient(&rest.Config{Host: apiServer(t).URL})
	c.Assert(err, qt.IsNil)

	ctx, ids := kube.CollectAuditIDs(t.Context())
	_, err = kube.WhoAmI(ctx, client)
	c.Assert(err, qt.IsNil)
	before := ids.IDs()
	_, err = kube.WhoAmI(t.Context(), client)
	c.Assert(err, qt.IsNil)

	c.Assert(ids.Last(), qt.Equals, reviewAuditID)
	c.Assert(ids.IDs(), qt.Contains, reviewAuditID)
	c.Assert(ids.IDs(), qt.DeepEquals, before)
}
