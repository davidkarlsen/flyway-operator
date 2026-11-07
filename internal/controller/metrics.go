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
	"strings"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Values of the result label.
const (
	resultSucceeded = "succeeded"
	resultRetrying  = "retrying"
	resultFailed    = "failed"

	collectTimeout = 10 * time.Second
)

var (
	results    = []string{resultSucceeded, resultRetrying, resultFailed}
	conditions = []string{flywayv1alpha1.ConditionApplied, flywayv1alpha1.ConditionProgressing, flywayv1alpha1.ConditionFailed}

	migrationRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "flyway_operator_migration_runs_total",
		Help: "Finished migration Job attempts by result: succeeded, retrying (failed, another attempt follows) " +
			"or failed (failed, retryLimit exhausted). Failed attempts are retrying + failed.",
	}, []string{"namespace", "migration", "result"})

	migrationLastRunDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "flyway_operator_migration_last_run_duration_seconds",
		Help: "Run time of the last finished migration Job attempt with this result, from Job start to completion or failure.",
	}, []string{"namespace", "migration", "result"})

	statusConditionDesc = prometheus.NewDesc("flyway_operator_migration_status_condition",
		"1 for the current status (true, false, unknown) of each Migration condition, 0 for the others.",
		[]string{"namespace", "migration", "condition", "status"}, nil)
	lastTransitionDesc = prometheus.NewDesc("flyway_operator_migration_status_condition_last_transition_timestamp_seconds",
		"Unix time of the last status change of the Migration condition.",
		[]string{"namespace", "migration", "condition"}, nil)
	pausedDesc = prometheus.NewDesc("flyway_operator_migration_paused",
		"1 when the Migration is paused, else 0.",
		[]string{"namespace", "migration"}, nil)
)

func init() {
	metrics.Registry.MustRegister(migrationRuns, migrationLastRunDuration)
}

// initRunMetrics creates the run counters of a Migration at 0, so increase() also sees its first outcome.
func initRunMetrics(key types.NamespacedName) {
	for _, result := range results {
		migrationRuns.WithLabelValues(key.Namespace, key.Name, result)
	}
}

// outcomeIsNew reports whether the outcome with the given reason was not yet recorded in the previous status
// for the current generation and attempt. All three conditions carry the same reason, so Applied is read.
func outcomeIsNew(prev *flywayv1alpha1.MigrationStatus, generation int64, attempt int32, reason string) bool {
	c := meta.FindStatusCondition(prev.Conditions, flywayv1alpha1.ConditionApplied)
	return c == nil || c.Reason != reason || c.ObservedGeneration != generation || prev.Attempts != attempt
}

// recordOutcome counts a finished Job attempt and records its run time.
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
	migrationLastRunDuration.WithLabelValues(migration.Namespace, migration.Name, result).
		Set(end.Sub(job.Status.StartTime.Time).Seconds())
}

// deleteRunMetrics removes the run series of a deleted Migration.
func deleteRunMetrics(key types.NamespacedName) {
	labels := prometheus.Labels{"namespace": key.Namespace, "migration": key.Name}
	migrationRuns.DeletePartialMatch(labels)
	migrationLastRunDuration.DeletePartialMatch(labels)
}

// MigrationCollector reports the state of every Migration at scrape time, read from the reader (the manager's
// cache). It reports nothing until elected is closed, so only the leader serves state series, and a deleted
// Migration has no series to clean up.
type MigrationCollector struct {
	reader  client.Reader
	elected <-chan struct{}
}

// NewMigrationCollector returns a collector reading Migrations from reader once elected is closed.
func NewMigrationCollector(reader client.Reader, elected <-chan struct{}) *MigrationCollector {
	return &MigrationCollector{reader: reader, elected: elected}
}

// Describe implements prometheus.Collector.
func (c *MigrationCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- statusConditionDesc
	ch <- lastTransitionDesc
	ch <- pausedDesc
}

// Collect implements prometheus.Collector.
func (c *MigrationCollector) Collect(ch chan<- prometheus.Metric) {
	select {
	case <-c.elected:
	default:
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()
	list := &flywayv1alpha1.MigrationList{}
	if err := c.reader.List(ctx, list); err != nil {
		log.Log.Error(err, "listing Migrations for metrics")
		return
	}
	for i := range list.Items {
		m := &list.Items[i]
		for _, condType := range conditions {
			status := metav1.ConditionUnknown
			cond := meta.FindStatusCondition(m.Status.Conditions, condType)
			if cond != nil {
				status = cond.Status
				ch <- prometheus.MustNewConstMetric(lastTransitionDesc, prometheus.GaugeValue,
					float64(cond.LastTransitionTime.Unix()), m.Namespace, m.Name, condType)
			}
			for _, s := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
				ch <- prometheus.MustNewConstMetric(statusConditionDesc, prometheus.GaugeValue,
					boolValue(status == s), m.Namespace, m.Name, condType, strings.ToLower(string(s)))
			}
		}
		ch <- prometheus.MustNewConstMetric(pausedDesc, prometheus.GaugeValue, boolValue(m.IsPaused()), m.Namespace, m.Name)
	}
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
