package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// A reconcile that changes nothing writes no status.
func TestReconcileWritesNoStatusWhenUnchanged(t *testing.T) {
	h := newStatusHarness(t, nil)
	h.reconcile() // creates the Job
	h.reconcile() // observes it running
	before := h.statusUpdates

	for range 3 {
		h.reconcile()
	}

	if h.statusUpdates != before {
		t.Errorf("status writes = %d after unchanged reconciles, want %d", h.statusUpdates, before)
	}
}

// A paused Migration gets no status at all: no ReconcileSuccess churn.
func TestReconcileOfPausedMigrationWritesNoStatus(t *testing.T) {
	h := newStatusHarness(t, nil)
	m := h.migration()
	m.Annotations = map[string]string{flywayv1alpha1.Prefix + "/paused": "true"}
	if err := h.c.Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}

	h.reconcile()

	if h.statusUpdates != 0 || len(h.migration().Status.Conditions) != 0 {
		t.Errorf("status writes = %d, conditions = %v, want none", h.statusUpdates, h.migration().Status.Conditions)
	}
}

// A failing reconcile returns the error, emits a ProcessingError Warning event and records ReconcileError; the next
// reconcile that succeeds removes it, and a repeated identical failure does not move lastTransitionTime or write status.
func TestReconcileErrorConditionAndEvent(t *testing.T) {
	failCreate := true
	h := newStatusHarness(t, nil, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if failCreate {
				return apierrors.NewForbidden(metav1.SchemeGroupVersion.WithResource("jobs").GroupResource(), "job", errors.New("denied"))
			}
			return c.Create(ctx, obj, opts...)
		},
	})
	req := reconcile.Request{NamespacedName: statusKey}

	if _, err := h.r.Reconcile(context.TODO(), req); err == nil {
		t.Fatal("reconcile returned no error, want the Job create error")
	}
	c := meta.FindStatusCondition(h.migration().Status.Conditions, conditionReconcileError)
	if c == nil || c.Status != metav1.ConditionTrue || c.Reason != reasonReconcileError || !strings.Contains(c.Message, "denied") || c.ObservedGeneration != 1 {
		t.Fatalf("ReconcileError condition = %+v", c)
	}
	var warned bool
	for _, e := range h.events() {
		warned = warned || (strings.HasPrefix(e, "Warning "+eventReasonProcessingError) && strings.Contains(e, "denied"))
	}
	if !warned {
		t.Error("no ProcessingError Warning event with the error message")
	}

	writes, transition := h.statusUpdates, c.LastTransitionTime
	if _, err := h.r.Reconcile(context.TODO(), req); err == nil {
		t.Fatal("second reconcile returned no error")
	}
	if got := meta.FindStatusCondition(h.migration().Status.Conditions, conditionReconcileError); !got.LastTransitionTime.Equal(&transition) || h.statusUpdates != writes {
		t.Errorf("repeated identical failure moved lastTransitionTime or wrote status (writes %d -> %d)", writes, h.statusUpdates)
	}

	failCreate = false
	h.reconcile()
	if meta.FindStatusCondition(h.migration().Status.Conditions, conditionReconcileError) != nil {
		t.Error("ReconcileError still set after a successful reconcile")
	}
}

// The ReconcileSuccess condition written by operator versions before 0.7.0 is removed.
func TestReconcileRemovesLegacyReconcileSuccess(t *testing.T) {
	h := newStatusHarness(t, nil)
	m := h.migration()
	meta.SetStatusCondition(&m.Status.Conditions, metav1.Condition{Type: legacyConditionReconcileSuccess, Status: metav1.ConditionTrue, Reason: "LastReconcileCycleSucceded"})
	if err := h.c.Status().Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}

	got := h.reconcile()

	if meta.FindStatusCondition(got.Status.Conditions, legacyConditionReconcileSuccess) != nil {
		t.Errorf("legacy ReconcileSuccess condition kept: %v", got.Status.Conditions)
	}
}
