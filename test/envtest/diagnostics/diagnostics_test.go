package diagnostics_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

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

// The operator's own minimal example.
const migrationYAML = `
apiVersion: operator.ptah.run/v1alpha1
kind: PtahMigration
metadata:
  name: billing
spec:
  target:
    engine: PostgreSQL
    coordinationKey: envtest/billing
    urlFrom:
      name: billing-database
      key: url
  artifact:
    ociRef: oci://registry.example/billing/migrations:1.0.0
    verificationPolicyFrom:
      name: ptah-migration-verification-policy
      key: policy.yaml
`

// lab is one namespace with a migration whose last run names a Job.
type lab struct {
	namespace string
	migration *operatorv1alpha1.PtahMigration
	job       *batchv1.Job
	pod       *corev1.Pod
}

// arrange stores a migration, its run's Job and Pod, a Pod that only claims
// the Job by label, and an event about each, and lets the user read what
// the rules say.
func arrange(t *testing.T, user string, recorded func(job *batchv1.Job) types.UID, rules ...rbacv1.PolicyRule) lab {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	l := lab{namespace: user + "-billing"}
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: l.namespace}}), qt.IsNil)

	l.job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: l.namespace, Name: "billing-run-1"},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{{Name: "runner", Image: "registry.example/runner:1"}},
		}}},
	}
	c.Assert(plane.Admin.Create(ctx, l.job), qt.IsNil)

	migration := &unstructured.Unstructured{}
	c.Assert(yaml.Unmarshal([]byte(migrationYAML), &migration.Object), qt.IsNil)
	migration.SetNamespace(l.namespace)
	c.Assert(plane.Admin.Create(ctx, migration), qt.IsNil)
	status := fmt.Sprintf(`{"status":{"lastRun":{"outcome":"Failed","jobName":%q,"jobUID":%q,"startedAt":"2026-10-19T10:00:00Z"}}}`,
		l.job.Name, recorded(l.job))
	c.Assert(plane.Admin.Status().Patch(ctx, migration, client.RawPatch(types.MergePatchType, []byte(status))), qt.IsNil)
	l.migration = &operatorv1alpha1.PtahMigration{}
	c.Assert(plane.Admin.Get(ctx, client.ObjectKeyFromObject(migration), l.migration), qt.IsNil)

	l.pod = runPod(l.namespace, "billing-run-1-x7k2p", l.job, true)
	c.Assert(plane.Admin.Create(ctx, l.pod), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, runPod(l.namespace, "impostor", l.job, false)), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, event(l.namespace, "PtahMigration", l.migration.Name, l.migration.UID, "ApplyFailed")), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, event(l.namespace, "Job", l.job.Name, l.job.UID, "BackoffLimitExceeded")), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, event(l.namespace, "Pod", l.pod.Name, l.pod.UID, "Started")), qt.IsNil)

	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: l.namespace, Name: "diag"}, Rules: rules}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: l.namespace, Name: "diag"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "diag"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: user}},
	}), qt.IsNil)
	return l
}

func theJob(job *batchv1.Job) types.UID { return job.UID }

func anEarlierJob(*batchv1.Job) types.UID { return "0b6a3d4e-0000-4000-8000-000000000001" }

var (
	readMigrations = rbacv1.PolicyRule{APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahmigrations"}, Verbs: []string{"get"}}
	readJobs       = rbacv1.PolicyRule{APIGroups: []string{"batch"}, Resources: []string{"jobs"}, Verbs: []string{"get"}}
	listPods       = rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}
	listEvents     = rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"list"}}
)

func runPod(namespace, name string, job *batchv1.Job, controlled bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace, Name: name,
			Labels: map[string]string{batchv1.ControllerUidLabel: string(job.UID)},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{{Name: "runner", Image: "registry.example/runner:1"}},
		},
	}
	controller := true
	references := map[bool][]metav1.OwnerReference{
		true: {{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}},
	}
	pod.OwnerReferences = references[controlled]
	return pod
}

func event(namespace, kind, name string, uid types.UID, reason string) *corev1.Event {
	now := metav1.NewTime(time.Now())
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: namespace, GenerateName: "diag-"},
		InvolvedObject: corev1.ObjectReference{Kind: kind, Namespace: namespace, Name: name, UID: uid},
		Type:           corev1.EventTypeWarning, Reason: reason, Message: reason + " happened",
		FirstTimestamp: now, LastTimestamp: now, Count: 1,
		Source: corev1.EventSource{Component: "envtest"},
	}
}

// fakeLogs stands in for the kubelet, which envtest does not run.
func fakeLogs(_ context.Context, _ *rest.Config, _, _, _ string) ([]byte, error) {
	return []byte("connecting\n\x1b[31mERROR\x1b[0m: relation \"orders\" does not exist\n"), nil
}

func diagnose(t *testing.T, user string, l lab) (*consolev1.GetDiagnosticsResponse, error) {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(kube.Principal{Mode: kube.ModeLocal, Context: "envtest", Generation: 1})
	c.Assert(err, qt.IsNil)
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDiagnosticsServiceHandler(server, api.NewDiagnosticsService(factory{built}, api.Scope{}, fakeLogs))
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	diagnostics := consolev1connect.NewDiagnosticsServiceClient(connect.NewClient(connectinprocess.New(server)))
	return diagnostics.GetDiagnostics(ctx, &consolev1.GetDiagnosticsRequest{
		Kind: consolev1.ResourceKind_RESOURCE_KIND_MIGRATION, Namespace: l.namespace, Name: "billing",
		Uid: string(l.migration.UID), IncludeLogs: true,
	})
}

func reasons(list *consolev1.EventList) []string {
	var out []string
	for _, event := range list.GetEvents() {
		out = append(out, event.GetReason())
	}
	return out
}

// The Job the last run names is shown with the Pod it controls, the events
// about each, and the end of the log as plain text. A Pod that only carries
// the Job's label is not the Job's.
func TestDiagnosticsShowTheRecordedRun(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := arrange(t, "oncall", theJob, readMigrations, readJobs, listPods, listEvents)

	response, err := diagnose(t, "oncall", l)

	c.Assert(err, qt.IsNil)
	c.Assert(reasons(response.GetEvents()), qt.DeepEquals, []string{"ApplyFailed"})
	c.Assert(response.GetJobs(), qt.HasLen, 1)
	job := response.GetJobs()[0]
	c.Assert(job.GetNamedBy(), qt.Equals, "status.lastRun")
	c.Assert(job.GetEvidence(), qt.Equals, consolev1.Evidence_EVIDENCE_SHOWN)
	c.Assert(job.GetUid(), qt.Equals, string(l.job.UID))
	c.Assert(reasons(job.GetEvents()), qt.DeepEquals, []string{"BackoffLimitExceeded"})
	c.Assert(job.GetPodsEvidence(), qt.Equals, consolev1.Evidence_EVIDENCE_SHOWN)
	c.Assert(job.GetPods(), qt.HasLen, 1)
	pod := job.GetPods()[0]
	c.Assert(pod.GetName(), qt.Equals, l.pod.Name)
	c.Assert(reasons(pod.GetEvents()), qt.DeepEquals, []string{"Started"})
}

// A Job under the recorded name with another UID is not the run's, and is
// not shown.
func TestAJobCreatedAgainUnderTheNameIsNotShown(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := arrange(t, "careful", anEarlierJob, readMigrations, readJobs, listPods, listEvents)

	response, err := diagnose(t, "careful", l)

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetJobs(), qt.HasLen, 1)
	job := response.GetJobs()[0]
	c.Assert(job.GetEvidence(), qt.Equals, consolev1.Evidence_EVIDENCE_NOT_RETAINED)
	c.Assert(job.GetProblem(), qt.Contains, "different Job")
	c.Assert(job.GetState(), qt.IsNil)
	c.Assert(job.GetPods(), qt.HasLen, 0)
	c.Assert(job.GetEvents(), qt.IsNil)
}

// Evidence the identity may not read is marked so, in the API server's
// words, beside what it may read.
func TestEvidenceNotGrantedIsMarked(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := arrange(t, "narrow", theJob, readMigrations, readJobs)

	response, err := diagnose(t, "narrow", l)

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetEvents().GetEvidence(), qt.Equals, consolev1.Evidence_EVIDENCE_NOT_GRANTED)
	c.Assert(response.GetEvents().GetProblem(), qt.Contains, `cannot list resource "events"`)
	job := response.GetJobs()[0]
	c.Assert(job.GetEvidence(), qt.Equals, consolev1.Evidence_EVIDENCE_SHOWN)
	c.Assert(job.GetPodsEvidence(), qt.Equals, consolev1.Evidence_EVIDENCE_NOT_GRANTED)
	c.Assert(job.GetPodsProblem(), qt.Contains, `cannot list resource "pods"`)
}
