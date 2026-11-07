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
	"testing"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newMetricsHarness returns a status harness with no metric series left over for statusKey.
func newMetricsHarness(t *testing.T, retryLimit *int32) *statusHarness {
	t.Helper()
	deleteMetrics(statusKey)
	t.Cleanup(func() { deleteMetrics(statusKey) })
	return newStatusHarness(t, retryLimit)
}

func runs(result string) float64 {
	return testutil.ToFloat64(migrationRuns.WithLabelValues(statusKey.Namespace, statusKey.Name, result))
}

func status(condition string) float64 {
	return testutil.ToFloat64(migrationStatus.WithLabelValues(statusKey.Namespace, statusKey.Name, condition))
}

// durations returns the sample count and sum of the duration histogram for result.
func durations(t *testing.T, result string) (uint64, float64) {
	t.Helper()
	m := &dto.Metric{}
	if err := migrationDuration.WithLabelValues(statusKey.Namespace, statusKey.Name, result).(prometheus.Histogram).Write(m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

// seriesOf counts the series of c that belong to statusKey.
func seriesOf(c prometheus.Collector) int {
	ch := make(chan prometheus.Metric, 100)
	go func() { c.Collect(ch); close(ch) }()
	n := 0
	for metric := range ch {
		m := &dto.Metric{}
		_ = metric.Write(m)
		labels := map[string]string{}
		for _, l := range m.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		if labels["namespace"] == statusKey.Namespace && labels["migration"] == statusKey.Name {
			n++
		}
	}
	return n
}

// timeJob sets the Job's start time and moves its completion (or failure) time to start+d.
func (h *statusHarness) timeJob(d time.Duration) {
	h.t.Helper()
	j := h.job()
	start := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	end := metav1.NewTime(start.Add(d))
	j.Status.StartTime = &start
	if j.Status.Succeeded > 0 {
		j.Status.CompletionTime = &end
	}
	for i := range j.Status.Conditions {
		j.Status.Conditions[i].LastTransitionTime = end
	}
	if err := h.c.Status().Update(context.TODO(), j); err != nil {
		h.t.Fatal(err)
	}
}

func (h *statusHarness) expectStatusGauge(applied, progressing, failed float64) {
	h.t.Helper()
	for cond, want := range map[string]float64{
		flywayv1alpha1.ConditionApplied:     applied,
		flywayv1alpha1.ConditionProgressing: progressing,
		flywayv1alpha1.ConditionFailed:      failed,
	} {
		if got := status(cond); got != want {
			h.t.Errorf("status gauge %s = %v, want %v", cond, got, want)
		}
	}
}

func TestMetricsSucceededCountedOnce(t *testing.T) {
	h := newMetricsHarness(t, nil)

	h.reconcile()
	h.expectStatusGauge(0, 1, 0)
	h.reconcile()
	h.expectStatusGauge(0, 1, 0)

	h.finishJob(batchv1.JobComplete, 0)
	h.timeJob(42 * time.Second)
	for range 3 {
		h.reconcile()
	}
	if got := runs(resultSucceeded); got != 1 {
		t.Errorf("succeeded runs = %v, want 1", got)
	}
	if count, sum := durations(t, resultSucceeded); count != 1 || sum != 42 {
		t.Errorf("succeeded duration samples = %d sum %v, want 1 sum 42", count, sum)
	}
	if got := runs(resultFailed) + runs(resultRetrying); got != 0 {
		t.Errorf("failed+retrying runs = %v, want 0", got)
	}
	h.expectStatusGauge(1, 0, 0)
}

func TestMetricsRetryingThenFailedCountedOnceEach(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](1))
	h.reconcile()

	// attempt 1 fails: retrying
	h.finishJob(batchv1.JobFailed, 3)
	h.timeJob(10 * time.Second)
	h.reconcile() // Retrying, Job deleted
	h.reconcile() // attempt 2 created
	h.reconcile() // attempt 2 running
	if got := runs(resultRetrying); got != 1 {
		t.Errorf("retrying runs = %v, want 1", got)
	}
	h.expectStatusGauge(0, 1, 0)

	// attempt 2 fails at the limit: failed, kept across reconciles
	h.finishJob(batchv1.JobFailed, 3)
	h.timeJob(20 * time.Second)
	for range 3 {
		h.reconcile()
	}
	if got := runs(resultFailed); got != 1 {
		t.Errorf("failed runs = %v, want 1", got)
	}
	if got := runs(resultRetrying); got != 1 {
		t.Errorf("retrying runs = %v after the final failure, want 1", got)
	}
	if count, sum := durations(t, resultRetrying); count != 1 || sum != 10 {
		t.Errorf("retrying duration samples = %d sum %v, want 1 sum 10", count, sum)
	}
	if count, sum := durations(t, resultFailed); count != 1 || sum != 20 {
		t.Errorf("failed duration samples = %d sum %v, want 1 sum 20", count, sum)
	}
	h.expectStatusGauge(0, 0, 1)
}

func TestMetricsNewGenerationCountsAgain(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](0))
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)
	h.reconcile()

	h.updateImage("somereg.io/someimage:2", 2)
	h.reconcile() // previous job replaced
	h.expectStatusGauge(0, 1, 0)
	h.reconcile() // job created
	h.finishJob(batchv1.JobComplete, 0)
	h.reconcile()
	h.reconcile()
	if got := runs(resultSucceeded); got != 2 {
		t.Errorf("succeeded runs = %v, want 2 (one per generation)", got)
	}
}

func TestMetricsNotCountedWhenStatusWriteFails(t *testing.T) {
	h := newMetricsHarness(t, nil)
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)

	h.failStatusUpdates = 1
	if _, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey}); err == nil {
		t.Fatal("expected the injected status conflict to be returned")
	}
	if got := runs(resultSucceeded); got != 0 {
		t.Errorf("succeeded runs = %v after a failed status write, want 0", got)
	}
	h.reconcile()
	h.reconcile()
	if got := runs(resultSucceeded); got != 1 {
		t.Errorf("succeeded runs = %v, want 1", got)
	}
}

func TestMetricsAfterRestart(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](0))
	h.reconcile()
	h.finishJob(batchv1.JobFailed, 3)
	h.reconcile()

	// a restart loses all series; the persisted status restores the gauge but does not count the outcome again
	deleteMetrics(statusKey)
	h.reconcile()
	if got := seriesOf(migrationRuns); got != 0 {
		t.Errorf("runs series after restart = %d, want 0", got)
	}
	h.expectStatusGauge(0, 0, 1)
}

func TestMetricsRemovedOnDelete(t *testing.T) {
	for name, finalizer := range map[string]bool{"not found": false, "being deleted": true} {
		t.Run(name, func(t *testing.T) {
			h := newMetricsHarness(t, nil)
			h.reconcile()
			h.finishJob(batchv1.JobComplete, 0)
			h.reconcile()
			if seriesOf(migrationRuns) == 0 || seriesOf(migrationStatus) == 0 {
				t.Fatal("expected series before delete")
			}

			m := h.migration()
			if finalizer {
				m.Finalizers = []string{"test/hold"}
				if err := h.c.Update(context.TODO(), m); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.c.Delete(context.TODO(), m); err != nil {
				t.Fatal(err)
			}
			if _, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey}); err != nil {
				t.Fatal(err)
			}
			for _, c := range []prometheus.Collector{migrationRuns, migrationDuration, migrationStatus} {
				if n := seriesOf(c); n != 0 {
					t.Errorf("%d series left after delete", n)
				}
			}
		})
	}
}

func TestMetricsRegistered(t *testing.T) {
	h := newMetricsHarness(t, nil)
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)
	h.timeJob(time.Second)
	h.reconcile()

	// other tests in the package may leave series of their own Migrations, so only presence is checked here
	for _, name := range []string{
		"flyway_operator_migration_runs_total",
		"flyway_operator_migration_duration_seconds",
		"flyway_operator_migration_status",
	} {
		if n, err := testutil.GatherAndCount(metrics.Registry, name); err != nil || n == 0 {
			t.Errorf("%s: %d series in the controller-runtime registry (%v), want > 0", name, n, err)
		}
	}
}
