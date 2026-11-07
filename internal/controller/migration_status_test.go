package controller

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var statusKey = types.NamespacedName{Namespace: "ns", Name: "status-migration"}

type statusHarness struct {
	t *testing.T
	c client.Client
	r *MigrationReconciler
	// failStatusUpdates makes that many next status updates fail with a conflict.
	failStatusUpdates int
	// failJobDeletes makes that many next Job deletes fail with a conflict.
	failJobDeletes int
}

func newStatusHarness(t *testing.T, retryLimit *int32, extra ...interceptor.Funcs) *statusHarness {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := flywayv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: statusKey.Name, Namespace: statusKey.Namespace, Generation: 1},
		Spec: flywayv1alpha1.MigrationSpec{
			Database: flywayv1alpha1.Database{
				Username:    "someUser",
				Credentials: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "pw"}, Key: "password"},
				JdbcUrl:     "jdbc:db2://somehost:50000/somedb",
			},
			MigrationSource: flywayv1alpha1.MigrationSource{ImageRef: "somereg.io/someimage:1"},
			RetryLimit:      retryLimit,
		},
	}
	h := &statusHarness{t: t}
	funcs := interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, isJob := obj.(*batchv1.Job); isJob && h.failJobDeletes > 0 {
				h.failJobDeletes--
				return apierrors.NewConflict(schema.GroupResource{Group: "batch", Resource: "jobs"}, obj.GetName(), errors.New("injected"))
			}
			return c.Delete(ctx, obj, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if h.failStatusUpdates > 0 {
				h.failStatusUpdates--
				return apierrors.NewConflict(schema.GroupResource{Resource: "migrations"}, obj.GetName(), errors.New("injected"))
			}
			return c.SubResource(sub).Update(ctx, obj, opts...)
		},
	}
	for _, f := range extra {
		if f.Create != nil {
			funcs.Create = f.Create
		}
	}
	h.c = fake.NewClientBuilder().WithScheme(s).WithObjects(migration).WithStatusSubresource(migration).
		WithInterceptorFuncs(funcs).Build()
	h.r = &MigrationReconciler{
		ReconcilerBase: util.NewReconcilerBase(h.c, s, nil, record.NewFakeRecorder(100), nil),
		Client:         h.c,
		Scheme:         s,
	}
	return h
}

// events drains and returns the events recorded so far.
func (h *statusHarness) events() []string {
	var out []string
	ch := h.r.GetRecorder().(*record.FakeRecorder).Events
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func countContaining(events []string, sub string) int {
	n := 0
	for _, e := range events {
		if strings.Contains(e, sub) {
			n++
		}
	}
	return n
}

func (h *statusHarness) reconcile() *flywayv1alpha1.Migration {
	h.t.Helper()
	if _, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey}); err != nil {
		h.t.Fatalf("reconcile: %v", err)
	}
	return h.migration()
}

func (h *statusHarness) migration() *flywayv1alpha1.Migration {
	h.t.Helper()
	m := &flywayv1alpha1.Migration{}
	if err := h.c.Get(context.TODO(), statusKey, m); err != nil {
		h.t.Fatal(err)
	}
	return m
}

// job returns the Job, or nil when it does not exist.
func (h *statusHarness) job() *batchv1.Job {
	h.t.Helper()
	j := &batchv1.Job{}
	err := h.c.Get(context.TODO(), statusKey, j)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return j
}

// finishJob marks the Job terminal the way the Job controller does.
func (h *statusHarness) finishJob(condType batchv1.JobConditionType, failedPods int32) {
	h.t.Helper()
	j := h.job()
	if j == nil {
		h.t.Fatal("no job to finish")
	}
	j.Status.Failed = failedPods
	cond := batchv1.JobCondition{Type: condType, Status: corev1.ConditionTrue}
	if condType == batchv1.JobFailed {
		// the Job controller sets FailureTarget before Failed
		j.Status.Conditions = append(j.Status.Conditions, batchv1.JobCondition{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue})
		cond.Reason = "BackoffLimitExceeded"
		cond.Message = "Job has reached the specified backoff limit"
	} else {
		j.Status.Succeeded = 1
	}
	j.Status.Conditions = append(j.Status.Conditions, cond)
	if err := h.c.Status().Update(context.TODO(), j); err != nil {
		h.t.Fatal(err)
	}
}

func (h *statusHarness) expect(m *flywayv1alpha1.Migration, reason string, applied, progressing, failed metav1.ConditionStatus) {
	h.t.Helper()
	if m.Status.ObservedGeneration != m.Generation {
		h.t.Errorf("status.observedGeneration = %d, want %d", m.Status.ObservedGeneration, m.Generation)
	}
	for condType, want := range map[string]metav1.ConditionStatus{
		flywayv1alpha1.ConditionApplied:     applied,
		flywayv1alpha1.ConditionProgressing: progressing,
		flywayv1alpha1.ConditionFailed:      failed,
	} {
		c := meta.FindStatusCondition(m.Status.Conditions, condType)
		if c == nil {
			h.t.Errorf("condition %s missing", condType)
			continue
		}
		if c.Status != want || c.Reason != reason || c.ObservedGeneration != m.Generation {
			h.t.Errorf("condition %s = %s/%s gen %d, want %s/%s gen %d (message %q)",
				condType, c.Status, c.Reason, c.ObservedGeneration, want, reason, m.Generation, c.Message)
		}
	}
}

func TestStatusRunningIsProgressing(t *testing.T) {
	h := newStatusHarness(t, nil)

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobCreated, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	if m.Status.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", m.Status.Attempts)
	}

	m = h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobRunning, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
}

func TestStatusSucceededIsApplied(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()
	// a pod failed once and the Job's own backoff retry succeeded: still a success
	h.finishJob(batchv1.JobComplete, 1)
	uid := h.job().UID

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
	if j := h.job(); j == nil || j.UID != uid {
		t.Error("succeeded job was replaced")
	}
}

func TestStatusFailedRetriesUntilLimitThenStops(t *testing.T) {
	h := newStatusHarness(t, ptr.To[int32](1))
	h.reconcile()

	// attempt 1 fails, under the limit: the Job is deleted and re-created as attempt 2
	h.finishJob(batchv1.JobFailed, 3)
	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonRetrying, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	if h.job() != nil {
		t.Fatal("failed job under the limit was not deleted")
	}
	m = h.reconcile()
	j := h.job()
	if j == nil || j.Annotations[flywayv1alpha1.Attempt] != "2" || m.Status.Attempts != 2 {
		t.Fatalf("retry job not created as attempt 2: job=%v attempts=%d", j, m.Status.Attempts)
	}

	// attempt 2 fails, at the limit: Failed=True and the Job is kept, never re-created
	h.finishJob(batchv1.JobFailed, 3)
	uid := h.job().UID
	for range 3 {
		m = h.reconcile()
		h.expect(m, flywayv1alpha1.ReasonRetryLimitExceeded, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
		if j := h.job(); j == nil || j.UID != uid {
			t.Fatal("failed job at the limit was deleted or re-created")
		}
	}
	if c := meta.FindStatusCondition(m.Status.Conditions, flywayv1alpha1.ConditionFailed); c.Message == "" {
		t.Error("Failed condition carries no message from the Job")
	}
}

func TestStatusSpecChangeReruns(t *testing.T) {
	h := newStatusHarness(t, ptr.To[int32](0))
	h.reconcile()
	h.finishJob(batchv1.JobFailed, 3)
	h.expect(h.reconcile(), flywayv1alpha1.ReasonRetryLimitExceeded, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)

	m := h.migration()
	m.Spec.MigrationSource.ImageRef = "somereg.io/someimage:2"
	m.Generation = 2
	if err := h.c.Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}

	m = h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	if h.job() != nil {
		t.Fatal("job of the previous generation was not deleted")
	}

	m = h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobCreated, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	j := h.job()
	if j == nil || j.Annotations[flywayv1alpha1.Generation] != "2" || j.Annotations[flywayv1alpha1.Attempt] != "1" || m.Status.Attempts != 1 {
		t.Fatalf("new generation did not start at attempt 1: job=%v attempts=%d", j, m.Status.Attempts)
	}

	h.finishJob(batchv1.JobComplete, 0)
	h.expect(h.reconcile(), flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
}

func TestStatusCreateConflictDoesNotCountAnAttempt(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()

	// a stale read: status says no Job was started yet, but the Job exists
	m := h.migration()
	m.Status.Attempts = 0
	if err := h.c.Status().Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	if err := h.r.Create(context.TODO(), createJobSpec(m, 1)); !apierrors.IsAlreadyExists(err) {
		t.Fatalf("expected the Job to exist, got %v", err)
	}
	if _, err := h.r.submitMigrationJob(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	if got := h.migration().Status.Attempts; got != 0 {
		t.Errorf("attempts = %d after a create that found the Job existing, want 0", got)
	}
}

func (h *statusHarness) expectLastApplied(m *flywayv1alpha1.Migration, image string, generation int64) {
	h.t.Helper()
	if m.Status.LastAppliedImageRef != image || m.Status.LastAppliedGeneration != generation {
		h.t.Errorf("lastApplied = %q gen %d, want %q gen %d",
			m.Status.LastAppliedImageRef, m.Status.LastAppliedGeneration, image, generation)
	}
}

func (h *statusHarness) updateImage(image string, generation int64) {
	h.t.Helper()
	m := h.migration()
	m.Spec.MigrationSource.ImageRef = image
	m.Generation = generation
	if err := h.c.Update(context.TODO(), m); err != nil {
		h.t.Fatal(err)
	}
}

func TestStatusLastAppliedImage(t *testing.T) {
	h := newStatusHarness(t, ptr.To[int32](0))

	m := h.reconcile()
	h.expectLastApplied(m, "", 0)
	h.finishJob(batchv1.JobComplete, 0)
	h.expectLastApplied(h.reconcile(), "somereg.io/someimage:1", 1)

	// a run of a new image is in progress, then fails: the last applied image is kept
	h.updateImage("somereg.io/someimage:2", 2)
	h.expectLastApplied(h.reconcile(), "somereg.io/someimage:1", 1) // previous job replaced
	h.expectLastApplied(h.reconcile(), "somereg.io/someimage:1", 1) // job created
	h.expectLastApplied(h.reconcile(), "somereg.io/someimage:1", 1) // job running
	h.finishJob(batchv1.JobFailed, 3)
	m = h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonRetryLimitExceeded, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	h.expectLastApplied(m, "somereg.io/someimage:1", 1)

	// a new image succeeds: the last applied image follows
	h.updateImage("somereg.io/someimage:3", 3)
	h.reconcile()
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)
	m = h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
	h.expectLastApplied(m, "somereg.io/someimage:3", 3)
}

func TestStatusLastAppliedImageComesFromJob(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)

	// the spec image differs from the Job's without a generation change: the Job's image is reported.
	// The API server bumps the generation on any spec change; only the fake client allows this state,
	// which stands in for a stale read.
	m := h.migration()
	m.Spec.MigrationSource.ImageRef = "somereg.io/other:9"
	if err := h.c.Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	h.expectLastApplied(h.reconcile(), "somereg.io/someimage:1", 1)
	if n := countContaining(h.events(), "Succeeded Migration Succeeded: ns/status-migration, source: somereg.io/someimage:1"); n != 1 {
		t.Errorf("Succeeded events naming the Job's image = %d, want 1", n)
	}
}

func TestStatusLastAppliedFromPreviousGenerationJob(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()

	// the spec moves on while the gen-1 Job runs; that Job then succeeds
	h.updateImage("somereg.io/someimage:2", 2)
	h.expect(h.reconcile(), flywayv1alpha1.ReasonPreviousJobRunning, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	h.finishJob(batchv1.JobComplete, 0)

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	h.expectLastApplied(m, "somereg.io/someimage:1", 1)
}

func TestStatusLastAppliedKeptWhenJobHasNoSourceImage(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)
	h.expectLastApplied(h.reconcile(), "somereg.io/someimage:1", 1)

	h.updateImage("somereg.io/someimage:2", 2)
	h.reconcile() // previous job replaced
	h.reconcile() // job created
	j := h.job()
	j.Spec.Template.Spec.Volumes[0].Name = "something-else"
	if err := h.c.Update(context.TODO(), j); err != nil {
		t.Fatal(err)
	}
	h.finishJob(batchv1.JobComplete, 0)

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
	h.expectLastApplied(m, "somereg.io/someimage:1", 1)

	if n := countContaining(h.events(), "LastAppliedUnknown"); n != 1 {
		t.Errorf("LastAppliedUnknown events = %d, want 1", n)
	}
}

func TestStatusDeadlineExceededRetriesThenFails(t *testing.T) {
	h := newStatusHarness(t, ptr.To[int32](1))
	m := h.migration()
	m.Spec.ActiveDeadline = &metav1.Duration{Duration: time.Minute}
	if err := h.c.Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	h.reconcile()

	deadlineExceeded := func() {
		t.Helper()
		if got := ptr.Deref(h.job().Spec.ActiveDeadlineSeconds, 0); got != 60 {
			t.Fatalf("activeDeadlineSeconds = %d, want 60", got)
		}
		h.failJobDeadlineExceeded()
	}
	const (
		timedOut = "timed out (activeDeadline 1m0s)"
		want     = "DeadlineExceeded: Job was active longer than specified deadline"
	)
	messageHas := func(m *flywayv1alpha1.Migration, condType string) {
		t.Helper()
		c := meta.FindStatusCondition(m.Status.Conditions, condType)
		if !strings.Contains(c.Message, timedOut) || !strings.Contains(c.Message, want) {
			t.Errorf("%s message %q does not contain %q and %q", condType, c.Message, timedOut, want)
		}
	}

	// a timeout under the limit is retried like any other failure
	deadlineExceeded()
	m = h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonRetrying, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	messageHas(m, flywayv1alpha1.ConditionProgressing)
	h.reconcile() // attempt 2 created

	// a timeout at the limit fails the Migration with reason TimedOut, and a TimedOut event instead of Failed
	deadlineExceeded()
	for range 3 {
		m = h.reconcile()
		h.expect(m, flywayv1alpha1.ReasonTimedOut, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	}
	messageHas(m, flywayv1alpha1.ConditionFailed)
	events := h.events()
	if n := countContaining(events, "Warning TimedOut Migration "+timedOut+" on attempt 2 of 2: DeadlineExceeded"); n != 1 {
		t.Errorf("TimedOut events = %d, want 1 (events: %q)", n, events)
	}
	if n := countContaining(events, "Warning Failed"); n != 0 {
		t.Errorf("Failed events = %d, want 0", n)
	}
}

// failJobDeadlineExceeded marks the Job failed the way the Job controller does when activeDeadlineSeconds is reached.
func (h *statusHarness) failJobDeadlineExceeded() {
	h.t.Helper()
	j := h.job()
	if j == nil {
		h.t.Fatal("no job to fail")
	}
	j.Status.Conditions = append(j.Status.Conditions, batchv1.JobCondition{
		Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
		Reason: batchv1.JobReasonDeadlineExceeded, Message: "Job was active longer than specified deadline",
	})
	if err := h.c.Status().Update(context.TODO(), j); err != nil {
		h.t.Fatal(err)
	}
}

func TestStatusLastAppliedFromPreviousGenerationJobSurvivesStatusConflict(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()
	h.updateImage("somereg.io/someimage:2", 2)
	h.reconcile() // previous job running
	h.finishJob(batchv1.JobComplete, 0)

	// the status write recording the previous generation's success fails: the Job must not be deleted
	h.failStatusUpdates = 1
	if _, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey}); err == nil {
		t.Fatal("expected the injected status conflict to be returned")
	}
	if h.job() == nil {
		t.Fatal("job deleted although its success was not persisted")
	}
	h.expectLastApplied(h.migration(), "", 0)

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	h.expectLastApplied(m, "somereg.io/someimage:1", 1)
	if h.job() != nil {
		t.Error("job of the previous generation was not deleted after its success was persisted")
	}
	if n := countContaining(h.events(), "LastAppliedUnknown"); n != 0 {
		t.Errorf("LastAppliedUnknown events = %d, want 0", n)
	}
}

// dropImageVolumes mimics an API server with the ImageVolume feature gate off: it drops image volumes and their mounts.
var dropImageVolumes = interceptor.Funcs{
	Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if job, ok := obj.(*batchv1.Job); ok {
			spec := &job.Spec.Template.Spec
			spec.Volumes = slices.DeleteFunc(spec.Volumes, func(v corev1.Volume) bool { return v.Image != nil })
			for i := range spec.Containers {
				spec.Containers[i].VolumeMounts = slices.DeleteFunc(spec.Containers[i].VolumeMounts,
					func(m corev1.VolumeMount) bool { return m.Name == sqlVolumeName })
			}
		}
		return c.Create(ctx, obj, opts...)
	},
}

func TestStatusDroppedImageVolumeFails(t *testing.T) {
	h := newStatusHarness(t, nil, dropImageVolumes)

	for range 3 {
		m := h.reconcile()
		h.expect(m, flywayv1alpha1.ReasonImageVolumeUnsupported, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
		if h.job() != nil {
			t.Fatal("job without the image volume was not deleted")
		}
	}
	recorder := h.r.GetRecorder().(*record.FakeRecorder)
	events := 0
	for len(recorder.Events) > 0 {
		if e := <-recorder.Events; strings.Contains(e, flywayv1alpha1.ReasonImageVolumeUnsupported) {
			events++
		}
	}
	if events != 1 {
		t.Errorf("got %d %s events, want 1 (no re-creation loop)", events, flywayv1alpha1.ReasonImageVolumeUnsupported)
	}

	// a spec change tries again
	m := h.migration()
	m.Spec.MigrationSource.ImageRef = "somereg.io/someimage:2"
	m.Generation = 2
	if err := h.c.Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	h.expect(h.reconcile(), flywayv1alpha1.ReasonImageVolumeUnsupported, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	if len(recorder.Events) != 1 {
		t.Errorf("spec change did not try again: %d new events", len(recorder.Events))
	}
}

func TestStatusStaleCachedJobWithoutImageVolumeKeepsFailed(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile()
	m := h.migration()
	setAll(m, flywayv1alpha1.ReasonImageVolumeUnsupported, "dropped", metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	if err := h.c.Status().Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	j := h.job()
	j.Spec.Template.Spec.Volumes = nil
	if err := h.c.Update(context.TODO(), j); err != nil {
		t.Fatal(err)
	}

	h.expect(h.reconcile(), flywayv1alpha1.ReasonImageVolumeUnsupported, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	if h.job() != nil {
		t.Fatal("cached job without the image volume was not deleted")
	}
}

// A current-generation Job created by an operator before 0.6.0 has a copy-sql init container and an emptyDir named
// sql instead of the image volume. It is still followed to its outcome, not rejected as ImageVolumeUnsupported.
func TestStatusLegacyCopySQLJobIsFollowed(t *testing.T) {
	for _, tt := range []struct {
		name        string
		succeeded   bool
		reason      string
		applied     metav1.ConditionStatus
		progressing metav1.ConditionStatus
	}{
		{name: "succeeded", succeeded: true, reason: flywayv1alpha1.ReasonJobSucceeded, applied: metav1.ConditionTrue, progressing: metav1.ConditionFalse},
		{name: "running", reason: flywayv1alpha1.ReasonJobRunning, applied: metav1.ConditionFalse, progressing: metav1.ConditionTrue},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newStatusHarness(t, nil)
			sqlMount := []corev1.VolumeMount{{Name: "sql", MountPath: "/flyway/sql"}}
			legacy := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: statusKey.Name, Namespace: statusKey.Namespace,
					Annotations: map[string]string{flywayv1alpha1.Generation: "1", flywayv1alpha1.Attempt: "1"},
				},
				Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: copySQLContainerName, Image: "somereg.io/someimage:1", VolumeMounts: sqlMount}},
					Containers:     []corev1.Container{{Name: "flyway", Image: defaultFlywayImage, VolumeMounts: sqlMount}},
					Volumes:        []corev1.Volume{{Name: "sql", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
				}}},
			}
			if err := h.c.Create(context.TODO(), legacy); err != nil {
				t.Fatal(err)
			}
			if tt.succeeded {
				h.finishJob(batchv1.JobComplete, 0)
			}
			uid := h.job().UID

			for range 2 {
				h.expect(h.reconcile(), tt.reason, tt.applied, tt.progressing, metav1.ConditionFalse)
			}
			if j := h.job(); j == nil || j.UID != uid {
				t.Error("legacy job was deleted or replaced")
			}
			if n := countContaining(h.events(), flywayv1alpha1.ReasonImageVolumeUnsupported); n != 0 {
				t.Errorf("%s events = %d, want 0", flywayv1alpha1.ReasonImageVolumeUnsupported, n)
			}
			if m := h.migration(); tt.succeeded && m.Status.LastAppliedImageRef != "somereg.io/someimage:1" {
				t.Errorf("lastAppliedImageRef = %q, want the copy-sql image", m.Status.LastAppliedImageRef)
			}
		})
	}
}

// holdJob adds a finalizer to the Job, so a delete leaves it terminating.
func (h *statusHarness) holdJob() {
	h.t.Helper()
	j := h.job()
	j.Finalizers = append(j.Finalizers, "test/hold")
	if err := h.c.Update(context.TODO(), j); err != nil {
		h.t.Fatal(err)
	}
}

// releaseJob removes the finalizers, so a terminating Job goes away.
func (h *statusHarness) releaseJob() {
	h.t.Helper()
	j := h.job()
	j.Finalizers = nil
	if err := h.c.Update(context.TODO(), j); err != nil {
		h.t.Fatal(err)
	}
}

// reconcileExpectingConflict runs a reconcile whose status write, after the Job delete, fails with a conflict.
func (h *statusHarness) reconcileExpectingConflict() {
	h.t.Helper()
	h.failStatusUpdates = 1
	if _, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey}); !apierrors.IsConflict(err) {
		h.t.Fatalf("reconcile err = %v, want a conflict", err)
	}
	if j := h.job(); j == nil || j.DeletionTimestamp == nil {
		h.t.Fatal("job is not terminating after the reconcile")
	}
}

// The reconcile that deletes a failed Job can lose its Retrying status write. While the Job terminates, later
// reconciles report Retrying again, count the attempt once, and do not create a successor.
func TestStatusRetryingSurvivesLostWriteWhileJobTerminates(t *testing.T) {
	h := newMetricsHarness(t, nil)
	h.reconcile()
	h.reconcile()
	h.holdJob()
	h.finishJob(batchv1.JobFailed, 3)
	uid := h.job().UID

	h.reconcileExpectingConflict()
	h.expect(h.migration(), flywayv1alpha1.ReasonJobRunning, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)

	for range 2 {
		m := h.reconcile()
		h.expect(m, flywayv1alpha1.ReasonRetrying, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
		if m.Status.Attempts != 1 {
			t.Errorf("attempts = %d, want 1", m.Status.Attempts)
		}
		if j := h.job(); j == nil || j.UID != uid {
			t.Fatal("terminating job was replaced")
		}
	}
	h.expectRuns(0, 1, 0)

	h.releaseJob()
	if h.job() != nil {
		t.Fatal("released job still exists")
	}
	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonJobCreated, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	if j := h.job(); j == nil || j.Annotations[flywayv1alpha1.Attempt] != "2" {
		t.Errorf("successor = %+v, want attempt 2", j)
	}
	h.expectRuns(0, 1, 0)
}

// The same for a Job of an older generation: PreviousJobReplaced is reported again while it terminates.
func TestStatusReplacedSurvivesLostWriteWhileJobTerminates(t *testing.T) {
	for _, tt := range []struct {
		name   string
		finish func(h *statusHarness)
	}{
		{name: "succeeded", finish: func(h *statusHarness) { h.finishJob(batchv1.JobComplete, 0) }},
		{name: "failed", finish: func(h *statusHarness) { h.finishJob(batchv1.JobFailed, 3) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newStatusHarness(t, ptr.To[int32](0))
			h.reconcile()
			h.holdJob()
			tt.finish(h)
			h.reconcile() // records the outcome of generation 1
			uid := h.job().UID

			m := h.migration()
			m.Spec.MigrationSource.ImageRef = "somereg.io/someimage:2"
			m.Generation = 2
			if err := h.c.Update(context.TODO(), m); err != nil {
				t.Fatal(err)
			}

			h.reconcileExpectingConflict()
			for range 2 {
				m := h.reconcile()
				h.expect(m, flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
				if m.Status.Attempts != 0 {
					t.Errorf("attempts = %d, want 0", m.Status.Attempts)
				}
				if j := h.job(); j == nil || j.UID != uid {
					t.Fatal("terminating job was replaced")
				}
			}

			h.releaseJob()
			m = h.reconcile()
			h.expect(m, flywayv1alpha1.ReasonJobCreated, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
			if j := h.job(); j == nil || j.Annotations[flywayv1alpha1.Generation] != "2" || j.Annotations[flywayv1alpha1.Attempt] != "1" {
				t.Errorf("successor annotations = %v, want generation 2 attempt 1", j.Annotations)
			}
		})
	}
}
