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
	"fmt"
	"github.com/caitlinelfring/go-env-default"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/crud"
	batchv1 "k8s.io/api/batch/v1"
	v12 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	sqlVolumeName         = "sql"
	defaultFlywayImage    = "ghcr.io/davidkarlsen/flyway-db2:9.22"
	env_name_flyway_image = "FLYWAY_IMAGE"
)

// MigrationReconciler reconciles a Migration object
type MigrationReconciler struct {
	util.ReconcilerBase
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=flyway.davidkarlsen.com,resources=migrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=flyway.davidkarlsen.com,resources=migrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=flyway.davidkarlsen.com,resources=migrations/finalizers,verbs=update

// Reconcile requested migration by creating a Job to run flyway.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *MigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = context.WithValue(ctx, "client", r.GetClient())
	logger := log.FromContext(ctx).WithValues("migration", req.NamespacedName)

	migration := &flywayv1alpha1.Migration{}

	if err := r.Client.Get(ctx, req.NamespacedName, migration); err != nil {
		logger.Error(err, err.Error())
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if util.IsBeingDeleted(migration) {
		logger.Info("Migration deleted, returning")
		r.GetRecorder().Event(migration, v12.EventTypeWarning, "Deleting", fmt.Sprintf("Migration deleted: %s", req.NamespacedName))
		return r.ManageSuccess(ctx, migration)
	}

	existingJob, err := r.getExistingJob(ctx, migration)
	if err != nil {
		return r.ManageError(ctx, migration, err)
	}

	if existingJob == nil { // no existing job - so submit one now
		err := r.submitMigrationJob(ctx, migration)
		if err != nil {
			return r.ManageError(ctx, migration, err)
		}
		return r.ManageSuccess(ctx, migration) //TODO: Should requeue
	}

	if !r.isJobFinished(existingJob) {
		logger.Info("Job still running, returning for reconcile", "job", existingJob)
		return r.ManageSuccessWithRequeue(ctx, migration, 3*time.Second)
	}

	if existingJob.Status.Failed > 0 {
		err := r.deleteExistingJob(ctx, existingJob)
		if err != nil {
			return r.ManageError(ctx, migration, err)
		}
		//TODO: should save onto status field of migration
		return r.ManageError(ctx, migration, fmt.Errorf("existing job failed - reattempting reconcilation"))
	}

	if existingJob.Status.Succeeded > 0 {
		logger.Info("Job succeeded - removing") //TODO - should match hash
		r.GetRecorder().Event(migration, v12.EventTypeNormal, "Succeeded", fmt.Sprintf("Migration Succeeded: %s", req.NamespacedName))

		err := r.deleteExistingJob(ctx, existingJob)
		if err != nil {
			return r.ManageError(ctx, migration, err)
		}
		return r.ManageSuccess(ctx, migration)
	}

	logger.Info("BUG - should not happen")
	return r.ManageSuccess(ctx, migration)
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

func (r *MigrationReconciler) submitMigrationJob(ctx context.Context, migration *flywayv1alpha1.Migration) error {
	job := r.createJobSpec(ctx, migration)
	logger := log.FromContext(ctx)
	logger.V(1).Info("Generated job", "job", fmt.Sprintf("%+v", job))

	return crud.CreateResourceIfNotExists(ctx, migration, migration.Namespace, &job)
}

func (r *MigrationReconciler) deleteExistingJob(ctx context.Context, existingJob *batchv1.Job) error {
	//_ = crud.DeleteResourceIfExists(ctx, existingJob)x
	opt := metav1.DeletePropagationForeground
	return r.Client.Delete(ctx, existingJob, &client.DeleteOptions{PropagationPolicy: &opt})
}

// from https://github.com/kubernetes/kubernetes/blob/v1.28.1/pkg/controller/job/utils.go
// IsJobFinished checks whether the given Job has finished execution.
// It does not discriminate between successful and failed terminations.
func (r *MigrationReconciler) isJobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == v12.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *MigrationReconciler) createJobSpec(ctx context.Context, migration *flywayv1alpha1.Migration) batchv1.Job {
	const targetPath = "/mnt/target/"

	return batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: batchv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      migration.Name,
			Namespace: migration.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: pointer.Int32(60 * 60),
			BackoffLimit:            pointer.Int32(2),
			Template: v12.PodTemplateSpec{
				Spec: v12.PodSpec{
					InitContainers: []v12.Container{
						{
							Name:            "copy-sql",
							Image:           migration.Spec.Migration.ImageRef,
							ImagePullPolicy: v12.PullAlways,
							Command:         []string{"sh", "-c"},
							Args:            []string{fmt.Sprintf("cd %s && cp -rp * %s", migration.Spec.Migration.SqlPath, targetPath)},
							VolumeMounts: []v12.VolumeMount{
								{
									Name:      sqlVolumeName,
									MountPath: targetPath,
								},
							},
						},
					},
					Containers: []v12.Container{
						{
							Name:            "flyway",
							Image:           env.GetDefault(env_name_flyway_image, defaultFlywayImage),
							ImagePullPolicy: v12.PullAlways,
							Args:            []string{"info", "migrate", "info"},
							Env: []v12.EnvVar{
								{
									Name:  "FLYWAY_USER",
									Value: migration.Spec.Database.Username,
								},
								{
									Name: "FLYWAY_PASSWORD",
									ValueFrom: &v12.EnvVarSource{
										SecretKeyRef: &(migration.Spec.Database).Credentials,
									},
								},
								{
									Name:  "FLYWAY_URL",
									Value: migration.Spec.Database.JdbcUrl,
								},
							},
							VolumeMounts: []v12.VolumeMount{
								{
									Name:      sqlVolumeName,
									MountPath: "/flyway/sql",
								},
							},
						},
					},
					Volumes: []v12.Volume{
						{
							Name: sqlVolumeName,
							VolumeSource: v12.VolumeSource{
								EmptyDir: &v12.EmptyDirVolumeSource{},
							},
						},
					},
					RestartPolicy: v12.RestartPolicyNever,
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flywayv1alpha1.Migration{}).
		Complete(r)
}
