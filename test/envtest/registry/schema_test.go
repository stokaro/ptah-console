// Package registry_test holds the registry kinds to the API server's verdict:
// what the CRDs admit, and each CEL rule refusing the object it exists to
// refuse, by field and message.
package registry_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{RegistryCRDs: true}, &plane))
}

const (
	thisConnection  = "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f"
	otherConnection = "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d"
	sha             = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

func newNamespace(t *testing.T) string {
	t.Helper()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "registry-"}}
	qt.New(t).Assert(plane.Admin.Create(t.Context(), namespace), qt.IsNil)
	return namespace.Name
}

func coLocated(namespace string) *consolev1alpha1.ConsoleCluster {
	return &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "this-cluster"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID:      thisConnection,
			Profile:            consolev1alpha1.ProfileCoLocated,
			InstallationID:     "prod-eu-1",
			Namespaces:         []string{"application", "billing"},
			ConfigurationOwner: consolev1alpha1.OwnerGitOps,
		},
	}
}

func agent(namespace string) *consolev1alpha1.ConsoleCluster {
	return &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "prod-ap-2"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID:      otherConnection,
			Profile:            consolev1alpha1.ProfileAgent,
			InstallationID:     "prod-eu-1",
			Namespaces:         []string{"application"},
			ConfigurationOwner: consolev1alpha1.OwnerConsole,
			Agent: &consolev1alpha1.AgentTrust{
				TargetServer:      "https://api.prod-ap-2.example.com:6443",
				TargetCASHA256:    sha,
				ConsoleServerName: "agents.console.example.com",
				ConsoleCASHA256:   sha,
			},
		},
	}
}

func preferences(namespace string) *consolev1alpha1.ConsoleClusterPreferences {
	return &consolev1alpha1.ConsoleClusterPreferences{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "this-cluster"},
		Spec: consolev1alpha1.ConsoleClusterPreferencesSpec{
			ClusterRef:  consolev1alpha1.ConnectionReference{Name: "this-cluster", ConnectionUID: thisConnection},
			DisplayName: "Production, Europe",
			Labels: []consolev1alpha1.PreferenceLabel{
				{Key: "region", Value: "eu"}, {Key: "tier", Value: "production"},
			},
		},
	}
}

// cause is one refusal the API server reported: the field it named and part
// of its message.
type cause struct{ field, message string }

// refusedWith reports whether err is an Invalid refusal carrying every cause
// in want and no other, so a row that also tripped a rule it did not ask for
// fails.
func refusedWith(err error, want ...cause) error {
	if err == nil {
		return errors.New("the API server admitted it")
	}
	var status *apierrors.StatusError
	if !errors.As(err, &status) || !apierrors.IsInvalid(err) || status.ErrStatus.Details == nil {
		return fmt.Errorf("not an Invalid refusal: %w", err)
	}
	var got []metav1.StatusCause
	for _, reported := range status.ErrStatus.Details.Causes {
		// A schema refusal is followed by the API server's note that it left
		// the CEL rules unevaluated; it names no rule of its own.
		if !strings.Contains(reported.Message, "some validation rules were not checked") {
			got = append(got, reported)
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("refused with %d causes, want %d: %v", len(got), len(want), got)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g.Field == w.field && strings.Contains(g.Message, w.message) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("no cause on %s saying %q among %v", w.field, w.message, got)
		}
	}
	return nil
}

func TestTheRegistryAdmitsBothProfilesAndPreferences(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := newNamespace(t)
	for _, object := range []client.Object{coLocated(namespace), agent(namespace), preferences(namespace)} {
		c.Assert(plane.Admin.Create(t.Context(), object), qt.IsNil, qt.Commentf("%T", object))
	}
	stored := &consolev1alpha1.ConsoleClusterPreferences{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "this-cluster"}, stored), qt.IsNil)
	c.Assert(stored.Spec.Enabled, qt.IsNotNil)
	c.Assert(*stored.Spec.Enabled, qt.IsTrue, qt.Commentf("unset is enabled"))
}

func TestAConnectionsTrustCannotBeEdited(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := newNamespace(t)
	for name, edit := range map[string]func(*consolev1alpha1.ConsoleCluster){
		"another namespace ceiling": func(cluster *consolev1alpha1.ConsoleCluster) {
			cluster.Spec.Namespaces = append(cluster.Spec.Namespaces, "payments")
		},
		"another connection UID": func(cluster *consolev1alpha1.ConsoleCluster) {
			cluster.Spec.ConnectionUID = "6a1d2c3b-4e5f-4061-8a9b-0c1d2e3f4a5b"
		},
		"another console CA": func(cluster *consolev1alpha1.ConsoleCluster) {
			cluster.Spec.Agent.ConsoleCASHA256 = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		},
		"another owner": func(cluster *consolev1alpha1.ConsoleCluster) {
			cluster.Spec.ConfigurationOwner = consolev1alpha1.OwnerGitOps
		},
	} {
		cluster := agent(namespace)
		cluster.Name = "prod-ap-2-" + strings.ReplaceAll(strings.ToLower(name[len("another "):]), " ", "-")
		c.Assert(plane.Admin.Create(t.Context(), cluster), qt.IsNil)
		edit(cluster)
		c.Assert(refusedWith(plane.Admin.Update(t.Context(), cluster),
			cause{"spec", "a connection's trust and reach are immutable"}), qt.IsNil, qt.Commentf("%s", name))
	}
	// Status is the console's to write, through its own subresource; an
	// update of status leaves the immutable spec as it was.
	cluster := coLocated(namespace)
	c.Assert(plane.Admin.Create(t.Context(), cluster), qt.IsNil)
	cluster.Status.LastObservedAt = &metav1.Time{Time: metav1.Now().Rfc3339Copy().Time}
	c.Assert(plane.Admin.Status().Update(t.Context(), cluster), qt.IsNil)
}

func TestTheRegistrySchemaRefusesWhatItExistsToRefuse(t *testing.T) {
	t.Parallel()
	namespace := newNamespace(t)
	for _, row := range []struct {
		name   string
		object func() client.Object
		want   []cause
	}{
		{"an Agent connection without agent trust", func() client.Object {
			cluster := agent(namespace)
			cluster.Spec.Agent = nil
			return cluster
		}, []cause{{"spec", "an Agent connection names its agent trust, and a CoLocated one names none"}}},
		{"a CoLocated connection with agent trust", func() client.Object {
			cluster := coLocated(namespace)
			cluster.Spec.Agent = agent(namespace).Spec.Agent
			return cluster
		}, []cause{{"spec", "an Agent connection names its agent trust, and a CoLocated one names none"}}},
		{"a connection UID that is not a UUID", func() client.Object {
			cluster := coLocated(namespace)
			cluster.Spec.ConnectionUID = "this-cluster"
			return cluster
		}, []cause{{"spec.connectionUID", "should match"}}},
		{"an unknown profile", func() client.Object {
			cluster := coLocated(namespace)
			cluster.Spec.Profile = "Direct"
			return cluster
		}, []cause{{"spec.profile", "Unsupported value"}}},
		{"no namespace", func() client.Object {
			cluster := coLocated(namespace)
			cluster.Spec.Namespaces = nil
			return cluster
		}, []cause{{"spec.namespaces", "Required value"}}},
		{"a namespace named twice", func() client.Object {
			cluster := coLocated(namespace)
			cluster.Spec.Namespaces = []string{"application", "application"}
			return cluster
		}, []cause{{"spec.namespaces[1]", "Duplicate value"}}},
		{"a target server over plain HTTP", func() client.Object {
			cluster := agent(namespace)
			cluster.Spec.Agent.TargetServer = "http://api.prod-ap-2.example.com:6443"
			return cluster
		}, []cause{{"spec.agent.targetServer", "should match"}}},
		{"a target server with credentials", func() client.Object {
			cluster := agent(namespace)
			cluster.Spec.Agent.TargetServer = "https://admin:secret@api.prod-ap-2.example.com:6443"
			return cluster
		}, []cause{{"spec.agent.targetServer", "should match"}}},
		{"a console CA that is not a digest", func() client.Object {
			cluster := agent(namespace)
			cluster.Spec.Agent.ConsoleCASHA256 = "-----BEGIN CERTIFICATE-----"
			return cluster
		}, []cause{{"spec.agent.consoleCASHA256", "should match"}}},
		{"a display name with a control character", func() client.Object {
			object := preferences(namespace)
			object.Spec.DisplayName = "Production\x1b[31m"
			return object
		}, []cause{{"spec.displayName", "should match"}}},
		{"a label key that is not a DNS label", func() client.Object {
			object := preferences(namespace)
			object.Spec.Labels = []consolev1alpha1.PreferenceLabel{{Key: "Region", Value: "eu"}}
			return object
		}, []cause{{"spec.labels[0].key", "should match"}}},
		{"a label value with a control character", func() client.Object {
			object := preferences(namespace)
			object.Spec.Labels = []consolev1alpha1.PreferenceLabel{{Key: "region", Value: "eu\n"}}
			return object
		}, []cause{{"spec.labels[0].value", "should match"}}},
		{"a label named twice", func() client.Object {
			object := preferences(namespace)
			object.Spec.Labels = []consolev1alpha1.PreferenceLabel{{Key: "region", Value: "eu"}, {Key: "region", Value: "us"}}
			return object
		}, []cause{{"spec.labels[1]", "Duplicate value"}}},
		{"a reference that is not a connection UID", func() client.Object {
			object := preferences(namespace)
			object.Spec.ClusterRef.ConnectionUID = "this-cluster"
			return object
		}, []cause{{"spec.clusterRef.connectionUID", "should match"}}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			err := plane.Admin.Create(context.Background(), row.object())
			c.Assert(refusedWith(err, row.want...), qt.IsNil)
		})
	}
}

// Preferences follow one connection: the reference cannot be pointed at
// another, while what is shown can change.
func TestPreferencesCannotMoveToAnotherConnection(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := newNamespace(t)
	object := preferences(namespace)
	c.Assert(plane.Admin.Create(t.Context(), object), qt.IsNil)
	object.Spec.DisplayName = "Production"
	disabled := false
	object.Spec.Enabled = &disabled
	c.Assert(plane.Admin.Update(t.Context(), object), qt.IsNil)
	object.Spec.ClusterRef.ConnectionUID = otherConnection
	c.Assert(refusedWith(plane.Admin.Update(t.Context(), object), cause{"spec.clusterRef", "clusterRef is immutable"}), qt.IsNil)
}
