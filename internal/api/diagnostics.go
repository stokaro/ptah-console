package api

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	"connectrpc.com/connect/v2"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"google.golang.org/protobuf/types/known/timestamppb"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// Bounds on what one diagnosis reads.
const (
	// LogTailLines is how many of a container's last log lines are shown.
	LogTailLines = 200
	// LogMaxBytes bounds the text shown from one container's log.
	LogMaxBytes = 64 << 10
	// eventsPerObject bounds the events shown about one object.
	eventsPerObject = 50
	// podsPerJob bounds the Pods listed for one Job.
	podsPerJob = 20
)

// LogReader reads the end of one container's log with the identity's own
// configuration. It returns at most LogTailLines+1 lines, so a caller can
// tell a log that has more.
type LogReader func(ctx context.Context, config *rest.Config, namespace, pod, container string) ([]byte, error)

// DiagnosticsService serves DiagnosticsService.
type DiagnosticsService struct {
	consolev1connect.UnimplementedDiagnosticsServiceHandler

	factory kube.ClientFactory
	scope   Scope
	logs    LogReader
}

// NewDiagnosticsService returns the DiagnosticsService. A nil logs reads
// through the API server's pods/log subresource.
func NewDiagnosticsService(factory kube.ClientFactory, scope Scope, logs LogReader) *DiagnosticsService {
	if logs == nil {
		logs = PodLogs
	}
	return &DiagnosticsService{factory: factory, scope: scope, logs: logs}
}

// PodLogs reads a container's last lines through pods/log.
func PodLogs(ctx context.Context, config *rest.Config, namespace, pod, container string) ([]byte, error) {
	if config == nil {
		return nil, errors.New("no configuration to read logs with")
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	tail := int64(LogTailLines + 1)
	// Four times the shown bound: the cut to LogMaxBytes is made from the
	// end, on a line boundary, here rather than by the kubelet from the start.
	limit := int64(4 * LogMaxBytes)
	return clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container, TailLines: &tail, LimitBytes: &limit,
	}).DoRaw(ctx)
}

// jobRef is a Job a status field names.
type jobRef struct {
	namedBy string
	name    string
	uid     types.UID
}

// GetDiagnostics shows the evidence about one schema or migration.
func (d *DiagnosticsService) GetDiagnostics(ctx context.Context, request *consolev1.GetDiagnosticsRequest) (*consolev1.GetDiagnosticsResponse, error) {
	if err := d.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	if err := required([]field{{"name", request.GetName()}, {"uid", request.GetUid()}}); err != nil {
		return nil, err
	}
	c, _, err := actor(ctx, d.factory)
	if err != nil {
		return nil, err
	}
	refs, err := jobRefs(ctx, c, request)
	if err != nil {
		return nil, err
	}
	logs := noLogs
	if request.GetIncludeLogs() {
		logs = d.logReading(ctx, c)
	}
	response := &consolev1.GetDiagnosticsResponse{Events: events(ctx, c, request.GetNamespace(), types.UID(request.GetUid()))}
	for _, ref := range refs {
		response.Jobs = append(response.Jobs, d.job(ctx, c, request.GetNamespace(), ref, logs))
	}
	return response, nil
}

// jobRefs reads the owner and returns the Jobs its status names, each once.
func jobRefs(ctx context.Context, c *kube.Client, request *consolev1.GetDiagnosticsRequest) ([]jobRef, error) {
	key := types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetName()}
	var refs []jobRef
	var uid types.UID
	switch request.GetKind() {
	case consolev1.ResourceKind_RESOURCE_KIND_SCHEMA:
		schema := &operatorv1alpha1.PtahSchema{}
		if err := c.Get(ctx, key, schema); err != nil {
			return nil, readError(err, "the schema")
		}
		uid = schema.UID
		if op := schema.Status.ActiveOperation; op != nil {
			refs = append(refs, jobRef{"status.activeOperation", op.JobName, op.JobUID})
		}
		if pending := schema.Status.PendingObservation; pending != nil {
			refs = append(refs, jobRef{"status.pendingObservation", pending.ApplyJobName, pending.ApplyJobUID})
		}
	case consolev1.ResourceKind_RESOURCE_KIND_MIGRATION:
		migration := &operatorv1alpha1.PtahMigration{}
		if err := c.Get(ctx, key, migration); err != nil {
			return nil, readError(err, "the migration")
		}
		uid = migration.UID
		if op := migration.Status.ActiveOperation; op != nil {
			refs = append(refs, jobRef{"status.activeOperation", op.JobName, op.JobUID})
		}
		if run := migration.Status.UnresolvedRun; run != nil {
			refs = append(refs, jobRef{"status.unresolvedRun", run.JobName, run.JobUID})
		}
		if run := migration.Status.LastRun; run != nil {
			refs = append(refs, jobRef{"status.lastRun", run.JobName, run.JobUID})
		}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, "name a schema or a migration")
	}
	if string(uid) != request.GetUid() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "another object holds this name now; look again")
	}
	return dedupe(refs), nil
}

// dedupe keeps the first field that names each Job.
func dedupe(refs []jobRef) []jobRef {
	seen := make(map[string]bool, len(refs))
	out := refs[:0]
	for _, ref := range refs {
		key := ref.name + "/" + string(ref.uid)
		if ref.name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ref)
	}
	return out
}

// job reads one Job by the name and UID the status recorded, and its Pods.
func (d *DiagnosticsService) job(ctx context.Context, c *kube.Client, namespace string, ref jobRef, logs logReading) *consolev1.JobEvidence {
	out := &consolev1.JobEvidence{NamedBy: ref.namedBy, Name: ref.name, Uid: string(ref.uid)}
	if ref.uid == "" {
		out.Evidence = consolev1.Evidence_EVIDENCE_NOT_RETAINED
		out.Problem = "The status names no Job UID yet; a Job is shown only by the UID the operator recorded."
		return out
	}
	job := &batchv1.Job{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.name}, job)
	if out.Evidence, out.Problem = evidenceOf(err, "the Job"); err != nil {
		return out
	}
	if job.UID != ref.uid {
		out.Evidence = consolev1.Evidence_EVIDENCE_NOT_RETAINED
		out.Problem = "A different Job holds this name now; the recorded one is gone."
		return out
	}
	out.State = jobState(job)
	out.Events = events(ctx, c, namespace, job.UID)

	pods := &corev1.PodList{}
	err = c.List(ctx, pods, client.InNamespace(namespace),
		client.MatchingLabels{batchv1.ControllerUidLabel: string(job.UID)}, client.Limit(podsPerJob))
	if out.PodsEvidence, out.PodsProblem = evidenceOf(err, "the Job's Pods"); err != nil {
		return out
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		// The label is anyone's to set; the controller reference is the Job's.
		if !metav1.IsControlledBy(pod, job) {
			continue
		}
		out.Pods = append(out.Pods, podEvidence(ctx, c, pod, logs))
	}
	return out
}

func jobState(job *batchv1.Job) *consolev1.JobState {
	state := &consolev1.JobState{
		Active: job.Status.Active, Succeeded: job.Status.Succeeded, Failed: job.Status.Failed,
		StartedAt: stamp(job.Status.StartTime), CompletedAt: stamp(job.Status.CompletionTime),
	}
	for _, condition := range job.Status.Conditions {
		state.Conditions = append(state.Conditions, &consolev1.Condition{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message,
			LastTransition: stamp(&condition.LastTransitionTime),
		})
	}
	return state
}

func podEvidence(ctx context.Context, c client.Client, pod *corev1.Pod, logs logReading) *consolev1.PodEvidence {
	out := &consolev1.PodEvidence{
		Name: pod.Name, Uid: string(pod.UID), Phase: string(pod.Status.Phase),
		Reason: pod.Status.Reason, Message: pod.Status.Message,
		Events: events(ctx, c, pod.Namespace, pod.UID),
	}
	for _, status := range pod.Status.InitContainerStatuses {
		container := containerEvidence(status)
		container.Init = true
		container.Log = logs(pod, status.Name)
		out.Containers = append(out.Containers, container)
	}
	for _, status := range pod.Status.ContainerStatuses {
		container := containerEvidence(status)
		container.Log = logs(pod, status.Name)
		out.Containers = append(out.Containers, container)
	}
	return out
}

func containerEvidence(status corev1.ContainerStatus) *consolev1.ContainerEvidence {
	out := &consolev1.ContainerEvidence{Name: status.Name, Ready: status.Ready, RestartCount: status.RestartCount}
	switch state := status.State; {
	case state.Terminated != nil:
		out.State, out.Reason, out.Message, out.ExitCode = "terminated", state.Terminated.Reason, state.Terminated.Message, state.Terminated.ExitCode
	case state.Running != nil:
		out.State = "running"
	case state.Waiting != nil:
		out.State, out.Reason, out.Message = "waiting", state.Waiting.Reason, state.Waiting.Message
	}
	return out
}

// logReading reads one container's log excerpt, or leaves it out.
type logReading func(pod *corev1.Pod, container string) *consolev1.LogExcerpt

// noLogs leaves every log out.
func noLogs(*corev1.Pod, string) *consolev1.LogExcerpt { return nil }

// logReading reads logs with the identity's own configuration.
func (d *DiagnosticsService) logReading(ctx context.Context, c *kube.Client) logReading {
	return func(pod *corev1.Pod, container string) *consolev1.LogExcerpt {
		data, err := d.logs(ctx, c.Config, pod.Namespace, pod.Name, container)
		evidence, problem := evidenceOf(err, "the log")
		out := &consolev1.LogExcerpt{Evidence: evidence, Problem: problem}
		if err == nil {
			out.Text, out.Truncated = excerpt(data)
		}
		return out
	}
}

// terminalControl matches terminal escape sequences and the control
// characters a log line has no business carrying.
var terminalControl = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\x1b[\x40-\x5f]|[\x00-\x08\x0b-\x1f\x7f]`)

// excerpt keeps the last LogTailLines lines, at most LogMaxBytes of them, as
// plain text, and says whether anything was left out.
func excerpt(data []byte) (string, bool) {
	text := strings.ToValidUTF8(string(data), "�")
	text = terminalControl.ReplaceAllString(text, "")
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	truncated := len(lines) > LogTailLines
	if truncated {
		lines = lines[len(lines)-LogTailLines:]
	}
	size := 0
	for i, line := range slices.Backward(lines) {
		size += len(line)
		if size > LogMaxBytes {
			return strings.Join(lines[i+1:], ""), true
		}
	}
	return strings.Join(lines, ""), truncated
}

// events lists what Kubernetes still keeps about one object, newest first.
func events(ctx context.Context, c client.Client, namespace string, uid types.UID) *consolev1.EventList {
	list := &corev1.EventList{}
	err := c.List(ctx, list, client.InNamespace(namespace), client.MatchingFields{"involvedObject.uid": string(uid)})
	out := &consolev1.EventList{}
	if out.Evidence, out.Problem = evidenceOf(err, "the events"); err != nil {
		return out
	}
	slices.SortFunc(list.Items, func(a, b corev1.Event) int {
		return cmp.Compare(lastSeen(&b).UnixNano(), lastSeen(&a).UnixNano())
	})
	for i := range list.Items[:min(len(list.Items), eventsPerObject)] {
		event := &list.Items[i]
		reporter := event.ReportingController
		if reporter == "" {
			reporter = event.Source.Component
		}
		last := lastSeen(event)
		out.Events = append(out.Events, &consolev1.KubeEvent{
			Type: event.Type, Reason: event.Reason, Message: event.Message, Count: max(event.Count, 1),
			FirstSeen: stamp(&event.FirstTimestamp), LastSeen: stamp(&last), Reporter: reporter,
		})
	}
	return out
}

// lastSeen is when an event last happened, whichever API wrote it.
func lastSeen(event *corev1.Event) metav1.Time {
	switch {
	case event.Series != nil && !event.Series.LastObservedTime.IsZero():
		return metav1.NewTime(event.Series.LastObservedTime.Time)
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp
	case !event.EventTime.IsZero():
		return metav1.NewTime(event.EventTime.Time)
	default:
		return event.CreationTimestamp
	}
}

// evidenceOf says whether a read showed its evidence, and why not.
func evidenceOf(err error, what string) (consolev1.Evidence, string) {
	var status apierrors.APIStatus
	switch {
	case err == nil:
		return consolev1.Evidence_EVIDENCE_SHOWN, ""
	case apierrors.IsForbidden(err) && errors.As(err, &status):
		return consolev1.Evidence_EVIDENCE_NOT_GRANTED, status.Status().Message
	case apierrors.IsNotFound(err) || apierrors.IsGone(err):
		return consolev1.Evidence_EVIDENCE_NOT_RETAINED, "Kubernetes no longer keeps " + what + "."
	case errors.As(err, &status):
		return consolev1.Evidence_EVIDENCE_UNREADABLE, status.Status().Message
	default:
		return consolev1.Evidence_EVIDENCE_UNREADABLE, what + " could not be read."
	}
}

func stamp(t *metav1.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(t.Time)
}
