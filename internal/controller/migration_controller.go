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
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/crud"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	sqlVolumeName      = "sql"
	defaultFlywayImage = "ghcr.io/davidkarlsen/flyway-db2:9.22"
	envNameFlywayImage = "FLYWAY_IMAGE"

	clientContextKey = "client"
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

	if err := r.Client.Get(ctx, req.NamespacedName, migration); err != nil {
		logger.Error(err, err.Error())
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if util.IsBeingDeleted(migration) {
		logger.Info("Migration deleted, returning")
		r.GetRecorder().Event(migration, corev1.EventTypeWarning, "Deleting", fmt.Sprintf("Migration deleted: %s", req.NamespacedName))
		return r.ManageSuccess(ctx, migration)
	}

	valid, err := r.IsValid(migration)
	if !valid || err != nil {
		return r.ManageError(ctx, migration, err)
	}

	existingJob, err := r.getExistingJob(ctx, migration)
	if err != nil {
		return r.ManageError(ctx, migration, err)
	}

	newJob := createJobSpec(migration)

	if existingJob == nil { // no existing job - so submit one now
		return r.submitMigrationJob(ctx, migration, newJob)
	} else {
		if !isJobFinished(existingJob) {
			logger.Info("Job still running, returning for reconcile", "job", existingJob)
			return r.ManageSuccess(ctx, migration)
		}

		jobsAreEqual := jobsAreEqual(existingJob, newJob)

		if hasFailed(existingJob) || !jobsAreEqual {
			return r.submitMigrationJob(ctx, migration, newJob)
		}

		if hasSucceeded(existingJob) {
			if jobsAreEqual {
				logger.Info("Migration succeeded")
				r.GetRecorder().Event(migration, corev1.EventTypeNormal, "Succeeded", fmt.Sprintf("Migration Succeeded: %s", req.NamespacedName))
				return r.ManageSuccess(ctx, migration)
			} else { // migration has changed - submit new job
				return r.submitMigrationJob(ctx, migration, newJob)
			}
		}
	}

	err = fmt.Errorf("this is a bug and should not happen")
	return r.ManageError(ctx, migration, err)
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

func (r *MigrationReconciler) submitMigrationJob(ctx context.Context, migration *flywayv1alpha1.Migration, job *batchv1.Job) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	//err := crud.DeleteResourceIfExists(ctx, job)
	opts := metav1.DeletePropagationBackground
	err := r.GetClient().Delete(ctx, job, &client.DeleteOptions{
		PropagationPolicy: &opts,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return r.ManageError(ctx, migration, err)
	}

	logger.Info("Creating job", "job", job)
	err = crud.CreateResourceIfNotExists(ctx, migration, migration.Namespace, job)
	if err != nil {
		return r.ManageError(ctx, migration, err)
	}

	return r.ManageSuccess(ctx, migration)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flywayv1alpha1.Migration{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
