package discovery_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
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
	crd := crdNamed("ptahmigrationrunacknowledgments.operator.ptah.run")
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(crd), crd), qt.IsNil)
	c.Assert(listedWithin(t.Context(), crd, settle), qt.IsNil)
	c.Assert(plane.Admin.Delete(t.Context(), crd), qt.IsNil)
	c.Assert(goneWithin(t.Context(), crd, settle), qt.IsNil)
	waitUntilNotServed(t, "ptahmigrationrunacknowledgments")

	answer := operatorAPI(t)

	c.Assert(answer.GetServed(), qt.IsTrue)
	c.Assert(answer.GetMissing(), qt.DeepEquals, []string{"ptahmigrationrunacknowledgments"})
	c.Assert(answer.GetProblem(), qt.Contains, "without every resource the console needs")
}

// A deletion that does not finish says what holds it. Nothing here removes
// either finalizer until the row does, so the report is the one a stalled run
// would print, and removing the finalizer then proves the deletion was only
// held.
func TestAHeldDeletionNamesWhatHoldsIt(t *testing.T) {
	const keep = "hold.ptah.run/keep"
	rows := []struct {
		name string
		// plural names the row's own CRD, so no row reads what another changed.
		plural string
		// objectFinalizers put an object in the way of the API server's cleanup.
		objectFinalizers []string
		// crdFinalizers hold the CRD itself once that cleanup has finished.
		crdFinalizers []string
		want          []string
	}{
		{
			name:             "an object's finalizer holds the cleanup",
			plural:           "heldbyobjects",
			objectFinalizers: []string{keep},
			want: []string{
				"finalizers [customresourcecleanup.apiextensions.k8s.io]",
				"Terminating=True",
				"remaining objects: 1 [default/" + heldObject + " finalizers [" + keep + "]]",
			},
		},
		{
			name:          "a finalizer on the CRD outlives the cleanup",
			plural:        "heldbycrds",
			crdFinalizers: []string{keep},
			want: []string{
				"finalizers [" + keep + "]",
				"Terminating=False InstanceDeletionCompleted",
				"remaining objects: 0 []",
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			c := qt.New(t)
			crd := holdCRD(row.plural)
			crd.SetFinalizers(row.crdFinalizers)
			c.Assert(plane.Admin.Create(t.Context(), crd), qt.IsNil)
			c.Assert(listedWithin(t.Context(), crd, settle), qt.IsNil)
			objects, err := objectsOf(crd)
			c.Assert(err, qt.IsNil)
			held := objects.Namespace("default")
			c.Assert(holdObject(t.Context(), held, row.objectFinalizers), qt.IsNil)
			c.Assert(plane.Admin.Delete(t.Context(), crd), qt.IsNil)

			report := reportOnceReached(t.Context(), crd, row.want)
			t.Log(report)
			for _, want := range row.want {
				c.Assert(report, qt.Contains, want)
			}

			c.Assert(release(t.Context(), crd, held, row.objectFinalizers), qt.IsNil)
			c.Assert(goneWithin(t.Context(), crd, settle), qt.IsNil)
		})
	}
}

// holdObject creates the object that holds a row's cleanup, if the row has one.
func holdObject(ctx context.Context, objects dynamic.ResourceInterface, finalizers []string) error {
	if len(finalizers) == 0 {
		return nil
	}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "hold.ptah.run/v1",
		"kind":       "Hold",
		"metadata":   map[string]any{"name": heldObject, "namespace": "default"},
	}}
	object.SetFinalizers(finalizers)
	_, err := objects.Create(ctx, object, metav1.CreateOptions{})
	return err
}

// reportOnceReached returns what an expired wait reports, once it says all that
// is wanted or settle has passed. Each wait is short because the deletion cannot
// finish before the row lets it, and the cleanup still has to reach the state
// the report is held to. A deletion that did finish reports nothing.
func reportOnceReached(ctx context.Context, crd *unstructured.Unstructured, want []string) string {
	deadline := time.Now().Add(settle)
	for {
		report := goneWithin(ctx, crd, time.Second)
		if report == nil {
			return ""
		}
		if containsAll(report.Error(), want) || time.Now().After(deadline) {
			return report.Error()
		}
	}
}

// release clears the finalizers holding a row's deletion: the object's when it
// has them, and otherwise the CRD's.
func release(ctx context.Context, crd *unstructured.Unstructured, objects dynamic.ResourceInterface, objectFinalizers []string) error {
	patch := []byte(`{"metadata":{"finalizers":null}}`)
	if len(objectFinalizers) > 0 {
		_, err := objects.Patch(ctx, heldObject, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	}
	return plane.Admin.Patch(ctx, crd.DeepCopy(), client.RawPatch(types.MergePatchType, patch))
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

// heldObject names the object that holds a row's cleanup.
const heldObject = "held"

// holdCRD is a CRD of the row's own, with one namespaced kind.
func holdCRD(plural string) *unstructured.Unstructured {
	crd := crdNamed(plural + ".hold.ptah.run")
	crd.Object["spec"] = map[string]any{
		"group": "hold.ptah.run",
		"names": map[string]any{"plural": plural, "singular": strings.TrimSuffix(plural, "s"), "kind": "Hold", "listKind": "HoldList"},
		"scope": "Namespaced",
		"versions": []any{map[string]any{
			"name": "v1", "served": true, "storage": true,
			"schema": map[string]any{"openAPIV3Schema": map[string]any{
				"type": "object", "x-kubernetes-preserve-unknown-fields": true,
			}},
		}},
	}
	return crd
}

// settle is how long the control plane gets to act on a CRD. It bounds a stall
// and does not measure the runner's pace: a deletion takes seconds at most once
// the resource has been listed, and one the API server loses (see listedWithin)
// finishes only at a five-minute informer resync. Raising it past that would
// hide a stall without removing it.
const settle = 3 * time.Minute

// Unstructured: the console's scheme has no reason to know CRDs.
func crdNamed(name string) *unstructured.Unstructured {
	crd := &unstructured.Unstructured{}
	crd.SetAPIVersion("apiextensions.k8s.io/v1")
	crd.SetKind("CustomResourceDefinition")
	crd.SetName(name)
	return crd
}

// objectsOf is a client for the objects a CRD serves, at its storage version.
// A dynamic client takes the resource as given, so it needs no discovery of a
// CRD that may be on its way out.
func objectsOf(crd *unstructured.Unstructured) (dynamic.NamespaceableResourceInterface, error) {
	group, _, _ := unstructured.NestedString(crd.Object, "spec", "group")
	plural, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "plural")
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	for _, entry := range versions {
		version, _ := entry.(map[string]any)
		if stored, _ := version["storage"].(bool); !stored {
			continue
		}
		name, _ := version["name"].(string)
		resources, err := dynamic.NewForConfig(plane.Env.Config)
		if err != nil {
			return nil, fmt.Errorf("a client for %s: %w", crd.GetName(), err)
		}
		return resources.Resource(schema.GroupVersionResource{Group: group, Version: name, Resource: plural}), nil
	}
	return nil, fmt.Errorf("the CRD %s has no storage version", crd.GetName())
}

// listedWithin waits until the API server answers a list of a CRD's objects, so
// that a delete is not the first thing to use the resource.
//
// The API server builds a resource's storage on first use and refuses a list
// until its watch cache has read etcd. A CRD deleted before then sends its
// cleanup finalizer into that refusal, which it records in the CRD's status
// before retrying a few milliseconds later. A retry that still reads the CRD
// from before that record gets a conflict and returns, expecting another event,
// and the event it gets differs only in the condition it just wrote, which it
// ignores as its own. Nothing then runs until the informer's resync, five
// minutes after the control plane started
// (k8s.io/apiextensions-apiserver/pkg/controller/finalizer/crd_finalizer.go).
//
// The list has no limit on purpose: the API server answers a limited list from
// etcd while the cache is still being built, which says nothing about it.
func listedWithin(ctx context.Context, crd *unstructured.Unstructured, bound time.Duration) error {
	objects, err := objectsOf(crd)
	if err != nil {
		return err
	}
	waiting, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	for {
		_, err := objects.List(waiting, metav1.ListOptions{})
		if err == nil {
			return nil
		}
		if waiting.Err() != nil {
			return fmt.Errorf("the objects of %s could not be listed within %s: %w", crd.GetName(), bound, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// goneWithin waits for the CRD's own deletion: its finalizers have run. When
// the bound passes, the error says what the API server holds it on.
func goneWithin(ctx context.Context, crd *unstructured.Unstructured, bound time.Duration) error {
	waiting, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	var failed error
	for waiting.Err() == nil {
		err := plane.Admin.Get(waiting, client.ObjectKeyFromObject(crd), crd.DeepCopy())
		if apierrors.IsNotFound(err) {
			return nil
		}
		if waiting.Err() == nil {
			failed = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("the CRD %s was still not deleted after %s: %s",
		crd.GetName(), bound, deletionAccount(ctx, crd.GetName(), failed))
}

// deletionAccount is what a deletion waits on, as the API server has it: the
// CRD's finalizers and conditions, and the objects still stored. A part that
// cannot be read says so, which is itself the account of a stalled API server.
func deletionAccount(ctx context.Context, name string, failed error) string {
	reading, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var account []string
	if failed != nil {
		account = append(account, fmt.Sprintf("the last read failed: %v", failed))
	}
	crd := crdNamed(name)
	if err := plane.Admin.Get(reading, client.ObjectKeyFromObject(crd), crd); err != nil {
		return strings.Join(append(account, fmt.Sprintf("reading it now failed: %v", err)), "; ")
	}
	account = append(account,
		fmt.Sprintf("deletion requested at %v", crd.GetDeletionTimestamp()),
		fmt.Sprintf("finalizers %v", crd.GetFinalizers()),
		"conditions "+conditionsOf(crd),
		remainingObjects(reading, crd),
	)
	return strings.Join(account, "; ")
}

func conditionsOf(crd *unstructured.Unstructured) string {
	conditions, _, _ := unstructured.NestedSlice(crd.Object, "status", "conditions")
	described := make([]string, 0, len(conditions))
	for _, entry := range conditions {
		condition, _ := entry.(map[string]any)
		described = append(described, fmt.Sprintf("%v=%v %v at %v: %v", condition["type"], condition["status"],
			condition["reason"], condition["lastTransitionTime"], condition["message"]))
	}
	return "[" + strings.Join(described, ", ") + "]"
}

// remainingObjects counts what the API server's own finalizer has yet to
// delete, and names the first few with their finalizers: the usual holder.
func remainingObjects(ctx context.Context, crd *unstructured.Unstructured) string {
	objects, err := objectsOf(crd)
	if err != nil {
		return fmt.Sprintf("remaining objects unknown: %v", err)
	}
	list, err := objects.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("remaining objects unknown: %v", err)
	}
	named := make([]string, 0, 3)
	for _, object := range list.Items[:min(len(list.Items), cap(named))] {
		named = append(named, fmt.Sprintf("%s/%s finalizers %v", object.GetNamespace(), object.GetName(), object.GetFinalizers()))
	}
	return fmt.Sprintf("remaining objects: %d %v", len(list.Items), named)
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
