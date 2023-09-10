package controller

import (
	"context"
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Migration controller", func() {

	const (
		namespace = "default"
		name      = "test"
	)

	Context("When creating a migration", func() {
		It("Should create a job", func() {
			ginkgo.By("By creating a new migration")
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
				},
			}

			createdMigration := &flywayv1alpha1.Migration{}

			Expect(k8sClient.Create(ctx, migration)).Should(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: migration.Namespace, Name: migration.Name}, createdMigration)
				return err == nil
			}).Should(BeTrue())
		})
	})
})
