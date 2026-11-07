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
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Values of the result label.
const (
	resultSucceeded = "succeeded"
	resultRetrying  = "retrying"
	resultFailed    = "failed"
)

var (
	migrationRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "flyway_operator_migration_runs_total",
		Help: "Finished migration Jobs by result: succeeded, retrying (another attempt follows) or failed (retryLimit exhausted).",
	}, []string{"namespace", "migration", "result"})

	migrationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "flyway_operator_migration_duration_seconds",
		Help:    "Run time of finished migration Jobs, from Job start to completion or failure.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"namespace", "migration", "result"})

	migrationStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "flyway_operator_migration_status",
		Help: "1 when the Migration condition (Applied, Progressing, Failed) is True, else 0.",
	}, []string{"namespace", "migration", "condition"})
)

func init() {
	metrics.Registry.MustRegister(migrationRuns, migrationDuration, migrationStatus)
}

// outcomeIsNew reports whether the outcome with the given reason was not yet recorded in the previous status
// for the current generation and attempt. All three conditions carry the same reason, so Applied is read.
func outcomeIsNew(prev *flywayv1alpha1.MigrationStatus, generation int64, attempt int32, reason string) bool {
	c := meta.FindStatusCondition(prev.Conditions, flywayv1alpha1.ConditionApplied)
	return c == nil || c.Reason != reason || c.ObservedGeneration != generation || prev.Attempts != attempt
}

// recordOutcome counts a finished Job and observes its run time.
func recordOutcome(migration *flywayv1alpha1.Migration, job *batchv1.Job, result string) {
	migrationRuns.WithLabelValues(migration.Namespace, migration.Name, result).Inc()
	if job.Status.StartTime == nil {
		return
	}
	end := job.Status.CompletionTime
	if end == nil {
		for _, c := range job.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				end = &c.LastTransitionTime
				break
			}
		}
	}
	if end == nil || end.IsZero() || end.Before(job.Status.StartTime) {
		return
	}
	migrationDuration.WithLabelValues(migration.Namespace, migration.Name, result).
		Observe(end.Sub(job.Status.StartTime.Time).Seconds())
}

// recordStatus sets the status gauge from the Migration's conditions.
func recordStatus(migration *flywayv1alpha1.Migration) {
	for _, condType := range []string{flywayv1alpha1.ConditionApplied, flywayv1alpha1.ConditionProgressing, flywayv1alpha1.ConditionFailed} {
		value := 0.0
		if meta.IsStatusConditionTrue(migration.Status.Conditions, condType) {
			value = 1
		}
		migrationStatus.WithLabelValues(migration.Namespace, migration.Name, condType).Set(value)
	}
}

// deleteMetrics removes all series of a Migration.
func deleteMetrics(key types.NamespacedName) {
	labels := prometheus.Labels{"namespace": key.Namespace, "migration": key.Name}
	migrationRuns.DeletePartialMatch(labels)
	migrationDuration.DeletePartialMatch(labels)
	migrationStatus.DeletePartialMatch(labels)
}
