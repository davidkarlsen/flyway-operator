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
	"testing"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newMetricsHarness returns a status harness with no run series left over for statusKey.
func newMetricsHarness(t *testing.T, retryLimit *int32) *statusHarness {
	t.Helper()
	deleteRunMetrics(statusKey)
	t.Cleanup(func() { deleteRunMetrics(statusKey) })
	return newStatusHarness(t, retryLimit)
}

func runs(result string) float64 {
	return testutil.ToFloat64(migrationRuns.WithLabelValues(statusKey.Namespace, statusKey.Name, result))
}

func lastDuration(result string) float64 {
	return testutil.ToFloat64(migrationLastRunDuration.WithLabelValues(statusKey.Namespace, statusKey.Name, result))
}

func (h *statusHarness) expectRuns(succeeded, retrying, failed float64) {
	h.t.Helper()
	for result, want := range map[string]float64{resultSucceeded: succeeded, resultRetrying: retrying, resultFailed: failed} {
		if got := runs(result); got != want {
			h.t.Errorf("runs{result=%q} = %v, want %v", result, got, want)
		}
	}
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

func TestMetricsRunsCreatedAtZero(t *testing.T) {
	h := newMetricsHarness(t, nil)
	h.reconcile()
	if n := seriesOf(migrationRuns); n != 3 {
		t.Errorf("runs series after the first reconcile = %d, want 3", n)
	}
	h.expectRuns(0, 0, 0)
}

func TestMetricsSucceededCountedOnce(t *testing.T) {
	h := newMetricsHarness(t, nil)
	h.reconcile()
	h.reconcile()

	h.finishJob(batchv1.JobComplete, 0)
	h.timeJob(42 * time.Second)
	for range 3 {
		h.reconcile()
	}
	h.expectRuns(1, 0, 0)
	if got := lastDuration(resultSucceeded); got != 42 {
		t.Errorf("last succeeded duration = %v, want 42", got)
	}
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
	h.expectRuns(0, 1, 0)

	// attempt 2 fails at the limit: failed, kept across reconciles
	h.finishJob(batchv1.JobFailed, 3)
	h.timeJob(20 * time.Second)
	for range 3 {
		h.reconcile()
	}
	h.expectRuns(0, 1, 1)
	if got := lastDuration(resultRetrying); got != 10 {
		t.Errorf("last retrying duration = %v, want 10", got)
	}
	if got := lastDuration(resultFailed); got != 20 {
		t.Errorf("last failed duration = %v, want 20", got)
	}
}

func TestMetricsNewGenerationCountsAgain(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](0))
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)
	h.reconcile()

	h.updateImage("somereg.io/someimage:2", 2)
	h.reconcile() // previous job replaced
	h.reconcile() // job created
	h.finishJob(batchv1.JobComplete, 0)
	h.reconcile()
	h.reconcile()
	h.expectRuns(2, 0, 0)
}

func TestMetricsNotCountedWhenStatusWriteFails(t *testing.T) {
	h := newMetricsHarness(t, nil)
	h.reconcile()
	h.finishJob(batchv1.JobComplete, 0)

	h.failStatusUpdates = 1
	if _, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey}); err == nil {
		t.Fatal("expected the injected status conflict to be returned")
	}
	h.expectRuns(0, 0, 0)
	h.reconcile()
	h.reconcile()
	h.expectRuns(1, 0, 0)
}

func TestMetricsNotCountedAgainAfterRestart(t *testing.T) {
	for name, tc := range map[string]struct {
		retryLimit *int32
		finish     batchv1.JobConditionType
	}{
		"succeeded": {nil, batchv1.JobComplete},
		"failed":    {ptr.To[int32](0), batchv1.JobFailed},
	} {
		t.Run(name, func(t *testing.T) {
			h := newMetricsHarness(t, tc.retryLimit)
			h.reconcile()
			h.finishJob(tc.finish, 3)
			h.reconcile()

			// a restart loses all series; the persisted status records the outcome, so it is not counted again
			deleteRunMetrics(statusKey)
			h.reconcile()
			h.reconcile()
			h.expectRuns(0, 0, 0)
		})
	}
}

func TestMetricsRemovedOnDelete(t *testing.T) {
	for name, finalizer := range map[string]bool{"not found": false, "being deleted": true} {
		t.Run(name, func(t *testing.T) {
			h := newMetricsHarness(t, nil)
			h.reconcile()
			h.finishJob(batchv1.JobComplete, 0)
			h.timeJob(time.Second)
			h.reconcile()
			if seriesOf(migrationRuns) == 0 || seriesOf(migrationLastRunDuration) == 0 {
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
			for _, c := range []prometheus.Collector{migrationRuns, migrationLastRunDuration} {
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

	for _, name := range []string{"flyway_operator_migration_runs_total", "flyway_operator_migration_last_run_duration_seconds"} {
		if n, err := testutil.GatherAndCount(metrics.Registry, name); err != nil || n == 0 {
			t.Errorf("%s: %d series in the controller-runtime registry (%v), want > 0", name, n, err)
		}
	}
}

// expectedState renders the collector's series for statusKey, with the given status per condition and the
// LastTransitionTime of each condition in m.
func expectedState(m *flywayv1alpha1.Migration, paused int, applied, progressing, failed string) string {
	var b strings.Builder
	b.WriteString("# HELP flyway_operator_migration_paused 1 when the Migration is paused, else 0.\n" +
		"# TYPE flyway_operator_migration_paused gauge\n")
	fmt.Fprintf(&b, "flyway_operator_migration_paused{migration=%q,namespace=%q} %d\n", statusKey.Name, statusKey.Namespace, paused)
	b.WriteString("# HELP flyway_operator_migration_status_condition 1 for the current status (true, false, unknown) of each Migration condition, 0 for the others.\n" +
		"# TYPE flyway_operator_migration_status_condition gauge\n")
	want := map[string]string{
		flywayv1alpha1.ConditionApplied: applied, flywayv1alpha1.ConditionProgressing: progressing, flywayv1alpha1.ConditionFailed: failed,
	}
	for _, cond := range []string{flywayv1alpha1.ConditionApplied, flywayv1alpha1.ConditionFailed, flywayv1alpha1.ConditionProgressing} {
		for _, s := range []string{"false", "true", "unknown"} {
			v := 0
			if want[cond] == s {
				v = 1
			}
			fmt.Fprintf(&b, "flyway_operator_migration_status_condition{condition=%q,migration=%q,namespace=%q,status=%q} %d\n",
				cond, statusKey.Name, statusKey.Namespace, s, v)
		}
	}
	b.WriteString("# HELP flyway_operator_migration_status_condition_last_transition_timestamp_seconds Unix time of the last status change of the Migration condition.\n" +
		"# TYPE flyway_operator_migration_status_condition_last_transition_timestamp_seconds gauge\n")
	for _, cond := range []string{flywayv1alpha1.ConditionApplied, flywayv1alpha1.ConditionFailed, flywayv1alpha1.ConditionProgressing} {
		if c := meta.FindStatusCondition(m.Status.Conditions, cond); c != nil {
			fmt.Fprintf(&b, "flyway_operator_migration_status_condition_last_transition_timestamp_seconds{condition=%q,migration=%q,namespace=%q} %d\n",
				cond, statusKey.Name, statusKey.Namespace, c.LastTransitionTime.Unix())
		}
	}
	return b.String()
}

func TestMigrationCollector(t *testing.T) {
	h := newStatusHarness(t, ptr.To[int32](0))
	elected := make(chan struct{})
	collector := NewMigrationCollector(h.c, elected)

	if n := testutil.CollectAndCount(collector); n != 0 {
		t.Fatalf("collector reported %d series before election, want 0", n)
	}
	close(elected)

	// no status yet: every condition unknown, no transition times
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expectedState(h.migration(), 0, "unknown", "unknown", "unknown"))); err != nil {
		t.Error(err)
	}

	m := h.reconcile()
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expectedState(m, 0, "false", "true", "false"))); err != nil {
		t.Error(err)
	}

	h.finishJob(batchv1.JobFailed, 3)
	m = h.reconcile()
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expectedState(m, 0, "false", "false", "true"))); err != nil {
		t.Error(err)
	}

	m.Annotations = map[string]string{flywayv1alpha1.Prefix + "/paused": "true"}
	if err := h.c.Update(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expectedState(m, 1, "false", "false", "true"))); err != nil {
		t.Error(err)
	}

	// a deleted Migration has no series
	if err := h.c.Delete(context.TODO(), m); err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(collector); n != 0 {
		t.Errorf("collector reported %d series after delete, want 0", n)
	}
}
