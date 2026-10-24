package api

import (
	"context"
	"fmt"
	"slices"

	"connectrpc.com/connect/v2"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/audit"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// engines are the databases the operator this console is built against
// supports; another one is refused here rather than sent.
var engines = []operatorv1alpha1.DatabaseEngine{
	operatorv1alpha1.DatabaseEnginePostgreSQL,
	operatorv1alpha1.DatabaseEngineMySQL,
}

var applyPolicies = []operatorv1alpha1.ApplyPolicy{
	operatorv1alpha1.ApplyPolicyNever,
	operatorv1alpha1.ApplyPolicyOnApproval,
	operatorv1alpha1.ApplyPolicyAlways,
}

// AuthoringService serves AuthoringService.
type AuthoringService struct {
	consolev1connect.UnimplementedAuthoringServiceHandler

	factory kube.ClientFactory
	scope   Scope
	audit   *audit.Logger
}

// NewAuthoringService returns the AuthoringService.
func NewAuthoringService(factory kube.ClientFactory, scope Scope, auditLog *audit.Logger) *AuthoringService {
	return &AuthoringService{factory: factory, scope: scope, audit: auditLog}
}

// CreateSchema checks a new PtahSchema with the API server and, unless asked
// only to check, creates it as the session's identity.
func (a *AuthoringService) CreateSchema(ctx context.Context, request *consolev1.CreateSchemaRequest) (*consolev1.CreateSchemaResponse, error) {
	object, err := objectFrom("PtahSchema", "desired", request.GetDraft())
	if err != nil {
		return nil, err
	}
	result, err := a.create(ctx, "create-schema", object, sendingFor[request.GetDryRun()])
	if err != nil {
		return nil, err
	}
	return &consolev1.CreateSchemaResponse{Result: result}, nil
}

// CreateMigration checks a new PtahMigration with the API server and, unless
// asked only to check, creates it as the session's identity.
func (a *AuthoringService) CreateMigration(ctx context.Context, request *consolev1.CreateMigrationRequest) (*consolev1.CreateMigrationResponse, error) {
	object, err := objectFrom("PtahMigration", "artifact", request.GetDraft())
	if err != nil {
		return nil, err
	}
	result, err := a.create(ctx, "create-migration", object, sendingFor[request.GetDryRun()])
	if err != nil {
		return nil, err
	}
	return &consolev1.CreateMigrationResponse{Result: result}, nil
}

// sending is whether a create stores the object or only checks it.
type sending int

const (
	storing sending = iota
	checking
)

// sendingFor is how a request's dry_run asks for the object to be sent.
var sendingFor = map[bool]sending{false: storing, true: checking}

// create sends the object, as a dry run or for real, and says how the API
// server answered in its own words.
func (a *AuthoringService) create(ctx context.Context, action string, object *unstructured.Unstructured, how sending) (*consolev1.CreateResult, error) {
	kind, dryRun := object.GetKind(), how == checking
	if err := a.scope.check([]string{object.GetNamespace()}); err != nil {
		return nil, err
	}
	c, principal, err := actor(ctx, a.factory)
	if err != nil {
		return nil, err
	}
	options := []client.CreateOption{client.FieldOwner(fieldOwner)}
	if dryRun {
		options = append(options, client.DryRunAll)
	}
	sendCtx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()
	sendCtx, ids := kube.CollectAuditIDs(sendCtx)
	err = c.Create(sendCtx, object, options...)
	result := &consolev1.CreateResult{
		DryRun: dryRun,
		Ref:    &consolev1.ObjectRef{Namespace: object.GetNamespace(), Name: object.GetName()},
	}
	switch {
	case apierrors.IsAlreadyExists(err):
		result.Outcome = consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED
		result.Message = fmt.Sprintf("%s %s already exists in namespace %s. Nothing was created.",
			kind, object.GetName(), object.GetNamespace())
	default:
		result.Outcome, result.Message = classify(err)
	}
	if err == nil {
		result.Ref.Uid = string(object.GetUID())
		if result.Yaml, err = specYAML(object); err != nil {
			return nil, connect.NewError(connect.CodeInternal, "the API server's answer could not be shown").WithCause(err)
		}
	}
	if !dryRun {
		a.audit.Record(ctx, audit.Event{
			Action: action, Outcome: auditOutcome(result.Outcome),
			AccountID: principal.AccountID, Context: principal.Context,
			Namespace: object.GetNamespace(), Kind: kind,
			Name: object.GetName(), UID: string(object.GetUID()),
			AuditID: ids.Last(), Reason: result.Message,
		})
	}
	return result, nil
}

// specYAML is what a person reads back: the kind, name, namespace and spec,
// with the defaults the API server filled in, and none of the bookkeeping.
func specYAML(object *unstructured.Unstructured) (string, error) {
	document := map[string]any{
		"apiVersion": object.GetAPIVersion(),
		"kind":       object.GetKind(),
		"metadata":   map[string]any{"name": object.GetName(), "namespace": object.GetNamespace()},
		"spec":       object.Object["spec"],
	}
	out, err := yaml.Marshal(document)
	return string(out), err
}

// errDraft is a draft the console cannot build an object from.
func errDraft(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Sprintf(format, args...))
}

// objectFrom builds the object a person would have written: the fields the
// draft sets and no other. A field left out takes the CRD's default, as it
// would in a manifest; a typed object would send every zero value it holds,
// such as an interval of 0s, which the CRD refuses.
func objectFrom(kind, sourceField string, draft *consolev1.ResourceDraft) (*unstructured.Unstructured, error) {
	if draft == nil {
		return nil, errDraft("a draft is required")
	}
	if err := required([]field{{"namespace", draft.GetNamespace()}, {"name", draft.GetName()}}); err != nil {
		return nil, err
	}
	target, err := targetFrom(draft.GetTarget())
	if err != nil {
		return nil, err
	}
	source, err := sourceFrom(draft.GetSource())
	if err != nil {
		return nil, err
	}
	spec := map[string]any{"target": target, sourceField: source}
	if apply := operatorv1alpha1.ApplyPolicy(draft.GetApplyPolicy()); apply != "" {
		if !slices.Contains(applyPolicies, apply) {
			return nil, errDraft("apply policy %q is not Never, OnApproval or Always", apply)
		}
		spec["policy"] = map[string]any{"apply": string(apply)}
	}
	object := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	object.SetGroupVersionKind(operatorv1alpha1.GroupVersion.WithKind(kind))
	object.SetNamespace(draft.GetNamespace())
	object.SetName(draft.GetName())
	return object, nil
}

func targetFrom(draft *consolev1.TargetDraft) (map[string]any, error) {
	engine := operatorv1alpha1.DatabaseEngine(draft.GetEngine())
	if !slices.Contains(engines, engine) {
		return nil, errDraft("engine %q is not one the operator supports", engine)
	}
	target := map[string]any{
		"engine":  string(engine),
		"urlFrom": map[string]any{"name": draft.GetUrlSecretName(), "key": draft.GetUrlSecretKey()},
	}
	set(target, "coordinationKey", draft.GetCoordinationKey())
	if realm := draft.GetRealm(); realm != "" {
		target["realmRef"] = map[string]any{"name": realm}
	}
	return target, nil
}

func sourceFrom(draft *consolev1.SourceDraft) (map[string]any, error) {
	switch kind := draft.GetKind().(type) {
	case *consolev1.SourceDraft_Oci:
		source := map[string]any{"ociRef": kind.Oci.GetRef()}
		if kind.Oci.GetVerificationPolicyName() != "" || kind.Oci.GetVerificationPolicyKey() != "" {
			source["verificationPolicyFrom"] = map[string]any{
				"name": kind.Oci.GetVerificationPolicyName(), "key": kind.Oci.GetVerificationPolicyKey(),
			}
		}
		return source, nil
	case *consolev1.SourceDraft_ConfigMap:
		items, err := configMapItems(kind.ConfigMap)
		if err != nil {
			return nil, err
		}
		return map[string]any{"configMap": map[string]any{"name": kind.ConfigMap.GetName(), "items": items}}, nil
	case *consolev1.SourceDraft_Git:
		return map[string]any{"git": gitFrom(kind.Git)}, nil
	default:
		return nil, errDraft("a source is required: an OCI artifact, a ConfigMap or a Git repository")
	}
}

// configMapItems is the one file a schema selects, or the files of a
// migration directory. The CRD judges how many a kind may select.
func configMapItems(draft *consolev1.ConfigMapSourceDraft) ([]any, error) {
	files := draft.GetFiles()
	if len(files) == 0 {
		return []any{map[string]any{"key": draft.GetKey(), "path": draft.GetPath()}}, nil
	}
	if draft.GetKey() != "" || draft.GetPath() != "" {
		return nil, errDraft("a ConfigMap source names one file by key and path, or several as files, not both")
	}
	items := make([]any, 0, len(files))
	for _, file := range files {
		items = append(items, map[string]any{"key": file.GetKey(), "path": file.GetPath()})
	}
	return items, nil
}

// gitFrom is a Git source: the commit, the path, and whom the fetch trusts.
// The CRD refuses an ssh:// repository without known hosts and a trust that
// names half a ConfigMap key.
func gitFrom(draft *consolev1.GitSourceDraft) map[string]any {
	ref := make(map[string]any)
	set(ref, "branch", draft.GetBranch())
	set(ref, "tag", draft.GetTag())
	set(ref, "commit", draft.GetCommit())
	git := map[string]any{"url": draft.GetUrl(), "ref": ref, "path": draft.GetPath()}
	if secret := draft.GetCredentialsSecret(); secret != "" {
		git["authFrom"] = map[string]any{"name": secret}
	}
	transport := make(map[string]any)
	if name, key := draft.GetCaConfigMap(), draft.GetCaKey(); name != "" || key != "" {
		transport["caFrom"] = map[string]any{"name": name, "key": key}
	}
	if name, key := draft.GetKnownHostsConfigMap(), draft.GetKnownHostsKey(); name != "" || key != "" {
		transport["knownHostsFrom"] = map[string]any{"name": name, "key": key}
	}
	if len(transport) > 0 {
		git["transport"] = transport
	}
	return git
}

// set puts value under key when it is not empty, as a manifest that leaves a
// field out.
func set(object map[string]any, key, value string) {
	if value != "" {
		object[key] = value
	}
}
