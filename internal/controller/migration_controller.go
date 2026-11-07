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
	"fmt"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	sqlVolumeName    = "sql"
	clientContextKey = "client"
	deletionRequeue  = 2 * time.Second
)

// MigrationReconciler reconciles a Migration object
type MigrationReconciler struct {
	util.ReconcilerBase
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=flyway.davidkarlsen.com,resources=migrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=flyway.davidkarlsen.com,resources=migrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=flyway.davidkarlsen.com,resources=migrations/finalizers,verbs=update

// Reconcile requested migration by creating a Job to run flyway.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *MigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// nolint:staticcheck // SA1029 ignore this!
	ctx = context.WithValue(ctx, clientContextKey, r.GetClient())
	logger := log.FromContext(ctx).WithValues("migration", req.NamespacedName)

	migration := &flywayv1alpha1.Migration{}

	if err := r.Get(ctx, req.NamespacedName, migration); err != nil {
		if apierrors.IsNotFound(err) {
			deleteMetrics(req.NamespacedName)
		}
		logger.Error(err, err.Error())
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	prev := migration.Status.DeepCopy()

	if util.IsBeingDeleted(migration) {
		deleteMetrics(req.NamespacedName)
		logger.Info("Migration deleted, returning")
		r.GetRecorder().Event(migration, corev1.EventTypeWarning, "Deleting", fmt.Sprintf("Migration deleted: %s", req.NamespacedName))
		return r.ManageSuccess(ctx, migration)
	}
	defer recordStatus(migration)

	valid, err := r.IsValid(migration)
	if !valid || err != nil {
		return r.ManageError(ctx, migration, err)
	}

	if migration.IsPaused() {
		logger.Info("Migration is paused - not creating flyway migration job.")
		return r.ManageSuccess(ctx, migration)
	}

	existingJob, err := r.getExistingJob(ctx, migration)
	if err != nil {
		return r.ManageError(ctx, migration, err)
	}

	if existingJob == nil {
		return r.submitMigrationJob(ctx, migration)
	}

	if existingJob.DeletionTimestamp != nil {
		logger.Info("Previous job is being deleted, requeueing", "job", existingJob.Name)
		return r.ManageSuccessWithRequeue(ctx, migration, deletionRequeue)
	}

	state, failure := jobState(existingJob)

	if !jobIsCurrent(existingJob, migration) {
		if state == jobRunning {
			setConditions(migration, flywayv1alpha1.ReasonPreviousJobRunning,
				fmt.Sprintf("Waiting for job of generation %s to finish", existingJob.Annotations[flywayv1alpha1.Generation]),
				metav1.ConditionTrue, metav1.ConditionFalse)
			migration.Status.Attempts = 0
			return r.ManageSuccess(ctx, migration)
		}
		if state == jobSucceeded {
			before := migration.Status
			if !setLastApplied(migration, existingJob) {
				r.warnLastAppliedUnknown(ctx, migration, existingJob)
			} else if before.LastAppliedImageRef != migration.Status.LastAppliedImageRef ||
				before.LastAppliedGeneration != migration.Status.LastAppliedGeneration {
				// persist before the Job, the only record of this success, is deleted
				if err := r.GetClient().Status().Update(ctx, migration); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
		setConditions(migration, flywayv1alpha1.ReasonPreviousJobReplaced,
			fmt.Sprintf("Replacing job of generation %s", existingJob.Annotations[flywayv1alpha1.Generation]),
			metav1.ConditionTrue, metav1.ConditionFalse)
		migration.Status.Attempts = 0
		return r.deleteJob(ctx, migration, existingJob)
	}

	attempt := jobAttempt(existingJob)
	retryLimit := migration.GetRetryLimit()
	migration.Status.Attempts = attempt

	switch state {
	case jobRunning:
		setConditions(migration, flywayv1alpha1.ReasonJobRunning,
			fmt.Sprintf("Job %s attempt %d of %d is running", existingJob.Name, attempt, retryLimit+1),
			metav1.ConditionTrue, metav1.ConditionFalse)
		return r.ManageSuccess(ctx, migration)

	case jobSucceeded:
		recorded := setLastApplied(migration, existingJob)
		if !meta.IsStatusConditionTrue(migration.Status.Conditions, flywayv1alpha1.ConditionApplied) {
			logger.Info("Migration succeeded")
			r.GetRecorder().Event(migration, corev1.EventTypeNormal, "Succeeded",
				fmt.Sprintf("Migration Succeeded: %s, source: %s", req.NamespacedName, jobSourceImage(existingJob)))
			if !recorded {
				r.warnLastAppliedUnknown(ctx, migration, existingJob)
			}
		}
		setApplied(migration, fmt.Sprintf("Job %s succeeded on attempt %d", existingJob.Name, attempt))
		res, err := r.ManageSuccess(ctx, migration)
		if err == nil && outcomeIsNew(prev, migration.Generation, attempt, flywayv1alpha1.ReasonJobSucceeded) {
			recordOutcome(migration, existingJob, resultSucceeded)
		}
		return res, err

	default: // jobFailed
		if attempt > retryLimit {
			if !meta.IsStatusConditionTrue(migration.Status.Conditions, flywayv1alpha1.ConditionFailed) {
				r.GetRecorder().Event(migration, corev1.EventTypeWarning, "Failed",
					fmt.Sprintf("Migration failed after %d attempts: %s", attempt, failure))
			}
			setFailed(migration, fmt.Sprintf("Job %s failed on attempt %d of %d: %s", existingJob.Name, attempt, retryLimit+1, failure))
			res, err := r.ManageSuccess(ctx, migration)
			if err == nil && outcomeIsNew(prev, migration.Generation, attempt, flywayv1alpha1.ReasonRetryLimitExceeded) {
				recordOutcome(migration, existingJob, resultFailed)
			}
			return res, err
		}
		setConditions(migration, flywayv1alpha1.ReasonRetrying,
			fmt.Sprintf("Job %s failed on attempt %d of %d, retrying: %s", existingJob.Name, attempt, retryLimit+1, failure),
			metav1.ConditionTrue, metav1.ConditionFalse)
		res, err := r.deleteJob(ctx, migration, existingJob)
		if err == nil && outcomeIsNew(prev, migration.Generation, attempt, flywayv1alpha1.ReasonRetrying) {
			recordOutcome(migration, existingJob, resultRetrying)
		}
		return res, err
	}
}

// warnLastAppliedUnknown reports a succeeded Job whose source image or generation could not be read.
func (r *MigrationReconciler) warnLastAppliedUnknown(ctx context.Context, migration *flywayv1alpha1.Migration, job *batchv1.Job) {
	log.FromContext(ctx).Info("Succeeded job has no readable source image or generation, keeping last applied image", "job", job.Name)
	r.GetRecorder().Event(migration, corev1.EventTypeWarning, "LastAppliedUnknown",
		fmt.Sprintf("Job %s succeeded but its source image or generation could not be read; status.lastAppliedImageRef not updated", job.Name))
}

// IsValid does validation of the CR
func (r *MigrationReconciler) IsValid(obj metav1.Object) (bool, error) {
	_, ok := obj.(*flywayv1alpha1.Migration)
	if !ok {
		return false, errors.New("failed cast")
	}

	return ok, nil
}

func (r *MigrationReconciler) getExistingJob(ctx context.Context, migration *flywayv1alpha1.Migration) (*batchv1.Job, error) {
	// look for any current migration job and check state
	existingJob := &batchv1.Job{}
	err := r.GetClient().Get(ctx, client.ObjectKeyFromObject(migration), existingJob)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}

	return existingJob, err
}

// submitMigrationJob creates the Job for the next attempt of the current generation.
func (r *MigrationReconciler) submitMigrationJob(ctx context.Context, migration *flywayv1alpha1.Migration) (reconcile.Result, error) {
	logger := log.FromContext(ctx)

	attempt := int32(1)
	if migration.Status.ObservedGeneration == migration.Generation {
		attempt = migration.Status.Attempts + 1
	}

	job := createJobSpec(migration, attempt)
	logger.Info("Creating job", "job", job.Name, "attempt", attempt)
	if err := controllerutil.SetControllerReference(migration, job, r.GetScheme()); err != nil {
		return r.ManageError(ctx, migration, err)
	}
	if err := r.GetClient().Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) { // the cache has not seen our Job yet; status already describes it
			return ctrl.Result{RequeueAfter: deletionRequeue}, nil
		}
		return r.ManageError(ctx, migration, err)
	}

	migration.Status.Attempts = attempt
	setConditions(migration, flywayv1alpha1.ReasonJobCreated,
		fmt.Sprintf("Created job %s attempt %d of %d", job.Name, attempt, migration.GetRetryLimit()+1),
		metav1.ConditionTrue, metav1.ConditionFalse)
	return r.ManageSuccess(ctx, migration)
}

// deleteJob deletes a finished Job; the next reconcile, triggered by the deletion, creates its successor.
func (r *MigrationReconciler) deleteJob(ctx context.Context, migration *flywayv1alpha1.Migration, job *batchv1.Job) (reconcile.Result, error) {
	opts := metav1.DeletePropagationBackground
	err := r.GetClient().Delete(ctx, job, &client.DeleteOptions{
		PropagationPolicy: &opts,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return r.ManageError(ctx, migration, err)
	}

	return r.ManageSuccessWithRequeue(ctx, migration, deletionRequeue)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flywayv1alpha1.Migration{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
