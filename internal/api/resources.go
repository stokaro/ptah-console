package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"connectrpc.com/connect/v2"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"github.com/stokaro/ptah-operator/pkg/planread"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/model"
)

// maxParallelNamespaces bounds the namespaces one list reads at once.
const maxParallelNamespaces = 8

// eachNamespace reads every namespace with read, at most a few at a time,
// and reports how each went. A forbidden namespace is a result, not an error:
// it is not the same as an empty one.
func eachNamespace(ctx context.Context, namespaces []string, read func(context.Context, string) (bool, error)) []*consolev1.NamespaceResult {
	results := make([]*consolev1.NamespaceResult, len(namespaces))
	slots := make(chan struct{}, maxParallelNamespaces)
	var wg sync.WaitGroup
	for i, namespace := range namespaces {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			truncated, err := read(ctx, namespace)
			results[i] = namespaceResult(namespace, err)
			results[i].Truncated = truncated
		})
	}
	wg.Wait()
	return results
}

// collect reads every namespace with read and gathers what each returned,
// sorted by namespace and name.
func collect[T any](
	ctx context.Context,
	namespaces []string,
	read func(context.Context, string) ([]T, bool, error),
	key func(T) string,
) ([]T, []*consolev1.NamespaceResult) {
	var mu sync.Mutex
	var items []T
	results := eachNamespace(ctx, namespaces, func(ctx context.Context, namespace string) (bool, error) {
		found, truncated, err := read(ctx, namespace)
		if err != nil {
			return false, err
		}
		mu.Lock()
		defer mu.Unlock()
		items = append(items, found...)
		return truncated, nil
	})
	sort.Slice(items, func(i, j int) bool { return key(items[i]) < key(items[j]) })
	return items, results
}

// pages lists one kind in one namespace, a page at a time, until the kind is
// read or bound objects are, and says whether it stopped short. Each page goes
// to each before the next is asked for. Selection narrows what the API server
// returns, such as by a field.
func pages[L client.ObjectList](ctx context.Context, c client.Client, namespace string, bound int, list func() L, each func(L), selection ...client.ListOption) (bool, error) {
	read, next := 0, ""
	for {
		page := list()
		options := append([]client.ListOption{client.InNamespace(namespace), client.Limit(int64(min(listPage, bound-read)))}, selection...)
		if next != "" {
			options = append(options, client.Continue(next))
		}
		if err := c.List(ctx, page, options...); err != nil {
			return false, err
		}
		each(page)
		read += meta.LenList(page)
		next = page.GetContinue()
		switch {
		case next == "":
			return false, nil
		case read >= bound:
			return true, nil
		}
	}
}

func refKey(ref *consolev1.ObjectRef) string { return ref.GetNamespace() + "/" + ref.GetName() }

// listEach checks the namespaces, takes the session's client, and lists each
// namespace with it.
func listEach[T any](
	ctx context.Context,
	factory kube.ClientFactory,
	scope Scope,
	namespaces []string,
	list func(context.Context, client.Client, string, int) ([]T, bool, error),
	ref func(T) *consolev1.ObjectRef,
) ([]T, []*consolev1.NamespaceResult, error) {
	if err := scope.check(namespaces); err != nil {
		return nil, nil, err
	}
	c, _, err := actor(ctx, factory)
	if err != nil {
		return nil, nil, err
	}
	items, results := collect(ctx, namespaces, func(ctx context.Context, namespace string) ([]T, bool, error) {
		return list(ctx, c, namespace, scope.maxPerNamespace())
	}, func(item T) string { return refKey(ref(item)) })
	return items, results, nil
}

func listSchemas(ctx context.Context, c client.Client, namespace string, bound int) ([]*consolev1.Schema, bool, error) {
	var out []*consolev1.Schema
	truncated, err := pages(ctx, c, namespace, bound, func() *operatorv1alpha1.PtahSchemaList { return &operatorv1alpha1.PtahSchemaList{} },
		func(page *operatorv1alpha1.PtahSchemaList) {
			for i := range page.Items {
				out = append(out, model.Schema(&page.Items[i]))
			}
		})
	return out, truncated, err
}

func listMigrations(ctx context.Context, c client.Client, namespace string, bound int) ([]*consolev1.Migration, bool, error) {
	var out []*consolev1.Migration
	truncated, err := pages(ctx, c, namespace, bound, func() *operatorv1alpha1.PtahMigrationList { return &operatorv1alpha1.PtahMigrationList{} },
		func(page *operatorv1alpha1.PtahMigrationList) {
			for i := range page.Items {
				out = append(out, model.Migration(&page.Items[i]))
			}
		})
	return out, truncated, err
}

// SchemaService serves SchemaService.
type SchemaService struct {
	consolev1connect.UnimplementedSchemaServiceHandler

	factory kube.ClientFactory
	scope   Scope
}

// NewSchemaService returns the SchemaService.
func NewSchemaService(factory kube.ClientFactory, scope Scope) *SchemaService {
	return &SchemaService{factory: factory, scope: scope}
}

// ListSchemas lists the schemas of each namespace as the session's identity.
func (s *SchemaService) ListSchemas(ctx context.Context, request *consolev1.ListSchemasRequest) (*consolev1.ListSchemasResponse, error) {
	schemas, results, err := listEach(ctx, s.factory, s.scope, request.GetNamespaces(), listSchemas, (*consolev1.Schema).GetRef)
	if err != nil {
		return nil, err
	}
	return &consolev1.ListSchemasResponse{Schemas: schemas, Namespaces: results}, nil
}

// GetSchema reads one schema.
func (s *SchemaService) GetSchema(ctx context.Context, request *consolev1.GetSchemaRequest) (*consolev1.GetSchemaResponse, error) {
	if err := s.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	c, _, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	schema := &operatorv1alpha1.PtahSchema{}
	key := types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetName()}
	if err := c.Get(ctx, key, schema); err != nil {
		return nil, readError(err, "schema "+key.String())
	}
	return &consolev1.GetSchemaResponse{Schema: model.Schema(schema)}, nil
}

// GetSchemaPlan reads a plan through the operator's plan reader. When the
// identity may read the plan but not its chunks, the manifest comes back
// without SQL; when the stored plan fails a check, nothing of its SQL does.
func (s *SchemaService) GetSchemaPlan(ctx context.Context, request *consolev1.GetSchemaPlanRequest) (*consolev1.GetSchemaPlanResponse, error) {
	if err := s.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	c, _, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	selection := planread.Current
	if request.GetSelection() == consolev1.PlanSelection_PLAN_SELECTION_APPLIED {
		selection = planread.Applied
	}

	read, err := planread.Load(ctx, c, request.GetNamespace(), request.GetSchema(), selection)
	if err == nil {
		return &consolev1.GetSchemaPlanResponse{Plan: model.SchemaPlan(read, request.GetSelection())}, nil
	}
	switch {
	case errors.Is(err, planread.ErrSchemaNotFound):
		return nil, connect.NewError(connect.CodeNotFound, "the schema does not exist").WithCause(err)
	case errors.Is(err, planread.ErrNoPlan):
		return nil, connect.NewError(connect.CodeNotFound, noPlan(selection)).WithCause(err)
	}

	// The SQL did not come back. Say why, and add what the identity may still
	// read of the manifest; a manifest that does not read either changes
	// nothing about why there is no SQL.
	var access consolev1.SqlAccess
	var problem string
	switch {
	case errors.Is(err, planread.ErrIntegrity):
		access, problem = consolev1.SqlAccess_SQL_ACCESS_INTEGRITY_FAILED, err.Error()
	case errors.Is(err, planread.ErrUnsupported):
		access, problem = consolev1.SqlAccess_SQL_ACCESS_UNSUPPORTED, err.Error()
	case apierrors.IsForbidden(err):
		access = consolev1.SqlAccess_SQL_ACCESS_NOT_GRANTED
		problem = "This identity may read the plan but not the chunks its SQL is stored in (get ptahschemaplanchunks)."
	default:
		return nil, readError(err, "the plan's SQL")
	}
	plan := &consolev1.SchemaPlan{Namespace: request.GetNamespace(), Schema: request.GetSchema(), Selection: request.GetSelection()}
	if manifest, manifestErr := s.manifest(ctx, c, request.GetNamespace(), request.GetSchema(), selection); manifestErr == nil {
		plan = model.SchemaPlanManifest(manifest, request.GetSelection())
	}
	plan.SqlAccess, plan.SqlProblem = access, problem
	return &consolev1.GetSchemaPlanResponse{Plan: plan}, nil
}

func noPlan(selection planread.Selection) string {
	if selection == planread.Applied {
		return "no confirmed apply is recorded for this schema"
	}
	return "this schema has no current plan"
}

// manifest reads the plan object the schema's status names, held to the UID
// the status records.
func (s *SchemaService) manifest(ctx context.Context, c client.Client, namespace, name string, selection planread.Selection) (*operatorv1alpha1.PtahSchemaPlan, error) {
	schema := &operatorv1alpha1.PtahSchema{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, schema); err != nil {
		return nil, err
	}
	var planName string
	var planUID types.UID
	switch {
	case selection == planread.Applied && schema.Status.Applied != nil:
		planName, planUID = schema.Status.Applied.PlanRef.Name, schema.Status.Applied.PlanRef.UID
	case selection == planread.Current && schema.Status.Plan != nil:
		planName, planUID = schema.Status.Plan.Name, schema.Status.Plan.UID
	default:
		return nil, fmt.Errorf("%w: no plan recorded", planread.ErrNoPlan)
	}
	plan := &operatorv1alpha1.PtahSchemaPlan{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: planName}, plan); err != nil {
		return nil, err
	}
	if plan.UID != planUID {
		return nil, fmt.Errorf("the plan %s was replaced under the same name", planName)
	}
	return plan, nil
}

// MigrationService serves MigrationService.
type MigrationService struct {
	consolev1connect.UnimplementedMigrationServiceHandler

	factory kube.ClientFactory
	scope   Scope
}

// NewMigrationService returns the MigrationService.
func NewMigrationService(factory kube.ClientFactory, scope Scope) *MigrationService {
	return &MigrationService{factory: factory, scope: scope}
}

// ListMigrations lists the migrations of each namespace as the session's
// identity.
func (s *MigrationService) ListMigrations(ctx context.Context, request *consolev1.ListMigrationsRequest) (*consolev1.ListMigrationsResponse, error) {
	migrations, results, err := listEach(ctx, s.factory, s.scope, request.GetNamespaces(), listMigrations, (*consolev1.Migration).GetRef)
	if err != nil {
		return nil, err
	}
	return &consolev1.ListMigrationsResponse{Migrations: migrations, Namespaces: results}, nil
}

// GetMigration reads one migration.
func (s *MigrationService) GetMigration(ctx context.Context, request *consolev1.GetMigrationRequest) (*consolev1.GetMigrationResponse, error) {
	if err := s.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	c, _, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	migration := &operatorv1alpha1.PtahMigration{}
	key := types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetName()}
	if err := c.Get(ctx, key, migration); err != nil {
		return nil, readError(err, "migration "+key.String())
	}
	return &consolev1.GetMigrationResponse{Migration: model.Migration(migration)}, nil
}

// GetMigrationPlan reads the plan the migration's status names, held to the
// UID and owner it records. A migration plan names files and checksums; the
// operator API does not carry their SQL.
func (s *MigrationService) GetMigrationPlan(ctx context.Context, request *consolev1.GetMigrationPlanRequest) (*consolev1.GetMigrationPlanResponse, error) {
	if err := s.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	c, _, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	migration := &operatorv1alpha1.PtahMigration{}
	key := types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetMigration()}
	if err := c.Get(ctx, key, migration); err != nil {
		return nil, readError(err, "migration "+key.String())
	}
	ref := migration.Status.Plan
	if ref == nil || ref.Name == "" {
		return nil, connect.NewError(connect.CodeNotFound, "this migration has no current plan")
	}
	plan := &operatorv1alpha1.PtahMigrationPlan{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: ref.Name}, plan); err != nil {
		return nil, readError(err, "plan "+ref.Name)
	}
	if plan.UID != ref.UID || plan.Spec.MigrationRef.UID != migration.UID {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "the plan under that name is not the one the migration records")
	}
	return &consolev1.GetMigrationPlanResponse{Plan: model.MigrationPlan(plan)}, nil
}
