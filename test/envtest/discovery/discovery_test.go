package discovery_test

import (
	"context"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{OperatorCRDs: true}, &plane))
}

type factory struct{ client *kube.Client }

func (f factory) For(kube.Principal) (*kube.Client, error) { return f.client, nil }

func operatorAPI(t *testing.T) *consolev1.OperatorApi {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User("discoverer")
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(kube.Principal{Mode: kube.ModeLocal, Context: "envtest", Generation: 1})
	c.Assert(err, qt.IsNil)
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterScopeServiceHandler(server, api.NewScopeService(factory{built}, api.Scope{Candidates: []string{"default"}}))
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	response, err := consolev1connect.NewScopeServiceClient(connect.NewClient(connectinprocess.New(server))).
		ListNamespaces(ctx, &consolev1.ListNamespacesRequest{})
	c.Assert(err, qt.IsNil)
	return response.GetOperatorApi()
}

// With the operator's CRDs installed, every resource the console needs is
// served, and an identity with no grant at all may still ask.
func TestTheOperatorAPIIsServed(t *testing.T) {
	c := qt.New(t)

	answer := operatorAPI(t)

	c.Assert(answer.GetServed(), qt.IsTrue)
	c.Assert(answer.GetMissing(), qt.HasLen, 0)
	c.Assert(answer.GetProblem(), qt.Equals, "")
}

// A CRD the console needs is removed: the API is still served, the missing
// resource is named, and the console says why it matters. This test changes
// the control plane, so it runs after the one above and not in parallel.
func TestAMissingResourceIsNamed(t *testing.T) {
	c := qt.New(t)
	// Unstructured: the console's scheme has no reason to know CRDs.
	crd := &unstructured.Unstructured{}
	crd.SetAPIVersion("apiextensions.k8s.io/v1")
	crd.SetKind("CustomResourceDefinition")
	crd.SetName("ptahmigrationrunacknowledgments.operator.ptah.run")
	c.Assert(plane.Admin.Delete(t.Context(), crd), qt.IsNil)
	waitUntilGone(t, crd)
	waitUntilNotServed(t, "ptahmigrationrunacknowledgments")

	answer := operatorAPI(t)

	c.Assert(answer.GetServed(), qt.IsTrue)
	c.Assert(answer.GetMissing(), qt.DeepEquals, []string{"ptahmigrationrunacknowledgments"})
	c.Assert(answer.GetProblem(), qt.Contains, "without every resource the console needs")
}

// settle is how long the control plane gets to act on a deleted CRD. The
// finalizer and discovery run asynchronously inside the API server, and
// envtest starts one per package, all at once on a CI runner, so their pace
// is the runner's: the bound is generous, and which step stalls is named.
const settle = 3 * time.Minute

// waitUntilGone waits for the CRD's own deletion: its finalizer has run.
func waitUntilGone(t *testing.T, crd *unstructured.Unstructured) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), settle)
	defer cancel()
	for ctx.Err() == nil {
		err := plane.Admin.Get(ctx, client.ObjectKeyFromObject(crd), crd.DeepCopy())
		if apierrors.IsNotFound(err) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the CRD %s was still not deleted after %s", crd.GetName(), settle)
}

// waitUntilNotServed waits for discovery to stop listing a deleted resource.
func waitUntilNotServed(t *testing.T, resource string) {
	t.Helper()
	discoverer, err := discovery.NewDiscoveryClientForConfig(plane.Env.Config)
	qt.New(t).Assert(err, qt.IsNil)
	ctx, cancel := context.WithTimeout(t.Context(), settle)
	defer cancel()
	for ctx.Err() == nil {
		list, err := discoverer.ServerResourcesForGroupVersion("operator.ptah.run/v1alpha1")
		if err == nil && !served(list.APIResources, resource) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s was deleted and was still served after %s", resource, settle)
}

func served(resources []metav1.APIResource, name string) bool {
	for _, resource := range resources {
		if resource.Name == name {
			return true
		}
	}
	return false
}
