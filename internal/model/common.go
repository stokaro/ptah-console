// Package model turns what the operator stores into what the console shows:
// the API's messages, the stages of a resource's lifecycle, and the reasons
// it needs a person.
//
// Everything here is a pure function of the objects read. It decides from
// conditions and their reasons, which the operator publishes as its stable
// interface, rather than from the phase a resource passes through; and it
// keeps the two families apart: a schema converges, a migration advances.
package model

import (
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// conditions is a resource's conditions, by type.
type conditions map[string]metav1.Condition

func index(list []metav1.Condition) conditions {
	out := make(conditions, len(list))
	for _, condition := range list {
		out[condition.Type] = condition
	}
	return out
}

func (c conditions) is(kind string, status metav1.ConditionStatus) bool {
	condition, ok := c[kind]
	return ok && condition.Status == status
}

func (c conditions) reason(kind string) string { return c[kind].Reason }

func (c conditions) message(kind string) string { return c[kind].Message }

func (c conditions) since(kind string) *timestamppb.Timestamp {
	condition, ok := c[kind]
	if !ok || condition.LastTransitionTime.IsZero() {
		return nil
	}
	return timestamppb.New(condition.LastTransitionTime.Time)
}

func toConditions(list []metav1.Condition) []*consolev1.Condition {
	out := make([]*consolev1.Condition, 0, len(list))
	for _, condition := range list {
		out = append(out, &consolev1.Condition{
			Type:               condition.Type,
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransition:     timestamp(&condition.LastTransitionTime),
			ObservedGeneration: condition.ObservedGeneration,
		})
	}
	return out
}

func objectRef(meta metav1.ObjectMeta) *consolev1.ObjectRef {
	return &consolev1.ObjectRef{
		Namespace:       meta.Namespace,
		Name:            meta.Name,
		Uid:             string(meta.UID),
		Generation:      meta.Generation,
		ResourceVersion: meta.ResourceVersion,
		CreatedAt:       timestamp(&meta.CreationTimestamp),
	}
}

func timestamp(t *metav1.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(t.Time)
}

// gitOps names the controller that manages an object, from the labels and
// annotations those controllers put on what they apply.
func gitOps(meta metav1.ObjectMeta) *consolev1.GitOps {
	labels, annotations := meta.Labels, meta.Annotations
	if app := labels["argocd.argoproj.io/instance"]; app != "" {
		return &consolev1.GitOps{Controller: "Argo CD", Application: app}
	}
	if tracking := annotations["argocd.argoproj.io/tracking-id"]; tracking != "" {
		app, _, _ := strings.Cut(tracking, ":")
		return &consolev1.GitOps{Controller: "Argo CD", Application: app}
	}
	if name := labels["kustomize.toolkit.fluxcd.io/name"]; name != "" {
		return &consolev1.GitOps{Controller: "Flux", Application: name}
	}
	if name := labels["helm.toolkit.fluxcd.io/name"]; name != "" {
		return &consolev1.GitOps{Controller: "Flux", Application: name}
	}
	return nil
}

// statusCurrent is false when the status was written for an older
// generation of the spec than the object now has.
func statusCurrent(generation, observed int64) bool {
	return observed == generation
}

// newest reports whether a condition was written at the newest generation
// any condition carries. A condition the operator has not rewritten since an
// older generation is history, not the current state.
func (c conditions) newest(kind string) bool {
	condition, ok := c[kind]
	if !ok {
		return false
	}
	for _, other := range c {
		if other.ObservedGeneration > condition.ObservedGeneration {
			return false
		}
	}
	return true
}

func stage(name string, state consolev1.StageState, detail string) *consolev1.Stage {
	return &consolev1.Stage{Name: name, State: state, Detail: detail}
}

// ptahCommand is the operator's read-only kubectl plugin, asked about one
// object. Names come from the API server, which allows only DNS labels and
// subdomains in them, so they need no quoting.
func ptahCommand(verb, name, namespace string) string {
	return "kubectl ptah " + verb + " " + name + " -n " + namespace
}

// specYAML renders a spec for a person to read. A spec holds references, not
// Secret content: a target names its Secret and key. A spec that cannot be
// rendered is left empty, and the page says nothing is shown.
func specYAML(spec any) string {
	data, err := yaml.Marshal(spec)
	if err != nil {
		return ""
	}
	return string(data)
}
