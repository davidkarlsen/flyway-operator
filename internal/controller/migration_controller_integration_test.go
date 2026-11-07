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
	"strings"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	envtestTimeout  = 20 * time.Second
	envtestInterval = 100 * time.Millisecond
	// consistentlyFor is how long a spec waits to prove that something does NOT happen.
	// It exceeds deletionRequeue so at least one requeued reconcile runs inside the window.
	consistentlyFor = 3 * time.Second
	testFinalizer   = "test.flyway-operator.davidkarlsen.com/hold"
)

// newTestNamespace creates a namespace for one spec. envtest has no namespace controller, so the
// namespace only reaches Terminating on cleanup; specs never share one.
func newTestNamespace(ctx context.Context) string {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "migration-"}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed())
	})
	return ns.Name
}

func newMigration(namespace, name string) *flywayv1alpha1.Migration {
	return &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: flywayv1alpha1.MigrationSpec{
			Database: flywayv1alpha1.Database{
				Username: "someUser",
				Credentials: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "somesecret"},
					Key:                  "someKey",
				},
				JdbcUrl: "jdbc:db2://somehost:50000/SOME_DB",
			},
			FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
				Commands:      []string{"info"},
				DefaultSchema: ptr.To("someSchema"),
			},
			MigrationSource: flywayv1alpha1.MigrationSource{
				ImageRef: "ghcr.io/davidkarlsen/testmigration:latest",
			},
		},
	}
}

func getMigration(ctx context.Context, g Gomega, key types.NamespacedName) *flywayv1alpha1.Migration {
	m := &flywayv1alpha1.Migration{}
	g.Expect(k8sClient.Get(ctx, key, m)).To(Succeed())
	return m
}

func getJob(ctx context.Context, g Gomega, key types.NamespacedName) *batchv1.Job {
	job := &batchv1.Job{}
	g.Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
	return job
}

// waitForJob waits until the operator has created a Job, optionally one that differs from a previous Job UID.
func waitForJob(ctx context.Context, key types.NamespacedName, notUID types.UID) *batchv1.Job {
	var job *batchv1.Job
	Eventually(func(g Gomega) {
		job = getJob(ctx, g, key)
		g.Expect(job.UID).NotTo(Equal(notUID))
	}, envtestTimeout, envtestInterval).Should(Succeed())
	return job
}

// expectCondition waits until the Migration reports the given condition with status and reason,
// computed for the Migration's current generation.
func expectCondition(ctx context.Context, key types.NamespacedName, condType string, status metav1.ConditionStatus, reason string) *flywayv1alpha1.Migration {
	var m *flywayv1alpha1.Migration
	Eventually(func(g Gomega) {
		m = getMigration(ctx, g, key)
		c := meta.FindStatusCondition(m.Status.Conditions, condType)
		g.Expect(c).NotTo(BeNil())
		g.Expect(c.Status).To(Equal(status))
		g.Expect(c.Reason).To(Equal(reason))
		g.Expect(c.ObservedGeneration).To(Equal(m.Generation))
		g.Expect(m.Status.ObservedGeneration).To(Equal(m.Generation))
	}, envtestTimeout, envtestInterval).Should(Succeed())
	return m
}

// updateMigration re-reads the Migration and applies mutate until the update wins against concurrent status writes.
func updateMigration(ctx context.Context, key types.NamespacedName, mutate func(*flywayv1alpha1.Migration)) {
	Eventually(func(g Gomega) {
		m := getMigration(ctx, g, key)
		mutate(m)
		g.Expect(k8sClient.Update(ctx, m)).To(Succeed())
	}, envtestTimeout, envtestInterval).Should(Succeed())
}

// updateJob re-reads the Job and applies mutate until the update succeeds.
func updateJob(ctx context.Context, key types.NamespacedName, mutate func(*batchv1.Job)) {
	Eventually(func(g Gomega) {
		job := getJob(ctx, g, key)
		mutate(job)
		g.Expect(k8sClient.Update(ctx, job)).To(Succeed())
	}, envtestTimeout, envtestInterval).Should(Succeed())
}

// finishJob simulates the Job controller, which envtest does not run, by writing a terminal Job status
// that passes the API server's Job status validation.
func finishJob(ctx context.Context, key types.NamespacedName, succeeded bool) {
	Eventually(func(g Gomega) {
		job := getJob(ctx, g, key)
		now := metav1.Now()
		job.Status.StartTime = &metav1.Time{Time: now.Add(-time.Minute)}
		if succeeded {
			job.Status.Succeeded = 1
			job.Status.CompletionTime = &now
			job.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, LastTransitionTime: now},
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: now},
			}
		} else {
			job.Status.Failed = 3
			job.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, LastTransitionTime: now,
					Reason: batchv1.JobReasonBackoffLimitExceeded, Message: "Job has reached the specified backoff limit"},
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: now,
					Reason: batchv1.JobReasonBackoffLimitExceeded, Message: "Job has reached the specified backoff limit"},
			}
		}
		g.Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
	}, envtestTimeout, envtestInterval).Should(Succeed())
}

// expectRunningAttempt waits until the Migration reports the running Job as the given attempt of 3.
func expectRunningAttempt(ctx context.Context, key types.NamespacedName, attempt int32) {
	Eventually(func(g Gomega) {
		m := getMigration(ctx, g, key)
		c := meta.FindStatusCondition(m.Status.Conditions, flywayv1alpha1.ConditionProgressing)
		g.Expect(c).NotTo(BeNil())
		g.Expect(c.Reason).To(Equal(flywayv1alpha1.ReasonJobRunning))
		g.Expect(c.Message).To(ContainSubstring(fmt.Sprintf("attempt %d of 3", attempt)))
		g.Expect(m.Status.Attempts).To(Equal(attempt))
	}, envtestTimeout, envtestInterval).Should(Succeed())
}

// holdJob adds testFinalizer to the Job so its deletion stays pending, and registers a cleanup that
// releases it in case the spec fails before releasing it itself.
func holdJob(ctx context.Context, key types.NamespacedName) {
	updateJob(ctx, key, func(j *batchv1.Job) { controllerutil.AddFinalizer(j, testFinalizer) })
	DeferCleanup(func(ctx SpecContext) {
		Eventually(func() error { return releaseJob(ctx, key) }, envtestTimeout, envtestInterval).Should(Succeed())
	})
}

// releaseJob removes testFinalizer from the Job, if the Job still exists and carries it.
func releaseJob(ctx context.Context, key types.NamespacedName) error {
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, key, job); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !controllerutil.RemoveFinalizer(job, testFinalizer) {
		return nil
	}
	return client.IgnoreNotFound(k8sClient.Update(ctx, job))
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, e := range env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

var _ = Describe("Migration controller (envtest)", func() {
	var (
		ns  string
		key types.NamespacedName
	)

	BeforeEach(func(ctx SpecContext) {
		ns = newTestNamespace(ctx)
		key = types.NamespacedName{Namespace: ns, Name: "migration"}
	})

	Context("when a Migration is created", func() {
		It("creates a Job owned by the Migration that copies the SQL and runs Flyway with the configured env", func(ctx SpecContext) {
			m := newMigration(ns, key.Name)
			m.Spec.FlywayConfiguration.BaselineOnMigrate = ptr.To(true)
			m.Spec.FlywayConfiguration.JdbcProperties = map[string]string{"sslMode": "require"}
			m.Spec.FlywayConfiguration.EnvVars = []corev1.EnvVar{{Name: "FLYWAY_CLEAN_DISABLED", Value: "true"}}
			m.Spec.MigrationSource.Placeholders = map[string]string{"owner": "app"}
			m.Spec.MigrationSource.SqlPath = "/migrations"
			m.Spec.MigrationSource.Encoding = "ISO-8859-1"
			Expect(k8sClient.Create(ctx, m)).To(Succeed())

			job := waitForJob(ctx, key, "")

			owner := metav1.GetControllerOf(job)
			Expect(owner).NotTo(BeNil())
			Expect(owner.Kind).To(Equal("Migration"))
			Expect(owner.Name).To(Equal(m.Name))
			Expect(owner.UID).To(Equal(m.UID))
			Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))

			Expect(job.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Generation, "1"))
			Expect(job.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Attempt, "1"))

			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			initContainer := job.Spec.Template.Spec.InitContainers[0]
			Expect(initContainer.Image).To(Equal(m.Spec.MigrationSource.ImageRef))
			Expect(strings.Join(initContainer.Args, " ")).To(ContainSubstring("cd /migrations && cp -rp * /mnt/target/"))

			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			flyway := job.Spec.Template.Spec.Containers[0]
			Expect(flyway.Args).To(Equal([]string{"info", "-outputType=json", "-environments.default.jdbcProperties.sslMode=require"}))
			Expect(envValue(flyway.Env, "FLYWAY_USER")).To(Equal("someUser"))
			Expect(envValue(flyway.Env, "FLYWAY_URL")).To(Equal("jdbc:db2://somehost:50000/SOME_DB"))
			Expect(envValue(flyway.Env, "FLYWAY_ENCODING")).To(Equal("ISO-8859-1"))
			Expect(envValue(flyway.Env, "FLYWAY_DEFAULT_SCHEMA")).To(Equal("someSchema"))
			Expect(envValue(flyway.Env, "FLYWAY_BASELINE_ON_MIGRATE")).To(Equal("true"))
			Expect(envValue(flyway.Env, "FLYWAY_CLEAN_DISABLED")).To(Equal("true"))
			Expect(envValue(flyway.Env, "FLYWAY_PLACEHOLDERS_owner")).To(Equal("app"))
			Expect(flyway.Env).To(ContainElement(corev1.EnvVar{
				Name:      "FLYWAY_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &m.Spec.Database.Credentials},
			}))
		})

		It("reports Progressing=True with reason JobRunning while the Job runs", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			waitForJob(ctx, key, "")

			m := expectCondition(ctx, key, flywayv1alpha1.ConditionProgressing, metav1.ConditionTrue, flywayv1alpha1.ReasonJobRunning)
			Expect(meta.IsStatusConditionFalse(m.Status.Conditions, flywayv1alpha1.ConditionApplied)).To(BeTrue())
			Expect(m.Status.Attempts).To(Equal(int32(1)))
		})

		It("treats a running Job without an attempt annotation as attempt 1", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			waitForJob(ctx, key, "")
			// Move the status to attempt 2 first, so attempt 1 can only come from the missing annotation.
			updateJob(ctx, key, func(j *batchv1.Job) { j.Annotations[flywayv1alpha1.Attempt] = "2" })
			expectRunningAttempt(ctx, key, 2)

			updateJob(ctx, key, func(j *batchv1.Job) { delete(j.Annotations, flywayv1alpha1.Attempt) })

			expectRunningAttempt(ctx, key, 1)
		})

		It("reports a ReconcileError and creates no Job when the API server rejects the Job", func(ctx SpecContext) {
			// A Job name longer than 63 characters is invalid (it becomes a label value); a Migration name is not.
			longKey := types.NamespacedName{Namespace: ns, Name: strings.Repeat("m", 70)}
			Expect(k8sClient.Create(ctx, newMigration(ns, longKey.Name))).To(Succeed())

			Eventually(func(g Gomega) {
				m := getMigration(ctx, g, longKey)
				c := meta.FindStatusCondition(m.Status.Conditions, apis.ReconcileError)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
			}, envtestTimeout, envtestInterval).Should(Succeed())
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, longKey, &batchv1.Job{}))).To(BeTrue())
		})
	})

	Context("when the Job finishes", func() {
		It("reports Applied=True with reason JobSucceeded when the Job completes", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			waitForJob(ctx, key, "")

			finishJob(ctx, key, true)

			m := expectCondition(ctx, key, flywayv1alpha1.ConditionApplied, metav1.ConditionTrue, flywayv1alpha1.ReasonJobSucceeded)
			Expect(meta.IsStatusConditionFalse(m.Status.Conditions, flywayv1alpha1.ConditionProgressing)).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(m.Status.Conditions, flywayv1alpha1.ConditionFailed)).To(BeTrue())
		})

		It("replaces a failed Job with the next attempt while under retryLimit", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			first := waitForJob(ctx, key, "")

			finishJob(ctx, key, false)

			second := waitForJob(ctx, key, first.UID)
			Expect(second.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Attempt, "2"))
			Expect(second.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Generation, "1"))
			Eventually(func(g Gomega) {
				g.Expect(getMigration(ctx, g, key).Status.Attempts).To(Equal(int32(2)))
			}, envtestTimeout, envtestInterval).Should(Succeed())
		})

		It("reports Progressing=True with reason Retrying after a failed attempt", func(ctx SpecContext) {
			// Hold the failed Job with a finalizer so the transient Retrying status stays observable.
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			waitForJob(ctx, key, "")
			holdJob(ctx, key)

			finishJob(ctx, key, false)

			m := expectCondition(ctx, key, flywayv1alpha1.ConditionProgressing, metav1.ConditionTrue, flywayv1alpha1.ReasonRetrying)
			Expect(meta.IsStatusConditionFalse(m.Status.Conditions, flywayv1alpha1.ConditionFailed)).To(BeTrue())
		})

		It("does not create a successor while the failed Job is still terminating", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			first := waitForJob(ctx, key, "")
			holdJob(ctx, key)

			finishJob(ctx, key, false)

			Eventually(func(g Gomega) {
				g.Expect(getJob(ctx, g, key).DeletionTimestamp).NotTo(BeNil())
			}, envtestTimeout, envtestInterval).Should(Succeed())
			Consistently(func(g Gomega) {
				g.Expect(getJob(ctx, g, key).UID).To(Equal(first.UID))
			}, consistentlyFor, envtestInterval).Should(Succeed())

			Expect(releaseJob(ctx, key)).To(Succeed())

			second := waitForJob(ctx, key, first.UID)
			Expect(second.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Attempt, "2"))
		})

		It("reports Failed=True with reason RetryLimitExceeded and keeps the Job when retryLimit is reached", func(ctx SpecContext) {
			m := newMigration(ns, key.Name)
			m.Spec.RetryLimit = ptr.To[int32](0)
			Expect(k8sClient.Create(ctx, m)).To(Succeed())
			first := waitForJob(ctx, key, "")

			finishJob(ctx, key, false)

			got := expectCondition(ctx, key, flywayv1alpha1.ConditionFailed, metav1.ConditionTrue, flywayv1alpha1.ReasonRetryLimitExceeded)
			Expect(meta.IsStatusConditionFalse(got.Status.Conditions, flywayv1alpha1.ConditionProgressing)).To(BeTrue())
			Expect(meta.FindStatusCondition(got.Status.Conditions, flywayv1alpha1.ConditionFailed).Message).
				To(ContainSubstring(batchv1.JobReasonBackoffLimitExceeded))
			Consistently(func(g Gomega) {
				job := getJob(ctx, g, key)
				g.Expect(job.UID).To(Equal(first.UID))
				g.Expect(job.DeletionTimestamp).To(BeNil())
			}, consistentlyFor, envtestInterval).Should(Succeed())
		})
	})

	Context("when the spec changes", func() {
		It("waits for the previous generation's running Job, then replaces it with a Job for the new generation", func(ctx SpecContext) {
			Expect(k8sClient.Create(ctx, newMigration(ns, key.Name))).To(Succeed())
			first := waitForJob(ctx, key, "")
			// Hold the old Job so the transient PreviousJobReplaced status stays observable.
			holdJob(ctx, key)

			updateMigration(ctx, key, func(m *flywayv1alpha1.Migration) {
				m.Spec.MigrationSource.ImageRef = "ghcr.io/davidkarlsen/testmigration:v2"
			})

			m := expectCondition(ctx, key, flywayv1alpha1.ConditionProgressing, metav1.ConditionTrue, flywayv1alpha1.ReasonPreviousJobRunning)
			Expect(m.Generation).To(Equal(int64(2)))
			Consistently(func(g Gomega) {
				job := getJob(ctx, g, key)
				g.Expect(job.UID).To(Equal(first.UID))
				g.Expect(job.DeletionTimestamp).To(BeNil())
			}, consistentlyFor, envtestInterval).Should(Succeed())

			finishJob(ctx, key, true)

			expectCondition(ctx, key, flywayv1alpha1.ConditionProgressing, metav1.ConditionTrue, flywayv1alpha1.ReasonPreviousJobReplaced)
			Expect(releaseJob(ctx, key)).To(Succeed())

			second := waitForJob(ctx, key, first.UID)
			Expect(second.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Generation, "2"))
			Expect(second.Annotations).To(HaveKeyWithValue(flywayv1alpha1.Attempt, "1"))
			Expect(second.Spec.Template.Spec.InitContainers[0].Image).To(Equal("ghcr.io/davidkarlsen/testmigration:v2"))
		})
	})

	Context("when the Migration is paused", func() {
		It("creates no Job until the paused annotation is removed", func(ctx SpecContext) {
			m := newMigration(ns, key.Name)
			m.Annotations = map[string]string{flywayv1alpha1.Prefix + "/paused": "true"}
			Expect(k8sClient.Create(ctx, m)).To(Succeed())

			Eventually(func(g Gomega) {
				got := getMigration(ctx, g, key)
				g.Expect(meta.IsStatusConditionTrue(got.Status.Conditions, apis.ReconcileSuccess)).To(BeTrue())
			}, envtestTimeout, envtestInterval).Should(Succeed())
			Consistently(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, key, &batchv1.Job{}))
			}, consistentlyFor, envtestInterval).Should(BeTrue())

			updateMigration(ctx, key, func(m *flywayv1alpha1.Migration) {
				delete(m.Annotations, flywayv1alpha1.Prefix+"/paused")
			})

			waitForJob(ctx, key, "")
		})
	})

	Context("when the Migration is deleted", func() {
		It("is removed and leaves its Job controlled by it for garbage collection", func(ctx SpecContext) {
			m := newMigration(ns, key.Name)
			Expect(k8sClient.Create(ctx, m)).To(Succeed())
			waitForJob(ctx, key, "")

			Expect(k8sClient.Delete(ctx, m)).To(Succeed())

			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, key, &flywayv1alpha1.Migration{}))
			}, envtestTimeout, envtestInterval).Should(BeTrue())
			// envtest runs no garbage collector; the controller owner reference is what makes the cascade happen.
			job := getJob(ctx, Default, key)
			Expect(job.OwnerReferences).To(HaveLen(1))
			Expect(job.OwnerReferences[0].UID).To(Equal(m.UID))
			Expect(job.OwnerReferences[0].Controller).To(Equal(ptr.To(true)))
			Expect(job.OwnerReferences[0].BlockOwnerDeletion).To(Equal(ptr.To(true)))
		})

		It("emits a Deleting event while a finalizer holds the Migration", func(ctx SpecContext) {
			m := newMigration(ns, key.Name)
			m.Finalizers = []string{testFinalizer}
			Expect(k8sClient.Create(ctx, m)).To(Succeed())
			waitForJob(ctx, key, "")
			DeferCleanup(func(ctx SpecContext) {
				updateMigration(ctx, key, func(m *flywayv1alpha1.Migration) { controllerutil.RemoveFinalizer(m, testFinalizer) })
			})

			Expect(k8sClient.Delete(ctx, m)).To(Succeed())

			Eventually(func(g Gomega) {
				events := &corev1.EventList{}
				g.Expect(k8sClient.List(ctx, events, client.InNamespace(ns))).To(Succeed())
				reasons := []string{}
				for _, e := range events.Items {
					if e.InvolvedObject.UID == m.UID {
						reasons = append(reasons, e.Reason)
					}
				}
				g.Expect(reasons).To(ContainElement("Deleting"))
			}, envtestTimeout, envtestInterval).Should(Succeed())
		})

		It("returns no error and no requeue when reconciling a Migration that never existed", func(ctx SpecContext) {
			r := &MigrationReconciler{
				ReconcilerBase: util.NewReconcilerBase(k8sClient, scheme.Scheme, cfg, record.NewFakeRecorder(10), k8sClient),
				Client:         k8sClient,
				Scheme:         scheme.Scheme,
			}

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "gone"}})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("when the API server validates a Migration", func() {
		validMigration := func(namespace string) *unstructured.Unstructured {
			return &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": flywayv1alpha1.GroupVersion.String(),
				"kind":       "Migration",
				"metadata":   map[string]any{"generateName": "validation-", "namespace": namespace},
				"spec": map[string]any{
					"database": map[string]any{
						"username":    "someUser",
						"credentials": map[string]any{"name": "somesecret", "key": "someKey"},
						"jdbcUrl":     "jdbc:postgresql://db:5432/app",
					},
					"flywayConfiguration": map[string]any{},
					"migrationSource":     map[string]any{"imageRef": "ghcr.io/davidkarlsen/testmigration:latest"},
				},
			}}
		}

		DescribeTable("rejects an invalid Migration",
			func(ctx SpecContext, mutate func(spec map[string]any), field string) {
				u := validMigration(ns)
				mutate(u.Object["spec"].(map[string]any))

				err := k8sClient.Create(ctx, u)

				Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid, got %v", err)
				Expect(err.Error()).To(ContainSubstring(field))
			},
			Entry("jdbcUrl without the jdbc: prefix", func(spec map[string]any) {
				spec["database"].(map[string]any)["jdbcUrl"] = "postgresql://db:5432/app"
			}, "spec.database.jdbcUrl"),
			Entry("missing imageRef", func(spec map[string]any) {
				delete(spec["migrationSource"].(map[string]any), "imageRef")
			}, "spec.migrationSource.imageRef"),
			Entry("missing credentials", func(spec map[string]any) {
				delete(spec["database"].(map[string]any), "credentials")
			}, "spec.database.credentials"),
			Entry("missing username", func(spec map[string]any) {
				delete(spec["database"].(map[string]any), "username")
			}, "spec.database.username"),
			Entry("negative retryLimit", func(spec map[string]any) {
				spec["retryLimit"] = int64(-1)
			}, "spec.retryLimit"),
		)

		It("applies defaults for commands, path, encoding and retryLimit", func(ctx SpecContext) {
			u := validMigration(ns)
			Expect(k8sClient.Create(ctx, u)).To(Succeed())

			m := getMigration(ctx, Default, types.NamespacedName{Namespace: ns, Name: u.GetName()})

			Expect(m.Spec.FlywayConfiguration.Commands).To(Equal([]string{"info", "migrate", "info"}))
			Expect(m.Spec.MigrationSource.SqlPath).To(Equal("/sql"))
			Expect(m.Spec.MigrationSource.Encoding).To(Equal("UTF-8"))
			Expect(m.Spec.RetryLimit).To(Equal(ptr.To(flywayv1alpha1.DefaultRetryLimit)))
		})
	})

	Context("IsValid", func() {
		// Reconcile always passes a *Migration, so this false branch is unreachable through Reconcile;
		// the spec only covers IsValid's contract.
		It("rejects an object that is not a Migration", func() {
			valid, err := (&MigrationReconciler{}).IsValid(&batchv1.Job{})

			Expect(valid).To(BeFalse())
			Expect(err).To(MatchError("failed cast"))
		})
	})
})
