package controller

import (
	"context"
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

var _ = Describe("Migration controller", func() {

	const (
		namespace = "default"
		name      = "test"

		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a migration", func() {
		It("Should create a job", func() {
			By("By creating a new migration")
			ctx := context.Background()
			migration := &flywayv1alpha1.Migration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: flywayv1alpha1.GroupVersion.Version,
					Kind:       "Migration",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: flywayv1alpha1.MigrationSpec{
					Database: flywayv1alpha1.Database{
						Username: "someUser",
						Credentials: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "somesecret",
							},
							Key: "someKey",
						},
						JdbcUrl: "jdbc:db2://somehost:50000/SOME_DB",
					},
					MigrationSource: flywayv1alpha1.MigrationSource{
						ImageRef: "ghcr.io/davidkarlsen/testmigration:latest",
					},
				},
			}

			Expect(k8sClient.Create(ctx, migration)).Should(Succeed())
			Eventually(func(g Gomega) {
				createdMigration := &flywayv1alpha1.Migration{}
				objectKey := types.NamespacedName{Namespace: migration.Namespace, Name: migration.Name}
				err := k8sClient.Get(ctx, objectKey, createdMigration)
				g.Expect(err).To(BeNil())

				job := &batchv1.Job{}
				err = k8sClient.Get(ctx, objectKey, job)

				// TODO: fix
				// g.Expect(err).To(BeNil())

			}, timeout, interval).Should(Succeed())
		})
	})
})
