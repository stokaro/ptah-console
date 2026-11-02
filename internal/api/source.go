package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"connectrpc.com/connect/v2"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/audit"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/ociref"
)

// sourceAttempts bounds how many times a change of source is decided again
// after another write moved the object between the console's read and its
// patch, as kubectl ptah set-source bounds it.
const sourceAttempts = 3

// sourceFamily is what a change of source needs to know about one kind.
type sourceFamily struct {
	kind  string
	noun  string
	field string
	read  func() client.Object
	// source is the spec's source and whether an Apply whose Job may exist
	// is in flight, with that Job's name.
	source func(client.Object) (operatorv1alpha1.SourceSpec, bool, string)
}

var sourceFamilies = map[consolev1.ResourceKind]sourceFamily{
	consolev1.ResourceKind_RESOURCE_KIND_SCHEMA: {
		kind: "PtahSchema", noun: "schema", field: "spec.desired",
		read: func() client.Object { return &operatorv1alpha1.PtahSchema{} },
		source: func(object client.Object) (operatorv1alpha1.SourceSpec, bool, string) {
			schema, _ := object.(*operatorv1alpha1.PtahSchema)
			operation := schema.Status.ActiveOperation
			if operation == nil || operation.Type != operatorv1alpha1.OperationApply {
				return schema.Spec.Desired, false, ""
			}
			return schema.Spec.Desired, operation.DispatchStarted || operation.JobUID != "", operation.JobName
		},
	},
	consolev1.ResourceKind_RESOURCE_KIND_MIGRATION: {
		kind: "PtahMigration", noun: "migration", field: "spec.artifact",
		read: func() client.Object { return &operatorv1alpha1.PtahMigration{} },
		source: func(object client.Object) (operatorv1alpha1.SourceSpec, bool, string) {
			migration, _ := object.(*operatorv1alpha1.PtahMigration)
			operation := migration.Status.ActiveOperation
			if operation == nil || operation.Type != operatorv1alpha1.MigrationOperationApply {
				return migration.Spec.Artifact, false, ""
			}
			return migration.Spec.Artifact, operation.DispatchStarted || operation.JobUID != "", operation.JobName
		},
	},
}

// notOCI says what a spec selects when it is not an OCI artifact, as the
// operator's plugin reads it: a ConfigMap or Git selection before an OCI
// reference. It is empty for an OCI source.
func notOCI(spec operatorv1alpha1.SourceSpec) string {
	switch {
	case spec.ConfigMap != nil:
		return "names a ConfigMap source, not an OCI artifact"
	case spec.Git != nil:
		return "names a Git source, not an OCI artifact"
	case spec.OCIRef != "":
		return ""
	default:
		return "names no OCI artifact"
	}
}

// SetSource points exactly the object the person saw at another OCI
// artifact, and changes nothing but its reference.
//
// The reference is held to the operator's own rules before anything is read.
// The console then reads the object as the person and decides from that read,
// the way kubectl ptah set-source does: the object has to be the one the
// person saw, its spec has to name the reference they saw, its source has to
// be an OCI artifact, and no Apply whose Job may exist may be in flight, since
// a spec change under one leaves an outcome nobody established. The JSON
// patch tests the UID and the resource version of that read and replaces the
// one field, so a write in between makes the API server refuse it rather
// than overwrite what the decision did not see. (The plugin replaces the
// resource version where this tests it; both make the API server refuse the
// patch, with 409 for a replace and 422 for a test.) Such a refusal is decided
// again from a fresh read, sourceAttempts times at most: a status write
// changes nothing the decision rests on, and another writer's reference is
// reported rather than replaced.
func (d *DecisionService) SetSource(ctx context.Context, request *consolev1.SetSourceRequest) (*consolev1.SetSourceResponse, error) {
	if err := d.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	family, ok := sourceFamilies[request.GetKind()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, "name a schema or a migration")
	}
	if err := required([]field{
		{"name", request.GetName()}, {"uid", request.GetUid()},
		{"from_reference", request.GetFromReference()}, {"reference", request.GetReference()},
	}); err != nil {
		return nil, err
	}
	// Everything about the reference is settled before the cluster is
	// reached: a refused one reads nothing and sends nothing.
	pinned, err := ociref.Parse(request.GetReference())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Sprintf("A source is an OCI reference pinned by digest, %s, and %s. Nothing was sent.", ociref.Form, err))
	}
	c, principal, err := actor(ctx, d.factory)
	if err != nil {
		return nil, err
	}
	change := sourceChange{
		family: family, request: request, pinned: pinned,
		key: types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetName()},
	}
	response, auditID, err := change.send(ctx, c)
	if err != nil {
		return nil, err
	}
	// A dry run stores nothing and is not audited, as a checked draft is not.
	if !request.GetDryRun() {
		d.audit.Record(ctx, audit.Event{
			Action: "set-source", Outcome: auditOutcome(response.GetOutcome()),
			AccountID: principal.AccountID, Context: principal.Context,
			Namespace: request.GetNamespace(), Kind: family.kind, Name: request.GetName(), UID: request.GetUid(),
			Reference: pinned.Reference, AuditID: auditID, Reason: response.GetMessage(),
		})
	}
	return response, nil
}

// sourceChange is one change of source on its way.
type sourceChange struct {
	family  sourceFamily
	request *consolev1.SetSourceRequest
	pinned  ociref.Pinned
	key     types.NamespacedName
}

// send decides from a fresh read and patches, and decides again when
// another write moved the object in between.
//
// The API server applies a JSON patch to the object as it is stored, so a
// write between the console's read and its patch fails the test of the
// resource version: the answer is 422 Invalid, the same answer a schema or
// CEL refusal gets, and only the object as it is now tells the two apart. A
// 409 Conflict means the same and is taken the same way.
func (s sourceChange) send(ctx context.Context, c client.Client) (*consolev1.SetSourceResponse, string, error) {
	auditID := ""
	var refused error
	for range sourceAttempts {
		object := s.family.read()
		if err := c.Get(ctx, s.key, object); err != nil {
			switch {
			case apierrors.IsNotFound(err):
				return s.changed(fmt.Sprintf("The %s no longer exists. Nothing was changed.", s.family.noun)), auditID, nil
			case refused != nil:
				return s.unread(err), auditID, nil
			}
			return nil, auditID, readError(err, "the "+s.family.noun)
		}
		if refusal := s.refusal(object); refusal != nil {
			return refusal, auditID, nil
		}
		answer, ids, err := s.patch(ctx, c, object)
		auditID = ids
		switch {
		case err == nil:
			return s.admitted(answer), auditID, nil
		case apierrors.IsNotFound(err):
			return s.changed(fmt.Sprintf("The %s no longer exists. Nothing was changed.", s.family.noun)), auditID, nil
		case apierrors.IsConflict(err):
			// Another write since the read; decide again from a fresh one.
			refused = err
			continue
		case apierrors.IsInvalid(err):
			moved, readErr := s.moved(ctx, c, object.GetResourceVersion())
			switch {
			case moved || apierrors.IsNotFound(readErr):
				refused = err
				continue
			case readErr != nil:
				return s.unread(readErr), auditID, nil
			}
		}
		response := &consolev1.SetSourceResponse{DryRun: s.request.GetDryRun(), Field: s.field()}
		response.Outcome, response.Message = classify(err)
		if response.GetOutcome() == consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN {
			response.Message = unknownChange(err, s.field(), s.request.GetDryRun())
		}
		return response, auditID, nil
	}
	return s.changed(fmt.Sprintf("The %s kept changing while the console sent the change, %d times over. Nothing was "+
		"changed; look at it again.", s.family.noun, sourceAttempts)), auditID, nil
}

// refusal is why the object as read is not one the change may be sent to,
// or nil when it is.
func (s sourceChange) refusal(object client.Object) *consolev1.SetSourceResponse {
	if string(object.GetUID()) != s.request.GetUid() {
		return s.changed(fmt.Sprintf("The %s was deleted and created again since you looked at it. Nothing was changed.", s.family.noun))
	}
	spec, applying, job := s.family.source(object)
	field := s.field()
	if other := notOCI(spec); other != "" {
		return s.changed(fmt.Sprintf("%s of %s %s. Nothing was changed. A change of source "+
			"replaces the reference of an OCI source and nothing else; moving to an artifact also names the "+
			"verification policy it is held to, which is an edit of the resource.", s.family.field, s.request.GetName(), other))
	}
	if spec.OCIRef == s.pinned.Reference {
		// What was asked for holds already: nothing is sent.
		message := fmt.Sprintf("%s already names this reference. Nothing was sent.", field)
		if s.request.GetFromReference() != s.pinned.Reference {
			message = fmt.Sprintf("%s names this reference already: another writer set it after you looked. Nothing was sent.", field)
		}
		return &consolev1.SetSourceResponse{
			Outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, Message: message,
			DryRun: s.request.GetDryRun(), Field: field,
			Generation: object.GetGeneration(), ResourceVersion: object.GetResourceVersion(),
		}
	}
	if spec.OCIRef != s.request.GetFromReference() {
		return s.changed(fmt.Sprintf("%s changed since you looked: it named %s and now names %s. Nothing was changed: "+
			"the reference this would replace is not the one you saw.", field, s.request.GetFromReference(), spec.OCIRef))
	}
	if applying {
		return s.changed(fmt.Sprintf("An Apply is running on %s %s (Job %s). Nothing was changed. A spec change now "+
			"moves the generation the Apply was decided from, and the operator records an outcome nobody "+
			"established without reading its result. Wait for the Apply to end, then change the source.",
			s.family.noun, s.request.GetName(), job))
	}
	return nil
}

// patch sends the change: a test of the UID and of the resource version
// read, and a replace of the one field. The answer is the object as the API
// server stored it, or would store it for a dry run.
func (s sourceChange) patch(ctx context.Context, c client.Client, read client.Object) (*unstructured.Unstructured, string, error) {
	operations, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(read.GetUID())},
		{"op": "test", "path": "/metadata/resourceVersion", "value": read.GetResourceVersion()},
		{"op": "replace", "path": "/" + strings.ReplaceAll(s.family.field, ".", "/") + "/ociRef", "value": s.pinned.Reference},
	})
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, "build the change").WithCause(err)
	}
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(operatorv1alpha1.GroupVersion.WithKind(s.family.kind))
	object.SetNamespace(s.key.Namespace)
	object.SetName(s.key.Name)
	options := []client.PatchOption{client.FieldOwner(fieldOwner)}
	if s.request.GetDryRun() {
		options = append(options, client.DryRunAll)
	}
	sendCtx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()
	sendCtx, ids := kube.CollectAuditIDs(sendCtx)
	err = c.Patch(sendCtx, object, client.RawPatch(types.JSONPatchType, operations), options...)
	return object, ids.Last(), err
}

// moved reports whether the object is no longer at the resource version the
// patch tested.
func (s sourceChange) moved(ctx context.Context, c client.Client, resourceVersion string) (bool, error) {
	object := s.family.read()
	if err := c.Get(ctx, s.key, object); err != nil {
		return false, err
	}
	return object.GetResourceVersion() != resourceVersion, nil
}

// admitted is the API server's answer to the change, as it stored it.
func (s sourceChange) admitted(answer *unstructured.Unstructured) *consolev1.SetSourceResponse {
	path := strings.Split(s.family.field, ".")
	observed, _, _ := unstructured.NestedInt64(answer.Object, "status", "observedGeneration")
	suspended, _, _ := unstructured.NestedBool(answer.Object, "spec", "suspend")
	response := &consolev1.SetSourceResponse{
		Outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, DryRun: s.request.GetDryRun(),
		Changed: !s.request.GetDryRun(), Field: s.field(), Previous: s.request.GetFromReference(),
		Generation: answer.GetGeneration(), ObservedGeneration: observed,
		ResourceVersion: answer.GetResourceVersion(), Suspended: suspended,
	}
	if source, found, _ := unstructured.NestedMap(answer.Object, path...); found {
		if out, err := yaml.Marshal(map[string]any{path[1]: source}); err == nil {
			response.SourceYaml = string(out)
		}
	}
	return response
}

// unread is a refused patch whose reason the console could not read back.
// The refusal is the answer to the change, so nothing was stored; only the
// read that would have said why failed.
func (s sourceChange) unread(err error) *consolev1.SetSourceResponse {
	return &consolev1.SetSourceResponse{
		Outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, DryRun: s.request.GetDryRun(), Field: s.field(),
		Message: fmt.Sprintf("The API server refused the change, and reading the %s again to see why failed: %v. "+
			"Nothing was changed.", s.family.noun, err),
	}
}

func (s sourceChange) changed(message string) *consolev1.SetSourceResponse {
	return &consolev1.SetSourceResponse{
		Outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, Message: message,
		DryRun: s.request.GetDryRun(), Field: s.field(),
	}
}

func (s sourceChange) field() string {
	return s.family.field + ".ociRef"
}

// unknownChange says what a change that got no answer may have done. A
// check stores nothing, whatever became of it.
func unknownChange(err error, field string, dryRun bool) string {
	var netErr net.Error
	switch {
	case dryRun:
		return "The API server did not answer the check. Nothing was stored; check again."
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout(), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return fmt.Sprintf("The API server did not answer in time. The change may have been stored; look at %s before changing it again.", field)
	default:
		return fmt.Sprintf("The answer was lost. The change may have been stored; look at %s before changing it again.", field)
	}
}
