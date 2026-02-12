package controller

import (
	"context"
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/gophercloud/gophercloud/testhelper"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
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
	testhelper.AssertEquals(t, true, res.IsZero())
}

func TestMigrationReconcile_NotFound(t *testing.T) {
	const namespace = "some-namespace"
	const name = "non-existent-migration"

	ctx := context.TODO()

	s := scheme.Scheme
	s.AddKnownTypes(flywayv1alpha1.SchemeBuilder.GroupVersion, &flywayv1alpha1.Migration{})

	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
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
	testhelper.AssertEquals(t, true, res.IsZero())
}

func TestMigrationReconcile_Paused(t *testing.T) {
	const namespace = "some-namespace"
	const name = "paused-migration"

	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				flywayv1alpha1.Prefix + "/paused": "true",
			},
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
	testhelper.AssertEquals(t, true, res.IsZero())

	// Verify no job was created
	job := &batchv1.Job{}
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, job)
	testhelper.AssertEquals(t, true, apierrors.IsNotFound(err))
}

func TestMigrationReconcile_WithExistingRunningJob(t *testing.T) {
	const namespace = "some-namespace"
	const name = "migration-with-running-job"

	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
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

	// Create a running job (not finished)
	runningJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				flywayv1alpha1.Generation: "1",
			},
		},
		Status: batchv1.JobStatus{
			Active: 1,
		},
	}

	objs := []runtime.Object{migration, runningJob}
	ctx := context.TODO()

	s := scheme.Scheme
	s.AddKnownTypes(flywayv1alpha1.SchemeBuilder.GroupVersion, migration)
	s.AddKnownTypes(batchv1.SchemeGroupVersion, runningJob)

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
	testhelper.AssertEquals(t, true, res.IsZero())
}

func TestMigrationReconcile_WithSucceededJob(t *testing.T) {
	const namespace = "some-namespace"
	const name = "migration-with-succeeded-job"

	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
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

	// Create a succeeded job
	succeededJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				flywayv1alpha1.Generation: "1",
			},
		},
		Status: batchv1.JobStatus{
			Succeeded: 1,
			Conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	objs := []runtime.Object{migration, succeededJob}
	ctx := context.TODO()

	s := scheme.Scheme
	s.AddKnownTypes(flywayv1alpha1.SchemeBuilder.GroupVersion, migration)
	s.AddKnownTypes(batchv1.SchemeGroupVersion, succeededJob)

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
	testhelper.AssertEquals(t, true, res.IsZero())
}

func TestMigrationReconcile_WithFailedJob(t *testing.T) {
	const namespace = "some-namespace"
	const name = "migration-with-failed-job"

	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
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

	// Create a failed job
	failedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				flywayv1alpha1.Generation: "1",
			},
		},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobFailed,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	objs := []runtime.Object{migration, failedJob}
	ctx := context.TODO()

	s := scheme.Scheme
	s.AddKnownTypes(flywayv1alpha1.SchemeBuilder.GroupVersion, migration)
	s.AddKnownTypes(batchv1.SchemeGroupVersion, failedJob)

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
	testhelper.AssertEquals(t, true, res.IsZero())
}

func TestIsValid(t *testing.T) {
	r := &MigrationReconciler{}

	t.Run("valid migration", func(t *testing.T) {
		migration := &flywayv1alpha1.Migration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		valid, err := r.IsValid(migration)
		testhelper.AssertNoErr(t, err)
		testhelper.AssertEquals(t, true, valid)
	})

	t.Run("invalid type", func(t *testing.T) {
		invalidObj := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		valid, err := r.IsValid(invalidObj)
		testhelper.AssertEquals(t, false, valid)
		if err == nil {
			t.Error("expected error for invalid type")
		}
	})
}
