/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// conditionReconcileError is set while the last reconcile failed and removed by the next one that succeeds.
	conditionReconcileError = "ReconcileError"
	// reasonReconcileError is the reason of the ReconcileError condition.
	reasonReconcileError = "LastReconcileCycleFailed"
	// legacyConditionReconcileSuccess was written on every reconcile by operator versions before 0.7.0; it is removed.
	legacyConditionReconcileSuccess = "ReconcileSuccess"
	// eventReasonProcessingError is the reason of the Warning event emitted when a reconcile fails.
	eventReasonProcessingError = "ProcessingError"
)

// manageSuccess saves the status if it differs from prev (the status as read at the start of the reconcile) and
// returns the result of a successful reconcile that requeues after requeueAfter (no requeue when zero).
// The write is an Update carrying the resourceVersion that was read, so a stale cache fails it with a Conflict.
func (r *MigrationReconciler) manageSuccess(ctx context.Context, migration *flywayv1alpha1.Migration, prev *flywayv1alpha1.MigrationStatus, requeueAfter time.Duration) (ctrl.Result, error) {
	meta.RemoveStatusCondition(&migration.Status.Conditions, conditionReconcileError)
	meta.RemoveStatusCondition(&migration.Status.Conditions, legacyConditionReconcileSuccess)
	if err := r.saveStatus(ctx, migration, prev); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// manageError emits a ProcessingError Warning event, records the error in the ReconcileError condition and returns
// the error so the controller requeues with backoff.
func (r *MigrationReconciler) manageError(ctx context.Context, migration *flywayv1alpha1.Migration, prev *flywayv1alpha1.MigrationStatus, issue error) (ctrl.Result, error) {
	if issue == nil {
		issue = errors.New("reconcile failed")
	}
	// a Conflict is a stale cache that the retry resolves, not a failure worth an event
	if !apierrors.IsConflict(issue) {
		r.Recorder.Event(migration, corev1.EventTypeWarning, eventReasonProcessingError, issue.Error())
	}
	meta.SetStatusCondition(&migration.Status.Conditions, metav1.Condition{
		Type:               conditionReconcileError,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: migration.Generation,
		Reason:             reasonReconcileError,
		Message:            issue.Error(),
	})
	if err := r.saveStatus(ctx, migration, prev); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, issue
}

// saveStatus writes the status when it differs from prev.
func (r *MigrationReconciler) saveStatus(ctx context.Context, migration *flywayv1alpha1.Migration, prev *flywayv1alpha1.MigrationStatus) error {
	if equality.Semantic.DeepEqual(prev, &migration.Status) {
		return nil
	}
	if err := r.Status().Update(ctx, migration); err != nil {
		log.FromContext(ctx).Error(err, "unable to update status")
		return err
	}
	return nil
}
