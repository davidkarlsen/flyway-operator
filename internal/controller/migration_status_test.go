package controller

import (
	"context"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var statusKey = types.NamespacedName{Namespace: "ns", Name: "status-migration"}

type statusHarness struct {
	t *testing.T
	c client.Client
	r *MigrationReconciler
}

func newStatusHarness(t *testing.T, retryLimit *int32) *statusHarness {
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
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(migration).WithStatusSubresource(migration).Build()
	r := &MigrationReconciler{
		ReconcilerBase: util.NewReconcilerBase(c, s, nil, record.NewFakeRecorder(100), nil),
		Client:         c,
		Scheme:         s,
	}
	return &statusHarness{t: t, c: c, r: r}
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
