package controller

import (
	"context"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/gophercloud/gophercloud/testhelper"
	"github.com/redhat-cop/operator-utils/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestGithubactionRunnerController(t *testing.T) {
	const namespace = "some-namespace"
	const name = "some-migration"

	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: flywayv1alpha1.MigrationSpec{
			FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
				BaselineOnMigrate: ptr.To(true),
				DefaultSchema:     ptr.To("someSchema"),
			},
			Database: flywayv1alpha1.Database{
				Username:    "someUser",
				Credentials: corev1.SecretKeySelector{},
				JdbcUrl:     "jdbc://db2:somehost:50000/somedb",
			},
			MigrationSource: flywayv1alpha1.MigrationSource{
				ImageRef: "somereg.io/someimage:sometag",
			},
		},
	}

	objs := []runtime.Object{migration}
	ctx := context.TODO()

	s := scheme.Scheme
	s.AddKnownTypes(flywayv1alpha1.SchemeBuilder.GroupVersion, migration)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).WithStatusSubresource(migration).Build()

	fakeRecorder := record.NewFakeRecorder(10)
	r := &MigrationReconciler{
		ReconcilerBase: util.NewReconcilerBase(fakeClient, s, nil, fakeRecorder, nil),
		Client:         fakeClient,
		Scheme:         s,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}

	res, err := r.Reconcile(ctx, req)
	testhelper.AssertNoErr(t, err)
	testhelper.AssertEquals(t, false, res.RequeueAfter)
}
