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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
)

const (
	// maxFlywayLogBytes bounds the part of the Flyway container's log that is read.
	maxFlywayLogBytes int64 = 4 << 20
	// flywayLogTimeout bounds the log read: it goes through the kubelet, and the controller reconciles one
	// Migration at a time.
	flywayLogTimeout = 10 * time.Second
	// maxSchemaVersion is the longest schema version recorded in the status; longer values are ignored.
	maxSchemaVersion = 64
	// flywayErrorSeparator joins Flyway's error message to a condition message.
	flywayErrorSeparator = "; flyway: "
	// maxDecodeAttempts bounds the JSON decodes of one log. A decode can scan the rest of the log, so a log with many
	// lines starting with '{' would otherwise cost quadratic time; Flyway prints one result.
	maxDecodeAttempts = 32
)

// PodLogReader reads the log of a container. The controller-runtime client cannot read logs, and tests fake it.
type PodLogReader interface {
	ReadLog(ctx context.Context, namespace, pod, container string, limitBytes int64) ([]byte, error)
}

// PodsLogReader reads logs through the pods/log subresource.
type PodsLogReader struct {
	Pods corev1client.PodsGetter
}

// ReadLog returns at most limitBytes of the container's log, from its start.
func (p PodsLogReader) ReadLog(ctx context.Context, namespace, pod, container string, limitBytes int64) ([]byte, error) {
	stream, err := p.Pods.Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container, LimitBytes: &limitBytes}).Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	return io.ReadAll(io.LimitReader(stream, limitBytes))
}

// flywayResult is the part of a Flyway -outputType=json result the operator reads. Several commands print one
// object with individualResults, whose own top-level schemaVersion is the version of the output format (a number),
// not the database's. Fields whose type differs between operations stay raw.
type flywayResult struct {
	Operation            string          `json:"operation"`
	FlywayVersion        string          `json:"flywayVersion"`
	SchemaVersion        json.RawMessage `json:"schemaVersion"`
	TargetSchemaVersion  json.RawMessage `json:"targetSchemaVersion"`
	InitialSchemaVersion json.RawMessage `json:"initialSchemaVersion"`
	Error                json.RawMessage `json:"error"`
	IndividualResults    []flywayResult  `json:"individualResults"`
}

// results returns the per-command results in command order.
func (f *flywayResult) results() []flywayResult {
	if f.IndividualResults != nil {
		return f.IndividualResults
	}
	return []flywayResult{*f}
}

// isResult reports whether a decoded object is shaped like a Flyway result: a command's result (operation and
// flywayVersion), an error result (an error object with a message or code), or several commands' results.
func (f *flywayResult) isResult() bool {
	if f.IndividualResults != nil {
		if len(f.IndividualResults) == 0 {
			return false
		}
		for i := range f.IndividualResults {
			if f.IndividualResults[i].IndividualResults != nil || !f.IndividualResults[i].isResult() {
				return false
			}
		}
		return true
	}
	if f.Operation != "" && f.FlywayVersion != "" {
		return true
	}
	var e struct {
		Message   string `json:"message"`
		ErrorCode string `json:"errorCode"`
	}
	return json.Unmarshal(f.Error, &e) == nil && (e.Message != "" || e.ErrorCode != "")
}

// parseFlywayOutput returns the last Flyway JSON result in a container log. JSON values are tried only where a line
// starts with '{', at most maxDecodeAttempts times, so log lines (stderr is merged into the pod log) before, between
// or after them are skipped. It returns false when the log holds no complete result, e.g. when it was cut or the
// output type was overridden.
func parseFlywayOutput(data []byte) (*flywayResult, bool) {
	var last *flywayResult
	attempts := 0
	for off := 0; off < len(data); {
		if data[off] == '{' {
			if attempts++; attempts > maxDecodeAttempts {
				break
			}
			dec := json.NewDecoder(bytes.NewReader(data[off:]))
			res := &flywayResult{}
			if err := dec.Decode(res); err == nil && res.isResult() {
				last = res
				off += int(dec.InputOffset())
				continue
			}
		}
		nl := bytes.IndexByte(data[off:], '\n')
		if nl < 0 {
			break
		}
		off += nl + 1
	}
	return last, last != nil
}

// flywaySchemaVersion returns the database's schema version from the last info (schemaVersion) or migrate
// (targetSchemaVersion, else initialSchemaVersion) result that has one, and false when none has.
func flywaySchemaVersion(f *flywayResult) (string, bool) {
	version := ""
	for _, r := range f.results() {
		var v string
		switch r.Operation {
		case "info":
			v = jsonString(r.SchemaVersion)
		case "migrate":
			if v = jsonString(r.TargetSchemaVersion); v == "" {
				v = jsonString(r.InitialSchemaVersion)
			}
		}
		if v != "" && len([]rune(v)) <= maxSchemaVersion {
			version = v
		}
	}
	return version, version != ""
}

// flywayErrorMessage returns error.message of the last result that has one, without Flyway's separator rules and
// its Statement line, whitespace collapsed and cut at maxWaitMessage; "" when no result has one.
func flywayErrorMessage(f *flywayResult) string {
	results := f.results()
	for i := len(results) - 1; i >= 0; i-- {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(results[i].Error, &e) != nil || strings.TrimSpace(e.Message) == "" {
			continue
		}
		lines := strings.Split(e.Message, "\n")
		lines = slices.DeleteFunc(lines, func(l string) bool {
			l = strings.TrimSpace(l)
			return strings.Trim(l, "-") == "" || strings.HasPrefix(l, "Statement ")
		})
		return truncate(strings.Join(lines, " "), maxWaitMessage)
	}
	return ""
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// flywayOutput reads and parses the Flyway output of the Job's newest pod, if that pod is in the given phase.
// Every failure (no reader, pods or log not readable, no JSON result) returns false and is only logged at V(1):
// the output is informational and never changes the result of the Migration.
func (r *MigrationReconciler) flywayOutput(ctx context.Context, job *batchv1.Job, phase corev1.PodPhase) (*flywayResult, bool) {
	if r.LogReader == nil {
		return nil, false
	}
	logger := log.FromContext(ctx).V(1).WithValues("job", job.Name)
	pods, err := r.podsOfJob(ctx, job, true) // fresh: the watch cache may not show the pod's final phase yet
	if err != nil {
		logger.Info("unable to list the pods of the job, Flyway output not read", "error", err.Error())
		return nil, false
	}
	if len(pods) == 0 {
		logger.Info("job has no pods, Flyway output not read")
		return nil, false
	}
	// the newest pod ran last: an earlier pod's output describes an earlier try
	pod := slices.MaxFunc(pods, func(a, b corev1.Pod) int { return a.CreationTimestamp.Compare(b.CreationTimestamp.Time) })
	if pod.Status.Phase != phase {
		logger.Info("newest pod of the job is not in the expected phase, Flyway output not read", "pod", pod.Name, "phase", pod.Status.Phase)
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, flywayLogTimeout)
	defer cancel()
	data, err := r.LogReader.ReadLog(ctx, pod.Namespace, pod.Name, flywayContainerName, maxFlywayLogBytes)
	if err != nil {
		logger.Info("unable to read the Flyway log", "pod", pod.Name, "error", err.Error())
		return nil, false
	}
	res, ok := parseFlywayOutput(data)
	if !ok {
		logger.Info("no Flyway JSON result in the log", "pod", pod.Name, "bytes", len(data))
	}
	return res, ok
}

// recordSchemaVersion sets status.schemaVersion from the output of the succeeded Job, and keeps it when the output
// cannot be read or reports no version.
func (r *MigrationReconciler) recordSchemaVersion(ctx context.Context, migration *flywayv1alpha1.Migration, job *batchv1.Job) {
	if res, ok := r.flywayOutput(ctx, job, corev1.PodSucceeded); ok {
		if v, ok := flywaySchemaVersion(res); ok {
			migration.Status.SchemaVersion = v
		}
	}
}

// failureMessage returns base with Flyway's error message of the failed Job appended. The log is read once per
// attempt: when the condition already carries base for this generation and reason, its message is kept as it is,
// so the message does not change (and the status is not written) while the Job terminates or stays failed. A first
// read that fails therefore leaves base for the rest of the attempt.
func (r *MigrationReconciler) failureMessage(ctx context.Context, migration *flywayv1alpha1.Migration, job *batchv1.Job, reason, base string) string {
	if c := meta.FindStatusCondition(migration.Status.Conditions, flywayv1alpha1.ConditionProgressing); c != nil &&
		c.Reason == reason && c.ObservedGeneration == migration.Generation &&
		(c.Message == base || strings.HasPrefix(c.Message, base+flywayErrorSeparator)) {
		return c.Message
	}
	if res, ok := r.flywayOutput(ctx, job, corev1.PodFailed); ok {
		if msg := flywayErrorMessage(res); msg != "" {
			return base + flywayErrorSeparator + msg
		}
	}
	return base
}
