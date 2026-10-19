package api_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// failedRun is a migration whose last run's Job ended with one failed Pod.
func failedRun() []client.Object {
	controller := true
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing-run-1", UID: "uid-job"}}
	return []client.Object{
		&operatorv1alpha1.PtahMigration{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing", UID: "uid-migration"},
			Status: operatorv1alpha1.PtahMigrationStatus{LastRun: &operatorv1alpha1.MigrationRunStatus{
				Outcome: operatorv1alpha1.MigrationRunOutcomeFailed, JobName: "billing-run-1", JobUID: "uid-job",
			}},
		},
		job,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace, Name: "billing-run-1-x7k2p", UID: "uid-pod",
				Labels:          map[string]string{batchv1.ControllerUidLabel: "uid-job"},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "runner",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 3, Reason: "Error"}},
			}}},
		},
	}
}

func diagnostics(t *testing.T, logs api.LogReader) (consolev1connect.DiagnosticsServiceClient, context.Context) {
	t.Helper()
	scheme, err := kube.Scheme()
	qt.New(t).Assert(err, qt.IsNil)
	built := fake.NewClientBuilder().WithScheme(scheme).WithObjects(failedRun()...).
		WithIndex(&corev1.Event{}, "involvedObject.uid", func(object client.Object) []string {
			return []string{string(object.(*corev1.Event).InvolvedObject.UID)}
		}).Build()
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDiagnosticsServiceHandler(server, api.NewDiagnosticsService(staticFactory{built}, api.Scope{}, logs))
	return consolev1connect.NewDiagnosticsServiceClient(connect.NewClient(connectinprocess.New(server))), signedIn(t, store)
}

func logOf(text string, err error) api.LogReader {
	return func(context.Context, *rest.Config, string, string, string) ([]byte, error) {
		return []byte(text), err
	}
}

func numbered(count int) string {
	var b strings.Builder
	for i := range count {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	return b.String()
}

// The log comes back as plain text, cut from the end to the line and byte
// bounds, and says when it was cut.
func TestALogExcerptIsPlainTextFromTheEnd(t *testing.T) {
	t.Parallel()
	forbidden := apierrors.NewForbidden(corev1.Resource("pods/log"), "billing-run-1-x7k2p", errors.New(`User "dev" cannot get resource "pods/log"`))
	long := strings.Repeat(strings.Repeat("y", 1023)+"\n", 70)
	tests := []struct {
		name      string
		logs      api.LogReader
		evidence  consolev1.Evidence
		text      string
		lines     int
		truncated bool
	}{
		{
			name: "escape sequences and control characters are dropped", logs: logOf("\x1b[31mERROR\x1b[0m\x07: bad \xff byte\n", nil),
			evidence: consolev1.Evidence_EVIDENCE_SHOWN, text: "ERROR: bad � byte\n", lines: 1, truncated: false,
		},
		{
			name: "one line more than the bound", logs: logOf(numbered(api.LogTailLines+1), nil),
			evidence: consolev1.Evidence_EVIDENCE_SHOWN, text: "", lines: api.LogTailLines, truncated: true,
		},
		{
			name: "exactly the bound", logs: logOf(numbered(api.LogTailLines), nil),
			evidence: consolev1.Evidence_EVIDENCE_SHOWN, text: "", lines: api.LogTailLines, truncated: false,
		},
		{
			name: "more bytes than the bound", logs: logOf(long, nil),
			evidence: consolev1.Evidence_EVIDENCE_SHOWN, text: "", lines: api.LogMaxBytes / 1024, truncated: true,
		},
		{
			name: "the identity may not read logs", logs: logOf("", forbidden),
			evidence: consolev1.Evidence_EVIDENCE_NOT_GRANTED, text: "", lines: 0, truncated: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service, ctx := diagnostics(t, test.logs)

			response, err := service.GetDiagnostics(ctx, &consolev1.GetDiagnosticsRequest{
				Kind: consolev1.ResourceKind_RESOURCE_KIND_MIGRATION, Namespace: namespace, Name: "billing", Uid: "uid-migration", IncludeLogs: true,
			})

			c.Assert(err, qt.IsNil)
			container := response.GetJobs()[0].GetPods()[0].GetContainers()[0]
			c.Assert(container.GetExitCode(), qt.Equals, int32(3))
			log := container.GetLog()
			c.Assert(log.GetEvidence(), qt.Equals, test.evidence)
			c.Assert(strings.Count(log.GetText(), "\n"), qt.Equals, test.lines)
			c.Assert(len(log.GetText()) <= api.LogMaxBytes, qt.IsTrue, qt.Commentf("%d bytes", len(log.GetText())))
			c.Assert(log.GetTruncated(), qt.Equals, test.truncated)
			c.Assert(log.GetText(), qt.Contains, test.text)
		})
	}
}

// Logs are read only when asked for.
func TestLogsAreReadOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	var reads atomic.Int32
	service, ctx := diagnostics(t, func(context.Context, *rest.Config, string, string, string) ([]byte, error) {
		reads.Add(1)
		return nil, nil
	})

	response, err := service.GetDiagnostics(ctx, &consolev1.GetDiagnosticsRequest{
		Kind: consolev1.ResourceKind_RESOURCE_KIND_MIGRATION, Namespace: namespace, Name: "billing", Uid: "uid-migration",
	})

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetJobs()[0].GetPods()[0].GetContainers()[0].GetLog(), qt.IsNil)
	c.Assert(reads.Load(), qt.Equals, int32(0))
}

// Another migration under the name is not diagnosed.
func TestDiagnosticsRefuseAnotherObjectUnderTheName(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service, ctx := diagnostics(t, logOf("", nil))

	_, err := service.GetDiagnostics(ctx, &consolev1.GetDiagnosticsRequest{
		Kind: consolev1.ResourceKind_RESOURCE_KIND_MIGRATION, Namespace: namespace, Name: "billing", Uid: "uid-earlier-migration",
	})

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeFailedPrecondition)
}
